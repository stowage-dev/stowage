---
type: how-to
---

# Why Stowage runs HTTP-only behind a proxy

Stowage's listener speaks plaintext HTTP. TLS termination is the job
of a reverse proxy you already operate. This page explains the
contract; the per-proxy pages
([nginx](./nginx.md), [Caddy](./caddy.md), [Traefik](./traefik.md))
have the worked configs.

## What Stowage expects from the proxy

1. **Terminate TLS.** Don't pass HTTPS-shaped traffic through the
   reverse proxy untouched.
2. **Set `X-Forwarded-Proto`** to `https` when serving over TLS.
   Stowage uses this when deciding to mark cookies `Secure` and when
   reconstructing absolute URLs.
3. **Set `X-Forwarded-For`** so Stowage knows the real client IP for
   audit, rate limit, and proxy-trust gating.
4. **Forward the original `Host`** so OIDC redirects and share URLs
   come out correct.
5. **Don't strip cookies, don't add caching.** The dashboard's
   responses are dynamic; aggressive proxy caches break things in
   surprising ways.
6. **Do strip `X-Forwarded-*` headers from the request before
   forwarding** — i.e. the proxy should set them, not pass through
   whatever the client sent. Otherwise an attacker can set
   `X-Forwarded-For: trusted` and lie about their IP.

## What Stowage exposes

| Path | Auth | Notes |
|---|---|---|
| `/` and SPA assets | none | The SvelteKit frontend. |
| `/api/*` | session cookie | Mutations require `X-CSRF-Token`. |
| `/auth/*` | none | Login + callback + logout. |
| `/s/*` | none (per-share) | Public share recipient pages and JSON+bytes. |
| `/healthz`, `/readyz` | none | Probes. Safe to expose to your load balancer. |
| `/metrics` | none | Prometheus. Restrict at the proxy / ACL layer. |

If `s3_proxy.enabled: true`, the embedded SigV4 proxy is on a
**separate listener** (default `:8090`). Run that behind a separate
proxy entry — the SDK clients pointing at it expect AWS-style virtual
host or path style routing, not the dashboard's HTTP semantics.

## `server.trusted_proxies`

Stowage's
[`internal/auth/proxy.go`](https://github.com/stowage-dev/stowage/blob/main/internal/auth/proxy.go)
implements a CIDR-based trust gate that decides whether to honour
inbound `X-Forwarded-*` headers.

```yaml
server:
  trusted_proxies:
    - 10.0.0.0/8
    - 192.168.0.0/16
    - 172.16.0.0/12
    - 127.0.0.1/32
```

| Value | Behaviour |
|---|---|
| Empty (default) | Honour `X-Forwarded-*` from every immediate peer. Right for the typical "Stowage on localhost behind nginx" topology. Wrong if Stowage's listener is reachable from anywhere except the proxy. |
| Non-empty | Honour the headers only when the immediate peer's IP falls within one of the listed CIDRs. |

The gate replaces chi's default `RealIP` middleware so the trust list
is enforced uniformly across every request.

## What "behind a proxy" doesn't mean

- It doesn't mean Stowage trusts the proxy to authenticate users for
  it. The session is still the authoritative auth artefact.
- It doesn't mean Stowage emits HSTS unconditionally. HSTS is only
  set when the request rode over TLS — emitting it on plaintext HTTP
  is at best ignored, and at worst a foot-gun for developers hitting
  localhost.
- It doesn't mean Stowage can be put behind multiple proxy hops
  without configuration. If you have two hops, both must be in
  `trusted_proxies`, and both must rewrite `X-Forwarded-For`
  correctly.

## Worked configs

- [nginx](./nginx.md)
- [Caddy](./caddy.md)
- [Traefik](./traefik.md)
