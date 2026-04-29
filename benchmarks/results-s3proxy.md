# stowage S3 proxy benchmark results

- target: `http://localhost:18090`
- concurrency: 16
- duration: 15s
- container limits: 1 CPU, 200 MiB memory (applied to **both** stowage and the upstream MinIO)
- captured: 2026-04-29T09:49:24Z UTC

## Summary

| Endpoint | Conc | Ops | Errs | Throughput (req/s) | Mean (ms) | p50 (ms) | p95 (ms) | p99 (ms) | Min (ms) | Max (ms) |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `Proxy ListBuckets` | 16 | 133995 | 0 | 8932.0 | 1.79 | 1.34 | 4.54 | 6.15 | 0.14 | 14.43 |
| `Proxy HeadBucket` | 16 | 24631 | 0 | 1636.5 | 9.77 | 4.33 | 55.71 | 66.02 | 0.56 | 140.78 |
| `Proxy ListObjectsV2` | 16 | 10837 | 0 | 722.0 | 22.15 | 9.58 | 73.50 | 82.93 | 1.10 | 98.12 |
| `Proxy HeadObject` | 16 | 16058 | 0 | 1070.3 | 14.94 | 6.16 | 65.87 | 74.45 | 0.96 | 100.40 |
| `Proxy GetObject 1 KiB` | 16 | 13187 | 0 | 875.4 | 18.26 | 7.91 | 69.07 | 78.35 | 0.96 | 157.56 |
| `Proxy GetObject 1 MiB` | 16 | 3229 | 0 | 214.0 | 74.56 | 79.89 | 182.52 | 217.38 | 4.49 | 396.01 |
| `Proxy GetObject (presigned)` | 16 | 13687 | 0 | 907.8 | 17.54 | 6.88 | 71.21 | 79.50 | 0.84 | 103.60 |
| `Proxy GetObject (anonymous)` | 16 | 14762 | 0 | 983.8 | 16.26 | 6.17 | 70.44 | 78.03 | 0.77 | 100.81 |
| `Proxy Auth Failure (bad sig)` | 16 | 159692 | 0 | 10645.5 | 1.50 | 0.75 | 3.28 | 29.86 | 0.07 | 46.98 |
| `Proxy Scope Violation` | 16 | 115084 | 0 | 7672.0 | 2.08 | 1.41 | 5.37 | 15.96 | 0.14 | 34.22 |
| `Proxy PutObject 1 KiB` | 16 | 8500 | 0 | 566.1 | 28.24 | 13.40 | 76.99 | 88.36 | 2.12 | 183.56 |
| `Proxy PutObject 1 MiB` | 16 | 2097 | 0 | 139.5 | 114.57 | 104.47 | 196.69 | 272.97 | 13.71 | 458.93 |
| `Proxy DeleteObject` | 16 | 18153 | 0 | 1205.5 | 13.27 | 6.08 | 61.34 | 68.98 | 0.84 | 93.71 |

## Notes
- `Proxy ListBuckets` is synthesised by the proxy from the credential's bucket scopes and never reaches MinIO; it is a pure measure of SigV4 verify + cache lookup + XML render.
- `Proxy GetObject (presigned)` reuses a single 2-hour-valid URL for every iteration so the timing is dominated by proxy work, not by client-side signing.
- `Proxy GetObject (anonymous)` exercises the unauthenticated fast-path: no SigV4 verify, just binding lookup + per-IP rate-limit + forward.
- `Proxy Auth Failure (bad sig)` and `Proxy Scope Violation` are reject-path cases: the "successful" iteration is one where the proxy returned 403 without ever calling the upstream.
- PutObject cases use a unique key per request so the upstream sees real writes; the bucket is left dirty on purpose so a subsequent run can be compared against the same probe set.
