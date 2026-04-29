---
type: how-to
---

# Health probes

Stowage exposes two unauthenticated probe endpoints on the dashboard
listener.

## Endpoints

| Path | Returns when healthy |
|---|---|
| `GET /healthz` | `200` `{"status":"ok"}` — the process is up. |
| `GET /readyz` | `200` `{"status":"ready"}` — the process is up and the HTTP server is accepting requests. |

Both are emitted unconditionally — they do not check backend health
or database connectivity. That's deliberate: probe endpoints should
fail only when the process itself is broken, not when an upstream
hiccups, otherwise Kubernetes restarts Stowage every time MinIO
flickers.

For backend-level health, use `/admin/backends/health` (admin-only,
shows the per-backend probe history).

## Kubernetes liveness / readiness

The Helm chart wires both:

```yaml
livenessProbe:
  httpGet: { path: /healthz, port: http }
  initialDelaySeconds: 5
  periodSeconds: 10
readinessProbe:
  httpGet: { path: /readyz, port: http }
  initialDelaySeconds: 1
  periodSeconds: 5
```

For non-Kubernetes deployments, point your load balancer or
monitoring at `/healthz`.

## Backend probes

`/admin/backends/health` shows a 20-tile rolling history strip per
backend, plus the last error and last latency.

The probe is `ListBuckets` against the upstream with a short timeout.
A red tile means that probe failed; the strip lets you visually
distinguish flapping (alternating tiles) from sustained outage (all
red).

The probe runs on a schedule from `internal/backend/registry.go`'s
`ProbeAll` and updates the in-memory status and history ring.
