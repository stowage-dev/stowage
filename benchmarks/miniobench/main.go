// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// MinIO-direct comparison benchmark. Mirrors the cases in
// ../s3proxybench (minus the proxy-only paths: ListBuckets-synthesised,
// anonymous, and scope-violation, which have no native MinIO equivalent),
// then renders results so the delta against the stowage-fronted run
// isolates the proxy overhead.
//
// Cases are deliberately named with a "MinIO " prefix so the comparison
// gate can pair them with "Proxy " names in ../results-s3proxy.json.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type sample struct {
	dur time.Duration
	ok  bool
}

type result struct {
	name        string
	concurrency int
	duration    time.Duration
	ops         int64
	errs        int64
	latencies   []time.Duration
}

func (r *result) percentile(p float64) time.Duration {
	if len(r.latencies) == 0 {
		return 0
	}
	return r.latencies[int(float64(len(r.latencies)-1)*p)]
}
func (r *result) min() time.Duration {
	if len(r.latencies) == 0 {
		return 0
	}
	return r.latencies[0]
}
func (r *result) max() time.Duration {
	if len(r.latencies) == 0 {
		return 0
	}
	return r.latencies[len(r.latencies)-1]
}
func (r *result) mean() time.Duration {
	if len(r.latencies) == 0 {
		return 0
	}
	var sum time.Duration
	for _, d := range r.latencies {
		sum += d
	}
	return sum / time.Duration(len(r.latencies))
}
func (r *result) throughput() float64 {
	return float64(r.ops-r.errs) / r.duration.Seconds()
}

// jsonResult mirrors ../s3proxybench's structure so a future check-style
// regression gate can ingest both files unchanged and pair them by name.
type jsonResult struct {
	Name          string  `json:"name"`
	Concurrency   int     `json:"concurrency"`
	Ops           int64   `json:"ops"`
	Errs          int64   `json:"errs"`
	DurationMS    float64 `json:"duration_ms"`
	ThroughputRPS float64 `json:"throughput_rps"`
	MeanMS        float64 `json:"mean_ms"`
	P50MS         float64 `json:"p50_ms"`
	P95MS         float64 `json:"p95_ms"`
	P99MS         float64 `json:"p99_ms"`
	MinMS         float64 `json:"min_ms"`
	MaxMS         float64 `json:"max_ms"`
}

func main() {
	endpoint := flag.String("endpoint", "http://localhost:19000", "MinIO S3 endpoint")
	access := flag.String("access-key", "minioadmin", "access key id")
	secret := flag.String("secret-key", "minioadmin", "secret access key")
	region := flag.String("region", "us-east-1", "S3 region (MinIO is region-agnostic but SigV4 needs one)")
	bucket := flag.String("bucket", "bench-direct", "bucket to use (created if missing)")
	duration := flag.Duration("duration", 15*time.Second, "per-endpoint benchmark duration")
	concurrency := flag.Int("concurrency", 16, "concurrent workers per endpoint")
	warmup := flag.Duration("warmup", 2*time.Second, "warmup duration before each case")
	output := flag.String("output", "results-minio.md", "output markdown file (use - for stdout only)")
	jsonOutput := flag.String("json", "", "optional JSON output path")
	skipWrites := flag.Bool("skip-writes", false, "skip Put/Delete cases (read-only run)")
	flag.Parse()

	ctx := context.Background()

	httpClient := awshttp.NewBuildableClient().WithTransportOptions(func(t *http.Transport) {
		t.MaxIdleConnsPerHost = 256
		t.MaxConnsPerHost = 256
	})
	cfg := aws.Config{
		Region:       *region,
		Credentials:  credentials.NewStaticCredentialsProvider(*access, *secret, ""),
		BaseEndpoint: aws.String(*endpoint),
		HTTPClient:   httpClient,
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true // MinIO requires path-style addressing
	})

	if err := ensureBucket(ctx, client, *bucket); err != nil {
		fail("ensure bucket: %v", err)
	}
	const (
		probe1KKey = "bench/probe-1k.bin"
		probe1MKey = "bench/probe-1m.bin"
	)
	probe1K := bytes.Repeat([]byte("x"), 1024)
	probe1M := bytes.Repeat([]byte("y"), 1<<20)
	if err := putObject(ctx, client, *bucket, probe1KKey, probe1K); err != nil {
		fail("seed %s: %v", probe1KKey, err)
	}
	if err := putObject(ctx, client, *bucket, probe1MKey, probe1M); err != nil {
		fail("seed %s: %v", probe1MKey, err)
	}
	fmt.Fprintln(os.Stderr, "seeded probe objects directly into MinIO")

	// Pre-built presigned URL — re-presigning every iteration would dominate
	// the case latency with client-side signing instead of MinIO work.
	presigner := s3.NewPresignClient(client)
	presigned, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(*bucket),
		Key:    aws.String(probe1KKey),
	}, s3.WithPresignExpires(2*time.Hour))
	if err != nil {
		fail("presign get: %v", err)
	}

	cases := []struct {
		name string
		fn   func() error
	}{
		{
			name: "MinIO ListBuckets",
			fn: func() error {
				_, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
				return err
			},
		},
		{
			name: "MinIO HeadBucket",
			fn: func() error {
				_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{
					Bucket: aws.String(*bucket),
				})
				return err
			},
		},
		{
			name: "MinIO ListObjectsV2",
			fn: func() error {
				_, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
					Bucket: aws.String(*bucket),
				})
				return err
			},
		},
		{
			name: "MinIO HeadObject",
			fn: func() error {
				_, err := client.HeadObject(ctx, &s3.HeadObjectInput{
					Bucket: aws.String(*bucket),
					Key:    aws.String(probe1KKey),
				})
				return err
			},
		},
		{
			name: "MinIO GetObject 1 KiB",
			fn:   getFn(ctx, client, *bucket, probe1KKey),
		},
		{
			name: "MinIO GetObject 1 MiB",
			fn:   getFn(ctx, client, *bucket, probe1MKey),
		},
		{
			name: "MinIO GetObject (presigned)",
			fn:   presignedGetFn(presigned.URL),
		},
		{
			name: "MinIO Auth Failure (bad sig)",
			fn:   badSigFn(*endpoint, *bucket, probe1KKey, *access),
		},
	}

	if !*skipWrites {
		var putCounter atomic.Uint64
		mkPutFn := func(payload []byte, prefix string) func() error {
			return func() error {
				idx := putCounter.Add(1)
				key := fmt.Sprintf("%s/%d.bin", prefix, idx)
				return putObject(ctx, client, *bucket, key, payload)
			}
		}
		cases = append(cases,
			struct {
				name string
				fn   func() error
			}{
				name: "MinIO PutObject 1 KiB",
				fn:   mkPutFn(probe1K, "bench/put-1k"),
			},
			struct {
				name string
				fn   func() error
			}{
				name: "MinIO PutObject 1 MiB",
				fn:   mkPutFn(probe1M, "bench/put-1m"),
			},
			struct {
				name string
				fn   func() error
			}{
				// Mirrors the proxy bench: DeleteObject on a never-existing key
				// returns 204, keeping the case stable across iterations without
				// a producer goroutine to refill keys.
				name: "MinIO DeleteObject",
				fn: func() error {
					key := fmt.Sprintf("bench/del/%d-%d.bin", time.Now().UnixNano(), randUint64())
					_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
						Bucket: aws.String(*bucket),
						Key:    aws.String(key),
					})
					return err
				},
			},
		)
	}

	results := make([]result, 0, len(cases)+1)
	for _, c := range cases {
		fmt.Fprintf(os.Stderr, "running %s @ %d concurrent for %s\n", c.name, *concurrency, *duration)
		results = append(results, runCase(c.name, c.fn, *concurrency, *duration, *warmup))
	}

	// Health endpoint, no auth — comparable to stowage's /healthz. Hit via
	// plain HTTP for an apples-to-apples router-only baseline.
	fmt.Fprintf(os.Stderr, "running MinIO /minio/health/live @ %d concurrent for %s\n",
		*concurrency, *duration)
	results = append(results, runHealthCase(*endpoint, *concurrency, *duration, *warmup))

	report := renderMarkdown(results, *endpoint, *concurrency, *duration)
	fmt.Println(report)
	if *output != "-" && *output != "" {
		if err := os.WriteFile(*output, []byte(report), 0o644); err != nil {
			fail("write %s: %v", *output, err)
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", *output)
	}
	if *jsonOutput != "" {
		blob, err := renderJSON(results)
		if err != nil {
			fail("render json: %v", err)
		}
		if err := os.WriteFile(*jsonOutput, blob, 0o644); err != nil {
			fail("write %s: %v", *jsonOutput, err)
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", *jsonOutput)
	}
}

func ensureBucket(ctx context.Context, c *s3.Client, bucket string) error {
	_, err := c.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(bucket)})
	if err == nil {
		return nil
	}
	_, err = c.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	if err != nil && !strings.Contains(err.Error(), "BucketAlreadyOwnedByYou") &&
		!strings.Contains(err.Error(), "BucketAlreadyExists") {
		return err
	}
	return nil
}

func putObject(ctx context.Context, c *s3.Client, bucket, key string, body []byte) error {
	_, err := c.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(body),
		ContentLength: aws.Int64(int64(len(body))),
	})
	return err
}

func getFn(ctx context.Context, c *s3.Client, bucket, key string) func() error {
	return func() error {
		out, err := c.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			return err
		}
		_, _ = io.Copy(io.Discard, out.Body)
		return out.Body.Close()
	}
}

func presignedGetFn(rawURL string) func() error {
	tr := &http.Transport{MaxIdleConnsPerHost: 256, MaxConnsPerHost: 256, IdleConnTimeout: 30 * time.Second}
	cli := &http.Client{Transport: tr, Timeout: 30 * time.Second}
	return func() error {
		req, _ := http.NewRequest(http.MethodGet, rawURL, nil)
		resp, err := cli.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status %d", resp.StatusCode)
		}
		return nil
	}
}

// badSigFn forges a syntactically valid Authorization header with a wrong
// signature. MinIO's verifier rejects with 403 SignatureDoesNotMatch — this
// is the cleanest measure of MinIO's reject-path latency, parallel to the
// proxy bench's bad-sig case.
func badSigFn(endpoint, bucket, key, akid string) func() error {
	tr := &http.Transport{MaxIdleConnsPerHost: 256, MaxConnsPerHost: 256, IdleConnTimeout: 30 * time.Second}
	cli := &http.Client{Transport: tr, Timeout: 30 * time.Second}
	target := strings.TrimRight(endpoint, "/") + "/" + bucket + "/" + key
	return func() error {
		req, _ := http.NewRequest(http.MethodHead, target, nil)
		ts := time.Now().UTC().Format("20060102T150405Z")
		req.Header.Set("X-Amz-Date", ts)
		req.Header.Set("X-Amz-Content-Sha256", "UNSIGNED-PAYLOAD")
		req.Header.Set("Authorization", fmt.Sprintf(
			"AWS4-HMAC-SHA256 Credential=%s/%s/us-east-1/s3/aws4_request, "+
				"SignedHeaders=host;x-amz-content-sha256;x-amz-date, "+
				"Signature=%s",
			akid, ts[:8], strings.Repeat("0", 64),
		))
		resp, err := cli.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode != http.StatusForbidden {
			return fmt.Errorf("expected 403, got %d", resp.StatusCode)
		}
		return nil
	}
}

func runCase(name string, fn func() error, conc int, dur, warm time.Duration) result {
	if warm > 0 {
		warmCtx, warmCancel := context.WithTimeout(context.Background(), warm)
		runWorkers(warmCtx, fn, conc, nil, nil, nil)
		warmCancel()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(dur, cancel)
	defer timer.Stop()

	var ops, errs int64
	samplesCh := make(chan sample, conc*128)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runWorkers(ctx, fn, conc, &ops, &errs, samplesCh)
		close(samplesCh)
	}()

	latencies := make([]time.Duration, 0, 1<<14)
	start := time.Now()
	for s := range samplesCh {
		if s.ok {
			latencies = append(latencies, s.dur)
		}
	}
	wg.Wait()
	elapsed := time.Since(start)
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	return result{
		name:        name,
		concurrency: conc,
		duration:    elapsed,
		ops:         atomic.LoadInt64(&ops),
		errs:        atomic.LoadInt64(&errs),
		latencies:   latencies,
	}
}

func runWorkers(ctx context.Context, fn func() error, conc int,
	ops, errs *int64, samples chan<- sample) {
	var wg sync.WaitGroup
	for i := 0; i < conc; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				start := time.Now()
				err := fn()
				dur := time.Since(start)
				ok := err == nil
				if ops != nil {
					atomic.AddInt64(ops, 1)
				}
				if !ok && errs != nil {
					atomic.AddInt64(errs, 1)
				}
				if samples != nil {
					select {
					case samples <- sample{dur: dur, ok: ok}:
					default:
					}
				}
			}
		}()
	}
	wg.Wait()
}

func runHealthCase(endpoint string, conc int, dur, warm time.Duration) result {
	url := strings.TrimRight(endpoint, "/") + "/minio/health/live"
	tr := &http.Transport{MaxIdleConnsPerHost: 256, MaxConnsPerHost: 256, IdleConnTimeout: 30 * time.Second}
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second}
	fn := func() error {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode != 200 {
			return fmt.Errorf("status %d", resp.StatusCode)
		}
		return nil
	}
	return runCase("MinIO /minio/health/live", fn, conc, dur, warm)
}

func renderMarkdown(results []result, endpoint string, conc int, dur time.Duration) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# MinIO direct benchmark results")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- target: `%s` (S3 API, SigV4)\n", endpoint)
	fmt.Fprintf(&b, "- concurrency: %d\n", conc)
	fmt.Fprintf(&b, "- duration: %s\n", dur)
	fmt.Fprintf(&b, "- container limits: 1 CPU, 200 MiB memory (matched to stowage for an apples-to-apples comparison)\n")
	fmt.Fprintf(&b, "- captured: %s UTC\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Summary")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Endpoint | Conc | Ops | Errs | Throughput (req/s) | Mean (ms) | p50 (ms) | p95 (ms) | p99 (ms) | Min (ms) | Max (ms) |")
	fmt.Fprintln(&b, "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
	for _, r := range results {
		successful := r.ops - r.errs
		fmt.Fprintf(&b, "| `%s` | %d | %d | %d | %.1f | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f |\n",
			r.name,
			r.concurrency,
			successful,
			r.errs,
			r.throughput(),
			ms(r.mean()),
			ms(r.percentile(0.50)),
			ms(r.percentile(0.95)),
			ms(r.percentile(0.99)),
			ms(r.min()),
			ms(r.max()),
		)
	}
	return b.String()
}

func renderJSON(results []result) ([]byte, error) {
	out := make([]jsonResult, 0, len(results))
	for _, r := range results {
		out = append(out, jsonResult{
			Name:          r.name,
			Concurrency:   r.concurrency,
			Ops:           r.ops,
			Errs:          r.errs,
			DurationMS:    float64(r.duration) / float64(time.Millisecond),
			ThroughputRPS: r.throughput(),
			MeanMS:        ms(r.mean()),
			P50MS:         ms(r.percentile(0.50)),
			P95MS:         ms(r.percentile(0.95)),
			P99MS:         ms(r.percentile(0.99)),
			MinMS:         ms(r.min()),
			MaxMS:         ms(r.max()),
		})
	}
	return json.MarshalIndent(out, "", "  ")
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func randUint64() uint64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "miniobench: "+format+"\n", args...)
	os.Exit(1)
}
