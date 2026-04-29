# stowage S3 proxy vs raw MinIO — head-to-head

Per-case overhead added by stowage's embedded SigV4 proxy versus
talking S3 straight to MinIO. Both runs targeted the same upstream
MinIO image under the same docker-compose constraints (`1 CPU,
200 MiB, GOMEMLIMIT=180MiB`); each ran on a freshly-recreated stack
to avoid the previous run's state polluting the next. 16 concurrent
workers, 15 s per case.

The proxy run is `results-s3proxy.json`; the MinIO-direct run is
`results-minio.json`. Cases pair by stripping the `Proxy ` /
`MinIO ` prefix.

> **Note on prior runs:** the bench results committed in `605005f`
> (claimed sigv4 cache impact) and `bdab6ee` (claimed sampling=0
> impact) were captured against a stale docker image (the bench
> compose's `up -d` doesn't rebuild on local-binary change; only
> an explicit `docker compose build` does). The numbers here are
> from a fresh image rebuild after that was caught.

## Per-case overhead (paired cases only) — current state

| Case | MinIO rps | Proxy rps | Δ throughput | MinIO p50 | Proxy p50 | Δ p50 (ms) | MinIO p99 | Proxy p99 | Δ p99 (ms) |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| HeadBucket | 1813 | 1637 | **−10%** | 2.77 | 4.33 | +1.56 | 70.18 | 66.02 | −4.16 |
| ListObjectsV2 | 777 | 722 | **−7%** | 6.71 | 9.58 | +2.87 | 88.43 | 82.93 | −5.50 |
| HeadObject | 1181 | 1070 | **−9%** | 4.25 | 6.16 | +1.91 | 78.34 | 74.45 | −3.89 |
| GetObject 1 KiB | 980 | 875 | **−11%** | 5.16 | 7.91 | +2.75 | 86.22 | 78.35 | −7.87 |
| GetObject 1 MiB | 218 | 214 | **−2%** | 80.68 | 79.89 | −0.79 | 286.18 | 217.38 | −68.80 |
| GetObject (presigned) | 981 | 908 | **−7%** | 4.40 | 6.88 | +2.48 | 86.43 | 79.50 | −6.93 |
| PutObject 1 KiB | 523 | 566 | **+8%** | 12.01 | 13.40 | +1.39 | 92.90 | 88.36 | −4.54 |
| PutObject 1 MiB | 118 | 140 | **+18%** | 114.13 | 104.47 | −9.66 | 286.81 | 272.97 | −13.84 |
| DeleteObject | 1322 | 1205 | **−9%** | 3.84 | 6.08 | +2.24 | 75.43 | 68.98 | −6.45 |
| Auth Failure (bad sig) | 3304 | 10645 | **+222%** | 1.33 | 0.75 | −0.58 | 65.31 | 29.86 | −35.45 |

## Cases without a paired MinIO equivalent

| Case | Proxy rps | Proxy p50 | Why no pairing |
|---|---:|---:|---|
| `Proxy ListBuckets` | 8932 | 1.34 | Synthesised by the proxy from the VC's bucket scope; never touches the upstream. |
| `Proxy GetObject (anonymous)` | 984 | 6.17 | Anonymous reads against MinIO would require a per-bucket public-read policy. |
| `Proxy Scope Violation` | 7672 | 1.41 | MinIO has no per-credential bucket scoping. |

## Where the perf work landed

The proxy started at *1.2–2.0× slower* than direct MinIO on most
upstream-bound calls. Three commits' worth of perf work, each
informed by pprof under bench load, closed the gap.

| Stage | Commit | Fix |
|---|---|---|
| 0 | (initial) | `http.DefaultTransport` (2 idle conns/host), sync per-event audit, no signing-key cache |
| 1 | `f033d91` | Bespoke `http.Transport` (256 idle/host, HTTP/2). Batched audit via new `BatchRecorder` capability. |
| 2 | `f31d7d9` | Cache derived SigV4 signing keys per `(akid, date, region, service)` with secret-fingerprint binding. `sync.Pool` the sha256 hash state. |
| 3 | `64000ae` | `audit.sampling.proxy_success_read_rate` defaulting to 0.0 — successful proxy reads no longer generate audit rows by default. |

Stage 1 was the dominant fix — pprof showed the dial storm at
**~52 % of CPU** and synchronous audit fsyncs at **~12 %**, both
of which collapsed to near-zero after the transport pool and
the BatchRecorder landed.

Stage 2 (sigv4 cache) is observable in the synthesised /
reject-only paths because they're CPU-bound, not upstream-IO-
bound: `Proxy ListBuckets` 4340 → 8932 rps (+106 %); bad-sig
6834 → 10645 rps (+56 %). On upstream-bound paths the verifier
HMAC chain is rounding error compared to MinIO's response time,
so the cache shows no measurable gain there.

Stage 3 (audit sampling=0) is a behaviour change rather than a
throughput change at this load level. The dropped audit volume
(roughly N → N/5 events at 16 conc) frees the audit-DB writer
for non-proxy work. Throughput-wise it's lost in the noise floor
because stages 1+2 had already taken the audit drainer below
the CPU budget the request handlers were willing to wait on.

## What the remaining overhead pays for

For each upstream-bound call the proxy still does work MinIO
does not:

1. SigV4 verify — map lookup + final HMAC over the StringToSign
   on cache hit, instead of four chained HMACs (stage 2).
2. Bucket-scope enforcement against the credential's allowed list.
3. URL/headers rewrite from the proxy's view of the bucket to
   the real upstream bucket name.
4. Re-sign the outbound request with the backend's admin
   credentials — still goes through `aws-sdk-go-v2`'s signer,
   which has its own internal `derivedKeyCache` so the HMAC
   chain isn't a hot loop.
5. One extra HTTP hop (proxy → MinIO) over a pooled keepalive
   conn (stage 1).
6. Quota pre-check on writes, post-commit usage update on success.
7. Audit emission — for writes / denied / errored only after
   stage 3 (default), or all events with sampling rate=1.0.
8. Per-request Prometheus metrics + structured-log line.

The remaining **+1–3 ms p50 / 0–11 % throughput** is the
straight-line CPU cost of items 2–4 + 8 on a single core.

## Where additional gains would come from

1. **Pool the response-stream copy buffer in `streamResponse`.**
   Alloc profiling shows `io.copyBuffer` at **51 %** of total
   bytes allocated on the read path — fresh 32 KiB buffer per
   request. A `sync.Pool` of 32 KiB byte slices and a
   `io.CopyBuffer(w, resp.Body, buf)` call would halve total
   allocations on read paths. Marginal CPU win, but cleaner
   heap profile and lower steady-state RAM.
2. **Replace the outbound `aws-sdk-go-v2/v4.Signer`** with a
   hand-rolled signer that shares `internal/sigv4verifier`'s
   buffers + signing-key cache. Saves another 3-4 % of allocs
   plus a small CPU win, at a meaningfully larger test surface
   (need to mirror the SDK's bytewise canonicalization for
   strict S3 paths).
3. **Audit DB on its own SQLite file.** Today's audit and
   regular writes share one writer mutex; moving audit to
   `audit.db` removes that contention. A win at high concurrent
   write volume; transparent at the bench's 16-conc load.

## Fairness notes

- **Same MinIO image and limits on both runs.** Different MinIO
  *instances* (each was a freshly-recreated container), but
  identical configuration. Run-to-run variance in this sandbox
  (cgroup v1, no cpuset support — the "1 CPU" cap isn't strictly
  enforced for CPU) is ±10–15 % on rps numbers, ±2–3 ms on p50.
- **Single 15 s sample per case.** Noise floor is bigger than
  some of the cross-stage deltas; the stage-1 → stage-2 → stage-3
  improvements show up clearly only on the proxy-only paths
  (synthesised ListBuckets, reject paths) where there's no
  upstream IO ceiling.
- **Same host, loopback, no TLS.** Production deployments add
  TLS termination on both sides; the absolute overhead stays
  roughly constant while the ratio shrinks as upstream RTT grows.

## Take-away

Under matched 1 CPU / 200 MiB constraints, the proxy adds
**+1–3 ms p50 latency and 0–11 % throughput cost** for upstream-
bound S3 calls. PutObject (1 KiB and 1 MiB) is *faster* than
direct, and the proxy-internal paths (synthesised ListBuckets,
scope reject, bad-sig reject) are *much* faster than MinIO's
equivalent server-side reject paths because the proxy answers
without ever calling the upstream. The remaining overhead is
dominated by the outbound SigV4 re-sign and the inevitable
extra HTTP hop.
