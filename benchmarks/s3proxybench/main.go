// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Comprehensive benchmark for stowage's embedded S3 SigV4 proxy.
//
// Where ../bench.go targets the dashboard (`/api/*`) and ../miniobench
// targets MinIO directly, this binary targets the proxy listener
// (`s3_proxy.listen`, port 18090 in the bench compose). It logs into the
// dashboard as admin to mint a virtual credential and an anonymous
// binding, then drives a fan of S3 operations against the proxy with the
// AWS SDK v2 SigV4 client. The result set isolates proxy overhead — the
// MinIO upstream is shared with ../miniobench so the deltas are
// apples-to-apples.
//
// Cases (each measured for `-duration` at `-concurrency` workers):
//
//   - ListBuckets         service-level, synthesised in-proxy (no upstream call)
//   - HeadBucket          bucket-level, no body
//   - ListObjectsV2       common dashboard pattern
//   - HeadObject          metadata-only object op
//   - GetObject 1 KiB     small read; latency-dominated
//   - GetObject 1 MiB     larger read; throughput-dominated
//   - PutObject 1 KiB     small write (signed payload)
//   - PutObject 1 MiB     larger write (streams aws-chunked, exercises the
//     signature-verifying chunked reader)
//   - DeleteObject        write op with empty response
//   - GetObject presigned exercises the presigned auth-mode path
//   - GetObject anonymous exercises the unauthenticated fast-path
//   - Auth Failure        expects 403; measures pure SigV4-reject latency
//   - Scope Violation     valid signature, bucket out-of-scope; expects 403
//
// Run from the host with the bench compose stack already up:
//
//	go run ./benchmarks/s3proxybench \
//	    -dashboard http://localhost:18080 \
//	    -proxy http://localhost:18090 \
//	    -username admin -password 'B3nchm@rk-Pa55w0rd'
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
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
	successful := r.ops - r.errs
	return float64(successful) / r.duration.Seconds()
}

// jsonResult mirrors ../bench.go's structure so a future check-style
// regression gate can ingest the proxy bench output unchanged.
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
	dashboard := flag.String("dashboard", "http://localhost:18080", "stowage dashboard base URL (used for VC + binding setup)")
	proxy := flag.String("proxy", "http://localhost:18090", "stowage S3 proxy base URL (target of the bench)")
	username := flag.String("username", "admin", "admin username")
	password := flag.String("password", "", "admin password (required)")
	backendID := flag.String("backend", "local-minio", "backend id the VC + binding live under")
	privateBucket := flag.String("private-bucket", "bench-proxy", "bucket reachable via the SigV4 VC")
	publicBucket := flag.String("public-bucket", "bench-public", "bucket reachable anonymously via a ReadOnly binding")
	otherBucket := flag.String("other-bucket", "bench-other", "bucket NOT in the VC scope, used by the scope-violation case")
	region := flag.String("region", "us-east-1", "S3 region for SigV4 (MinIO is region-agnostic but the signer needs one)")
	duration := flag.Duration("duration", 15*time.Second, "per-case benchmark duration")
	concurrency := flag.Int("concurrency", 16, "concurrent workers per case")
	warmup := flag.Duration("warmup", 2*time.Second, "warmup duration before each case")
	output := flag.String("output", "results-s3proxy.md", "markdown output path (use - for stdout only)")
	jsonOutput := flag.String("json", "", "optional JSON output path")
	skipWrites := flag.Bool("skip-writes", false, "skip Put/Delete cases (read-only run)")
	flag.Parse()

	if *password == "" {
		fail("must supply -password")
	}

	jar, _ := cookiejar.New(nil)
	dashClient := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 64,
			MaxConnsPerHost:     64,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	if err := waitReady(*dashboard, 60*time.Second); err != nil {
		fail("dashboard /readyz: %v", err)
	}
	fmt.Fprintln(os.Stderr, "dashboard is ready")

	csrf, err := login(dashClient, *dashboard, *username, *password)
	if err != nil {
		fail("login: %v", err)
	}
	fmt.Fprintln(os.Stderr, "logged in, csrf token captured")

	for _, b := range []string{*privateBucket, *publicBucket} {
		if err := ensureBucket(dashClient, *dashboard, csrf, *backendID, b); err != nil {
			fail("ensure bucket %s: %v", b, err)
		}
	}

	akid, secretKey, err := mintCredential(dashClient, *dashboard, csrf, *backendID,
		[]string{*privateBucket, *publicBucket})
	if err != nil {
		fail("mint credential: %v", err)
	}
	fmt.Fprintf(os.Stderr, "minted virtual credential %s scoped to %v\n", akid,
		[]string{*privateBucket, *publicBucket})

	if err := upsertAnonBinding(dashClient, *dashboard, csrf, *backendID, *publicBucket); err != nil {
		fail("anonymous binding: %v", err)
	}
	fmt.Fprintf(os.Stderr, "anonymous binding active for %s/%s\n", *backendID, *publicBucket)

	// Make sure the proxy's in-memory cache has picked up the new VC + binding
	// before we start. The reload is fired synchronously by the create
	// handlers, so this is just a belt-and-braces sanity probe.
	if err := waitReady(*proxy, 30*time.Second); err != nil {
		fail("proxy readiness: %v", err)
	}

	ctx := context.Background()

	httpClient := awshttp.NewBuildableClient().WithTransportOptions(func(t *http.Transport) {
		t.MaxIdleConnsPerHost = 256
		t.MaxConnsPerHost = 256
	})
	signedCfg := aws.Config{
		Region:       *region,
		Credentials:  credentials.NewStaticCredentialsProvider(akid, secretKey, ""),
		BaseEndpoint: aws.String(*proxy),
		HTTPClient:   httpClient,
	}
	signed := s3.NewFromConfig(signedCfg, func(o *s3.Options) { o.UsePathStyle = true })

	// Seed probe objects via the proxy itself (using the just-issued VC) so
	// the seed path is exercised before the steady-state PutObject case.
	const (
		probe1KKey = "bench/probe-1k.bin"
		probe1MKey = "bench/probe-1m.bin"
		publicKey  = "bench/public-probe.bin"
	)
	probe1K := bytes.Repeat([]byte("x"), 1024)
	probe1M := bytes.Repeat([]byte("y"), 1<<20)
	if err := putObject(ctx, signed, *privateBucket, probe1KKey, probe1K); err != nil {
		fail("seed %s: %v", probe1KKey, err)
	}
	if err := putObject(ctx, signed, *privateBucket, probe1MKey, probe1M); err != nil {
		fail("seed %s: %v", probe1MKey, err)
	}
	if err := putObject(ctx, signed, *publicBucket, publicKey, probe1K); err != nil {
		fail("seed %s: %v", publicKey, err)
	}
	fmt.Fprintln(os.Stderr, "seeded probe objects")

	// Pre-built presigned URL — re-presigning every iteration would dominate
	// the case latency with client-side signing instead of proxy work.
	presigner := s3.NewPresignClient(signed)
	presigned, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(*privateBucket),
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
			name: "Proxy ListBuckets",
			fn: func() error {
				_, err := signed.ListBuckets(ctx, &s3.ListBucketsInput{})
				return err
			},
		},
		{
			name: "Proxy HeadBucket",
			fn: func() error {
				_, err := signed.HeadBucket(ctx, &s3.HeadBucketInput{
					Bucket: aws.String(*privateBucket),
				})
				return err
			},
		},
		{
			name: "Proxy ListObjectsV2",
			fn: func() error {
				_, err := signed.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
					Bucket: aws.String(*privateBucket),
				})
				return err
			},
		},
		{
			name: "Proxy HeadObject",
			fn: func() error {
				_, err := signed.HeadObject(ctx, &s3.HeadObjectInput{
					Bucket: aws.String(*privateBucket),
					Key:    aws.String(probe1KKey),
				})
				return err
			},
		},
		{
			name: "Proxy GetObject 1 KiB",
			fn:   getFn(ctx, signed, *privateBucket, probe1KKey),
		},
		{
			name: "Proxy GetObject 1 MiB",
			fn:   getFn(ctx, signed, *privateBucket, probe1MKey),
		},
		{
			name: "Proxy GetObject (presigned)",
			fn:   presignedGetFn(presigned.URL),
		},
		{
			name: "Proxy GetObject (anonymous)",
			fn:   anonGetFn(*proxy, *publicBucket, publicKey),
		},
		{
			name: "Proxy Auth Failure (bad sig)",
			fn:   badSigFn(*proxy, *privateBucket, probe1KKey, akid),
		},
		{
			name: "Proxy Scope Violation",
			fn: func() error {
				// Signature is valid; bucket is outside the VC scope. The
				// proxy enforces scope before forwarding, so a 403 here means
				// the proxy short-circuited correctly. The SDK surfaces it
				// as an error — for this case, that is success.
				_, err := signed.HeadBucket(ctx, &s3.HeadBucketInput{
					Bucket: aws.String(*otherBucket),
				})
				return invertErr(err)
			},
		},
	}

	if !*skipWrites {
		// PutObject cases use a unique key per request so concurrent workers
		// don't trample each other's content-length accounting on the proxy
		// side, and so quotas aren't double-counted on a stable key.
		var putCounter atomic.Uint64
		mkPutFn := func(payload []byte, prefix string) func() error {
			return func() error {
				idx := putCounter.Add(1)
				key := fmt.Sprintf("%s/%d.bin", prefix, idx)
				return putObject(ctx, signed, *privateBucket, key, payload)
			}
		}
		cases = append(cases,
			struct {
				name string
				fn   func() error
			}{
				name: "Proxy PutObject 1 KiB",
				fn:   mkPutFn(probe1K, "bench/put-1k"),
			},
			struct {
				name string
				fn   func() error
			}{
				name: "Proxy PutObject 1 MiB",
				fn:   mkPutFn(probe1M, "bench/put-1m"),
			},
			struct {
				name string
				fn   func() error
			}{
				// DeleteObject on a never-existing key returns 204 in S3.
				// That keeps the case stable across iterations without
				// tying its rate to a producer that refills keys.
				name: "Proxy DeleteObject",
				fn: func() error {
					key := fmt.Sprintf("bench/del/%d-%d.bin", time.Now().UnixNano(), randUint64())
					_, err := signed.DeleteObject(ctx, &s3.DeleteObjectInput{
						Bucket: aws.String(*privateBucket),
						Key:    aws.String(key),
					})
					return err
				},
			},
		)
	}

	results := make([]result, 0, len(cases))
	for _, c := range cases {
		fmt.Fprintf(os.Stderr, "running %s @ %d concurrent for %s\n", c.name, *concurrency, *duration)
		results = append(results, runCase(c.name, c.fn, *concurrency, *duration, *warmup))
	}

	report := renderMarkdown(results, *proxy, *concurrency, *duration)
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

func anonGetFn(proxyBase, bucket, key string) func() error {
	tr := &http.Transport{MaxIdleConnsPerHost: 256, MaxConnsPerHost: 256, IdleConnTimeout: 30 * time.Second}
	cli := &http.Client{Transport: tr, Timeout: 30 * time.Second}
	target := strings.TrimRight(proxyBase, "/") + "/" + bucket + "/" + key
	return func() error {
		req, _ := http.NewRequest(http.MethodGet, target, nil)
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
// signature. The proxy's verifier rejects with 403 SignatureDoesNotMatch
// without ever touching the upstream — this is the cleanest measure of
// reject-path latency.
func badSigFn(proxyBase, bucket, key, akid string) func() error {
	tr := &http.Transport{MaxIdleConnsPerHost: 256, MaxConnsPerHost: 256, IdleConnTimeout: 30 * time.Second}
	cli := &http.Client{Transport: tr, Timeout: 30 * time.Second}
	target := strings.TrimRight(proxyBase, "/") + "/" + bucket + "/" + key
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

// invertErr swaps "got error" and "got nil" so reject-path cases can be
// counted as successes when the proxy correctly refuses the request. Used
// by the scope-violation case where any successful upstream call would be
// a security regression.
func invertErr(err error) error {
	if err == nil {
		return errors.New("expected proxy to reject, got 200")
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

// --- dashboard helpers (login, bucket create, VC mint, anon binding) ---

func waitReady(base string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// /readyz is the dashboard probe; the proxy listener doesn't expose
		// one, so for the proxy URL we accept any 4xx (the proxy answers
		// with a SigV4 error on `GET /`, which is "alive" enough for us).
		resp, err := http.Get(base + "/readyz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		// Try a bare GET in case this is the proxy listener.
		resp, err = http.Get(base + "/")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode > 0 {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("timeout waiting for endpoint to answer")
}

func login(c *http.Client, base, user, pass string) (string, error) {
	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	req, _ := http.NewRequest(http.MethodPost, base+"/auth/login/local", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}
	io.Copy(io.Discard, resp.Body)
	u, _ := url.Parse(base)
	for _, ck := range c.Jar.Cookies(u) {
		if ck.Name == "stowage_csrf" {
			return ck.Value, nil
		}
	}
	return "", errors.New("no csrf cookie set")
}

func ensureBucket(c *http.Client, base, csrf, backendID, bucket string) error {
	body, _ := json.Marshal(map[string]string{"name": bucket})
	req, _ := http.NewRequest(http.MethodPost,
		fmt.Sprintf("%s/api/backends/%s/buckets", base, backendID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent, http.StatusConflict:
		io.Copy(io.Discard, resp.Body)
		return nil
	default:
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}
}

func mintCredential(c *http.Client, base, csrf, backendID string, buckets []string) (akid, secret string, err error) {
	payload, _ := json.Marshal(map[string]any{
		"backend_id":  backendID,
		"buckets":     buckets,
		"description": "s3proxybench",
	})
	req, _ := http.NewRequest(http.MethodPost,
		base+"/api/admin/s3-credentials", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := c.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}
	var dto struct {
		AccessKey string `json:"access_key"`
		SecretKey string `json:"secret_key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&dto); err != nil {
		return "", "", err
	}
	if dto.AccessKey == "" || dto.SecretKey == "" {
		return "", "", errors.New("create response missing access_key or secret_key")
	}
	return dto.AccessKey, dto.SecretKey, nil
}

func upsertAnonBinding(c *http.Client, base, csrf, backendID, bucket string) error {
	rps := 100000
	payload, _ := json.Marshal(map[string]any{
		"backend_id":        backendID,
		"bucket":            bucket,
		"mode":              "ReadOnly",
		"per_source_ip_rps": rps,
	})
	req, _ := http.NewRequest(http.MethodPost,
		base+"/api/admin/s3-anonymous", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(b))
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

// --- output ---

func renderMarkdown(results []result, target string, conc int, dur time.Duration) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# stowage S3 proxy benchmark results")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- target: `%s`\n", target)
	fmt.Fprintf(&b, "- concurrency: %d\n", conc)
	fmt.Fprintf(&b, "- duration: %s\n", dur)
	fmt.Fprintf(&b, "- container limits: 1 CPU, 200 MiB memory (applied to **both** stowage and the upstream MinIO)\n")
	fmt.Fprintf(&b, "- captured: %s UTC\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Summary")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Endpoint | Conc | Ops | Errs | Throughput (req/s) | Mean (ms) | p50 (ms) | p95 (ms) | p99 (ms) | Min (ms) | Max (ms) |")
	fmt.Fprintln(&b, "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
	for _, r := range results {
		successful := r.ops - r.errs
		fmt.Fprintf(&b, "| `%s` | %d | %d | %d | %.1f | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f |\n",
			r.name, r.concurrency, successful, r.errs, r.throughput(),
			ms(r.mean()), ms(r.percentile(0.50)), ms(r.percentile(0.95)),
			ms(r.percentile(0.99)), ms(r.min()), ms(r.max()),
		)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Notes")
	fmt.Fprintln(&b, "- `Proxy ListBuckets` is synthesised by the proxy from the credential's bucket scopes and never reaches MinIO; it is a pure measure of SigV4 verify + cache lookup + XML render.")
	fmt.Fprintln(&b, "- `Proxy GetObject (presigned)` reuses a single 2-hour-valid URL for every iteration so the timing is dominated by proxy work, not by client-side signing.")
	fmt.Fprintln(&b, "- `Proxy GetObject (anonymous)` exercises the unauthenticated fast-path: no SigV4 verify, just binding lookup + per-IP rate-limit + forward.")
	fmt.Fprintln(&b, "- `Proxy Auth Failure (bad sig)` and `Proxy Scope Violation` are reject-path cases: the \"successful\" iteration is one where the proxy returned 403 without ever calling the upstream.")
	fmt.Fprintln(&b, "- PutObject cases use a unique key per request so the upstream sees real writes; the bucket is left dirty on purpose so a subsequent run can be compared against the same probe set.")
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
	fmt.Fprintf(os.Stderr, "s3proxybench: "+format+"\n", args...)
	os.Exit(1)
}
