// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// stowage performance benchmark.
//
// Hits a running stowage instance with N concurrent workers per endpoint
// for a fixed duration and reports throughput, latency, and total ops.
//
// Run from the host while stowage runs in a CPU/memory-constrained container:
//
//	go run ./bench.go -base http://localhost:18080 \
//	    -username admin -password 'B3nchm@rk-Pa55w0rd!' \
//	    -duration 15s -concurrency 16
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type benchCase struct {
	name   string
	method string
	path   string
	body   []byte
	ctype  string
	csrf   bool
	expect int
}

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
	idx := int(float64(len(r.latencies)-1) * p)
	return r.latencies[idx]
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
	// Successful-ops/sec — failed connections don't represent useful work,
	// so we exclude them from throughput. The Errs column makes the failure
	// count visible separately.
	successful := r.ops - r.errs
	return float64(successful) / r.duration.Seconds()
}

func main() {
	base := flag.String("base", "http://localhost:8080", "stowage base URL")
	username := flag.String("username", "admin", "admin username")
	password := flag.String("password", "", "admin password (required)")
	duration := flag.Duration("duration", 15*time.Second, "per-endpoint benchmark duration")
	concurrency := flag.Int("concurrency", 16, "concurrent workers per endpoint")
	// argon2id in this codebase uses m=65536 (64 MiB) per hash. At a 200 MiB
	// container limit even 2 concurrent verifications can OOM-kill stowage
	// (two 64 MiB buffers + Go runtime + sqlite working set), so we default
	// to 1. Bump this only if the container limit is raised in tandem.
	loginConcurrency := flag.Int("login-concurrency", 1, "concurrent workers for login (argon2id ~64MiB/hash)")
	loginDuration := flag.Duration("login-duration", 10*time.Second, "duration for the login benchmark")
	backendID := flag.String("backend", "local-minio", "backend id to exercise")
	bucket := flag.String("bucket", "bench", "bucket name to use (created if missing)")
	objectKey := flag.String("object", "bench/probe.bin", "object key to upload + read")
	objectSize := flag.Int("object-size", 1024, "size of seeded object in bytes")
	output := flag.String("output", "results.md", "output markdown file (use - for stdout only)")
	jsonOutput := flag.String("json", "", "optional path to also write structured JSON results (consumed by ./check)")
	skipS3 := flag.Bool("skip-s3", false, "skip endpoints that require an S3 backend")
	warmup := flag.Duration("warmup", 2*time.Second, "warmup duration before each case")
	flag.Parse()

	if *password == "" {
		fail("must supply -password")
	}

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 256,
			MaxConnsPerHost:     256,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	if err := waitReady(*base, 60*time.Second); err != nil {
		fail("readyz: %v", err)
	}
	fmt.Fprintln(os.Stderr, "stowage is ready")

	csrf, err := login(client, *base, *username, *password)
	if err != nil {
		fail("login: %v", err)
	}
	fmt.Fprintln(os.Stderr, "logged in, csrf token captured")

	if !*skipS3 {
		if err := ensureBucket(client, *base, csrf, *backendID, *bucket); err != nil {
			fail("ensure bucket: %v", err)
		}
		if err := uploadProbe(client, *base, csrf, *backendID, *bucket, *objectKey, *objectSize); err != nil {
			fail("seed object: %v", err)
		}
		fmt.Fprintf(os.Stderr, "seeded s3://%s/%s/%s (%d bytes)\n", *backendID, *bucket, *objectKey, *objectSize)
	}

	// Build cases.
	listObjPath := fmt.Sprintf("/api/backends/%s/buckets/%s/objects?prefix=", *backendID, *bucket)
	getObjPath := fmt.Sprintf("/api/backends/%s/buckets/%s/object?key=%s", *backendID, *bucket, url.QueryEscape(*objectKey))
	headObjPath := getObjPath

	cases := []benchCase{
		{name: "GET /healthz", method: "GET", path: "/healthz", expect: 200},
		{name: "GET /readyz", method: "GET", path: "/readyz", expect: 200},
		{name: "GET /metrics", method: "GET", path: "/metrics", expect: 200},
		{name: "GET /api/auth/config", method: "GET", path: "/api/auth/config", expect: 200},
		{name: "GET /api/me", method: "GET", path: "/api/me", expect: 200},
		{name: "GET /api/backends", method: "GET", path: "/api/backends", expect: 200},
	}
	if !*skipS3 {
		cases = append(cases,
			benchCase{name: "GET /api/backends/{id}/buckets", method: "GET", path: "/api/backends/" + *backendID + "/buckets", expect: 200},
			benchCase{name: "GET /api/backends/{id}/.../objects", method: "GET", path: listObjPath, expect: 200},
			benchCase{name: "HEAD /api/backends/{id}/object", method: "HEAD", path: headObjPath, expect: 200},
			benchCase{name: "GET /api/backends/{id}/object", method: "GET", path: getObjPath, expect: 200},
		)
	}

	results := make([]result, 0, len(cases)+1)
	for _, c := range cases {
		fmt.Fprintf(os.Stderr, "running %s @ %d concurrent for %s\n", c.name, *concurrency, *duration)
		r := runCase(client, *base, csrf, c, *concurrency, *duration, *warmup)
		results = append(results, r)
	}

	// Login case uses its own jar per request (no session reuse).
	fmt.Fprintf(os.Stderr, "running POST /auth/login/local @ %d concurrent for %s (argon2id is heavy)\n",
		*loginConcurrency, *loginDuration)
	loginResult := runLoginCase(*base, *username, *password, *loginConcurrency, *loginDuration)
	results = append(results, loginResult)

	report := renderMarkdown(results, *base, *concurrency, *duration, *loginConcurrency, *loginDuration)
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

// jsonResult is the on-disk shape consumed by ./check. Field names are
// kept short and stable; renaming any of them is a breaking change for
// the baseline file checked into the repo.
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

func waitReady(base string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/readyz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return errors.New("timeout waiting for /readyz")
}

func login(c *http.Client, base, user, pass string) (string, error) {
	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	req, _ := http.NewRequest("POST", base+"/auth/login/local", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
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
	// PUT-style create.
	body, _ := json.Marshal(map[string]string{"name": bucket})
	req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/backends/%s/buckets", base, backendID), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200, 201, 204, 409: // exists is ok
		io.Copy(io.Discard, resp.Body)
		return nil
	default:
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create bucket status %d: %s", resp.StatusCode, string(b))
	}
}

func uploadProbe(c *http.Client, base, csrf, backendID, bucket, key string, size int) error {
	// The upload endpoint accepts multipart/form-data with a "file" part and
	// a "key" form value (see handleUploadObject). Build that here.
	payload := bytes.Repeat([]byte("x"), size)
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	if err := mw.WriteField("key", key); err != nil {
		return err
	}
	fw, err := mw.CreateFormFile("file", "probe.bin")
	if err != nil {
		return err
	}
	if _, err := fw.Write(payload); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	u := fmt.Sprintf("%s/api/backends/%s/buckets/%s/object", base, backendID, bucket)
	req, _ := http.NewRequest("POST", u, bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("upload status %d: %s", resp.StatusCode, string(b))
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}

func runCase(c *http.Client, base, csrf string, bc benchCase, conc int, dur, warm time.Duration) result {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if warm > 0 {
		warmCtx, warmCancel := context.WithTimeout(context.Background(), warm)
		runWorkers(warmCtx, c, base, csrf, bc, conc, nil, nil, nil)
		warmCancel()
	}

	timer := time.AfterFunc(dur, cancel)
	defer timer.Stop()

	var ops int64
	var errs int64
	samplesCh := make(chan sample, conc*128)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runWorkers(ctx, c, base, csrf, bc, conc, &ops, &errs, samplesCh)
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
		name:        bc.name,
		concurrency: conc,
		duration:    elapsed,
		ops:         atomic.LoadInt64(&ops),
		errs:        atomic.LoadInt64(&errs),
		latencies:   latencies,
	}
}

func runWorkers(ctx context.Context, c *http.Client, base, csrf string, bc benchCase, conc int,
	ops, errs *int64, samples chan<- sample) {
	var wg sync.WaitGroup
	for i := 0; i < conc; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				start := time.Now()
				ok := doRequest(c, base, csrf, bc)
				dur := time.Since(start)
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

func doRequest(c *http.Client, base, csrf string, bc benchCase) bool {
	var body io.Reader
	if bc.body != nil {
		body = bytes.NewReader(bc.body)
	}
	req, err := http.NewRequest(bc.method, base+bc.path, body)
	if err != nil {
		return false
	}
	if bc.ctype != "" {
		req.Header.Set("Content-Type", bc.ctype)
	}
	if bc.csrf {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	resp, err := c.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if bc.expect != 0 && resp.StatusCode != bc.expect {
		return false
	}
	return true
}

// runLoginCase exercises POST /auth/login/local. The endpoint is guarded by
// a hardcoded 10-per-15min per-IP limiter (see internal/server/server.go,
// auth.NewRateLimiter(10, 15*time.Minute)), so we stop each worker as soon
// as it sees a 429: the meaningful number is per-login latency, not
// sustained throughput.
func runLoginCase(base, user, pass string, conc int, dur time.Duration) result {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(dur, cancel)
	defer timer.Stop()

	var ops, errs, ratelimited int64
	samplesCh := make(chan sample, conc*128)

	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})

	var wg sync.WaitGroup
	for i := 0; i < conc; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr := &http.Transport{MaxIdleConnsPerHost: 16, IdleConnTimeout: 30 * time.Second}
			client := &http.Client{Transport: tr, Timeout: 30 * time.Second}
			for ctx.Err() == nil {
				start := time.Now()
				req, _ := http.NewRequest("POST", base+"/auth/login/local", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(req)
				lat := time.Since(start)
				if err != nil {
					atomic.AddInt64(&ops, 1)
					atomic.AddInt64(&errs, 1)
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				atomic.AddInt64(&ops, 1)
				switch resp.StatusCode {
				case 200:
					select {
					case samplesCh <- sample{dur: lat, ok: true}:
					default:
					}
				case 429:
					atomic.AddInt64(&ratelimited, 1)
					// Server-side limiter has tripped; further attempts are
					// just empty calls. Stop this worker.
					return
				default:
					atomic.AddInt64(&errs, 1)
				}
			}
		}()
	}
	go func() { wg.Wait(); close(samplesCh) }()

	latencies := make([]time.Duration, 0, 1<<10)
	start := time.Now()
	for s := range samplesCh {
		if s.ok {
			latencies = append(latencies, s.dur)
		}
	}
	elapsed := time.Since(start)
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	r := result{
		name:        "POST /auth/login/local",
		concurrency: conc,
		duration:    elapsed,
		ops:         int64(len(latencies)), // count only successes — the 429s are an inherent design limit
		errs:        atomic.LoadInt64(&errs),
		latencies:   latencies,
	}
	fmt.Fprintf(os.Stderr, "  login: %d successful, %d 429-rate-limited, %d other errors\n",
		len(latencies), atomic.LoadInt64(&ratelimited), atomic.LoadInt64(&errs))
	return r
}

func renderMarkdown(results []result, base string, conc int, dur time.Duration, lconc int, ldur time.Duration) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# stowage benchmark results")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- target: `%s`\n", base)
	fmt.Fprintf(&b, "- concurrency (default): %d\n", conc)
	fmt.Fprintf(&b, "- duration (default): %s\n", dur)
	fmt.Fprintf(&b, "- concurrency (login): %d\n", lconc)
	fmt.Fprintf(&b, "- duration (login): %s\n", ldur)
	fmt.Fprintf(&b, "- container limits: 1 CPU, 200 MiB memory (applied to **both** stowage and the upstream MinIO for an apples-to-apples run)\n")
	fmt.Fprintf(&b, "- captured: %s UTC\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Summary")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Endpoint | Conc | Ops | Errs | Throughput (req/s) | Mean (ms) | p50 (ms) | p95 (ms) | p99 (ms) | Min (ms) | Max (ms) |")
	fmt.Fprintln(&b, "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
	for _, r := range results {
		fmt.Fprintf(&b, "| `%s` | %d | %d | %d | %.1f | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f |\n",
			r.name,
			r.concurrency,
			r.ops,
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
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Notes")
	fmt.Fprintln(&b, "- Throughput = successful ops / wall duration of the case.")
	fmt.Fprintln(&b, "- Latencies are computed only over successful responses (status == expected).")
	fmt.Fprintln(&b, "- `POST /auth/login/local` is capped by a hardcoded 10-attempts / 15-min /")
	fmt.Fprintln(&b, "  IP limiter in `internal/server/server.go`; the bench worker stops after")
	fmt.Fprintln(&b, "  the first 429 so the latency / throughput row reflects only the")
	fmt.Fprintln(&b, "  successful attempts. argon2id verification uses `m=65536` (~64 MiB)")
	fmt.Fprintln(&b, "  per hash, so login concurrency cannot safely exceed 1 inside a 200 MiB")
	fmt.Fprintln(&b, "  container without OOM-killing the server.")
	fmt.Fprintln(&b, "- Object endpoints are exercised against a 1 KiB seeded object on a co-located MinIO backend.")
	fmt.Fprintln(&b, "- `GET /metrics` is the slowest read because Prometheus has to serialise")
	fmt.Fprintln(&b, "  every request-histogram bucket plus the Go runtime collectors on each")
	fmt.Fprintln(&b, "  scrape. A 5–15s scrape interval in production keeps it negligible.")
	return b.String()
}

func ms(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "bench: "+format+"\n", args...)
	os.Exit(1)
}
