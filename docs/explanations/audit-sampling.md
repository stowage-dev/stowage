---
type: explanation
---

# Audit sampling tradeoffs

Successful read-shaped proxy events are not recorded by default.
Writes, deletes, and any non-2xx response always are. This page
explains why.

## The numbers

Under bench load:

- The proxy handles ~875 rps for `GetObject 1 KiB` per single-CPU
  process.
- Each request would generate one audit row.
- pprof showed audit insertion consuming ~28 % of the proxy's
  available CPU even after the BatchRecorder fix (which moved sync
  fsync overhead off the request path).
- Successful reads dominate that traffic: in a typical workload
  read:write is somewhere between 5:1 and 100:1.

## The knob

```yaml
audit:
  sampling:
    proxy_success_read_rate: 0.0
```

Default 0.0. Range 0.0 to 1.0. Documented in
[`internal/config/config.go`](https://github.com/stowage-dev/stowage/blob/main/internal/config/config.go).

| Value | Effect |
|---|---|
| 0.0 | Skip every successful proxy read. **Default.** |
| 1.0 | Record every event. |
| 0.1 | Record 10% of successful reads (random). |

## What's always recorded regardless

- Writes: `s3.proxy.putobject`, `s3.proxy.uploadpart`,
  `s3.proxy.completemultipart`, etc.
- Deletes: `s3.proxy.deleteobject`, `s3.proxy.deleteobjects`,
  `s3.proxy.abortmultipart`.
- Anything that returned a non-2xx/3xx (auth failure, scope
  violation, rate limit, quota exceeded, upstream error).
- Dashboard-side actions (auth, shares, bucket settings, quotas) —
  the sampling knob doesn't apply to them.

## When to flip it to 1.0

- **Compliance regime** that demands attribution of every read
  (regulated industries, government).
- **Investigations** in progress where you want a few hours of full
  fidelity.
- **Low-traffic deployments** where the audit cost is negligible.

When you flip it on, watch:

- `stowage_sqlite_db_bytes` for growth.
- `stowage_request_duration_seconds` p99 for the proxy — audit
  back-pressure shows up as elevated tail.
- The audit-recorder's queue depth.

You can roll it back to 0.0 at runtime; existing rows stay where
they are.

## Why not just default 1.0 with a "drop on overflow" recorder

The async recorder already drops on queue overflow. But:

- A "log most events" model lulls operators into thinking the audit
  is forensic-grade when it's actually best-effort.
- The compliance-vs-cost tradeoff varies by deployment. Forcing the
  decision into config makes it explicit.
- The events that matter for forensics — writes, deletes, auth
  failures — are always recorded. Successful reads are usually
  noise unless you have a regulatory reason to keep them.

## Why proxy reads specifically

Dashboard reads are recorded too — those handlers don't go through
the sampling rule. The default favours the proxy because the proxy's
RPS is much higher than the dashboard's, and the proxy's reads are
the loudest contributor to audit volume.

If you're seeing audit cost on the dashboard side too, the answer is
to tune `ratelimit.api_per_minute` lower or to identify which
handlers are noisy and ask the maintainer for a per-handler sampling
knob.

## Source

- Config: [`internal/config/config.go`](https://github.com/stowage-dev/stowage/blob/main/internal/config/config.go)
- Recorder: [`internal/audit/`](https://github.com/stowage-dev/stowage/tree/main/internal/audit)
- Proxy emission: [`internal/s3proxy/server.go`](https://github.com/stowage-dev/stowage/blob/main/internal/s3proxy/server.go)
