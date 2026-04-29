# MinIO direct benchmark results

- target: `http://localhost:19000` (S3 API, SigV4)
- concurrency: 16
- duration: 15s
- container limits: 1 CPU, 200 MiB memory (matched to stowage for an apples-to-apples comparison)
- captured: 2026-04-29T09:53:00Z UTC

## Summary

| Endpoint | Conc | Ops | Errs | Throughput (req/s) | Mean (ms) | p50 (ms) | p95 (ms) | p99 (ms) | Min (ms) | Max (ms) |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| `MinIO ListBuckets` | 16 | 26925 | 0 | 1794.7 | 8.91 | 3.10 | 61.26 | 69.79 | 0.31 | 154.03 |
| `MinIO HeadBucket` | 16 | 27202 | 0 | 1813.3 | 8.82 | 2.76 | 63.59 | 72.40 | 0.33 | 96.81 |
| `MinIO ListObjectsV2` | 16 | 11721 | 0 | 777.4 | 20.48 | 6.70 | 78.38 | 87.29 | 0.78 | 111.53 |
| `MinIO HeadObject` | 16 | 17729 | 0 | 1181.5 | 13.54 | 4.25 | 71.25 | 78.15 | 0.55 | 99.32 |
| `MinIO GetObject 1 KiB` | 16 | 14699 | 0 | 979.5 | 16.33 | 5.16 | 74.51 | 82.77 | 0.65 | 98.37 |
| `MinIO GetObject 1 MiB` | 16 | 3289 | 0 | 218.0 | 73.16 | 80.68 | 191.28 | 277.43 | 2.44 | 481.74 |
| `MinIO GetObject (presigned)` | 16 | 14727 | 0 | 981.0 | 16.31 | 4.41 | 77.44 | 87.23 | 0.61 | 169.80 |
| `MinIO Auth Failure (bad sig)` | 16 | 49572 | 0 | 3304.5 | 4.84 | 1.33 | 14.83 | 67.77 | 0.14 | 89.18 |
| `MinIO PutObject 1 KiB` | 16 | 7846 | 0 | 522.6 | 30.59 | 12.00 | 83.84 | 95.70 | 1.65 | 194.16 |
| `MinIO PutObject 1 MiB` | 16 | 1790 | 0 | 118.5 | 134.68 | 114.13 | 209.68 | 297.00 | 9.16 | 479.78 |
| `MinIO DeleteObject` | 16 | 19829 | 0 | 1321.7 | 12.10 | 3.84 | 69.92 | 75.74 | 0.53 | 170.97 |
| `MinIO /minio/health/live` | 16 | 86564 | 0 | 5770.5 | 2.77 | 0.80 | 5.00 | 60.85 | 0.09 | 79.34 |
