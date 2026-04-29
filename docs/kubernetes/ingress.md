---
type: how-to
---

# Ingress

The chart can render an `Ingress` for the dashboard. The S3 proxy is
exposed on the same Service but typically wants its own Ingress
(distinct host and timeouts) — render that separately.

## Built-in dashboard Ingress

```yaml
ingress:
  enabled: true
  className: nginx
  host: stowage.example.com
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
    nginx.ingress.kubernetes.io/proxy-body-size: 5g
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
  tls: true
```

When `tls: true`, the chart emits a `tls:` block referencing a
Secret named `<release>-tls` — your Ingress controller (or
cert-manager) is expected to populate it.

For a more complete view of what the reverse proxy must forward,
see [Self-host → Reverse proxy → overview](../self-host/reverse-proxy/overview.md).
The same headers apply on Kubernetes; the Ingress controller takes
the role of the reverse proxy.

## S3 proxy Ingress

Render a second Ingress for the SDK path. Example:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: stowage-s3
  namespace: stowage-system
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
    nginx.ingress.kubernetes.io/proxy-body-size: 5g
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
spec:
  ingressClassName: nginx
  tls:
    - hosts: [s3.stowage.example.com]
      secretName: stowage-s3-tls
  rules:
    - host: s3.stowage.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: stowage
                port:
                  number: 8090
```

For virtual-hosted-style (`<bucket>.s3.stowage.example.com`), add a
wildcard host:

```yaml
  rules:
    - host: "*.s3.stowage.example.com"
      ...
```

And mirror the suffix in the Stowage config:

```yaml
config:
  s3_proxy:
    host_suffixes:
      - s3.stowage.example.com
```

## What gets forwarded

The chart's annotations cover the typical nginx-ingress configuration
for streamed uploads / downloads. If you use a different controller
(Traefik, HAProxy, contour), translate them — see
[Self-host → Reverse proxy](../self-host/reverse-proxy/overview.md).

## TLS termination

The Ingress controller terminates TLS. Stowage itself sees plaintext
HTTP, with `X-Forwarded-Proto: https` set by the controller. Set
`server.trusted_proxies` in the Stowage config to the Pod CIDR of
your Ingress controller.

## Restricting `/metrics`

The Prometheus scrape endpoint is unauthenticated. Restrict it at
the Ingress layer or, better, scrape it only from inside the cluster
(no Ingress route exposing it externally):

```yaml
config:
  # /metrics is on the dashboard listener; the Service exposes both ports.
  # Restrict to in-cluster scrape only by NOT routing /metrics externally.
```

The dashboard Ingress above doesn't include a path-specific exclusion;
add one if you need it.
