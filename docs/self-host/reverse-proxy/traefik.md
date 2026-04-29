---
type: how-to
---

# Traefik

Worked Traefik v2 / v3 example using the file provider. Adapt to the
docker-label or Kubernetes-CRD provider as needed.

```yaml
# /etc/traefik/dynamic/stowage.yml
http:
  routers:
    stowage-dashboard:
      rule: "Host(`stowage.example.com`)"
      entryPoints: [websecure]
      tls:
        certResolver: letsencrypt
      service: stowage-dashboard
      middlewares: [stowage-headers]

    stowage-s3:
      rule: "HostRegexp(`{sub:[a-z0-9-]+}.s3.stowage.example.com`) || Host(`s3.stowage.example.com`)"
      entryPoints: [websecure]
      tls:
        certResolver: letsencrypt
      service: stowage-s3
      middlewares: [stowage-headers]

  services:
    stowage-dashboard:
      loadBalancer:
        servers:
          - url: "http://127.0.0.1:8080"
        passHostHeader: true

    stowage-s3:
      loadBalancer:
        servers:
          - url: "http://127.0.0.1:8090"
        passHostHeader: true

  middlewares:
    stowage-headers:
      headers:
        customRequestHeaders:
          # Belt-and-braces; Traefik already sets these.
          X-Forwarded-Proto: "https"
```

## Stowage config to match

```yaml
server:
  listen: "127.0.0.1:8080"
  trusted_proxies:
    - 127.0.0.1/32

s3_proxy:
  enabled: true
  listen: "127.0.0.1:8090"
  host_suffixes:
    - s3.stowage.example.com
```

## Notes

- Traefik sets `X-Forwarded-For`, `X-Forwarded-Proto`,
  `X-Forwarded-Host`, and `X-Real-Ip` by default. Don't double up.
- For long-running uploads / downloads via the SDK path, increase the
  `entryPoints.websecure.transport.respondingTimeouts.idleTimeout` /
  `readTimeout` / `writeTimeout` on the entrypoint, not just the
  service.
- Use a Traefik IP-allowlist middleware on `/metrics` if you don't
  want it world-readable.
