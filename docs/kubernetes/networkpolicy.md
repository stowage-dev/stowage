---
type: how-to
---

# NetworkPolicy

The chart can render a baseline `NetworkPolicy` that locks Stowage's
ingress and egress to what the workloads need.

## Enable

```yaml
networkPolicy:
  enabled: true
```

When enabled, the chart renders policies that:

- **Allow ingress** to Stowage's dashboard port (8080) from the
  Ingress controller namespace.
- **Allow ingress** to Stowage's S3 proxy port (8090) from the
  Ingress controller namespace and (optionally) any Pod with a
  configured selector.
- **Allow egress** from the operator and Stowage to:
  - The Kubernetes API server (for the operator's controllers and
    the proxy's informer).
  - DNS (kube-dns / CoreDNS).
  - The configured upstream S3 endpoints.

The exact selectors live in
[`deploy/chart/templates/networkpolicy.yaml`](https://github.com/stowage-dev/stowage/blob/main/deploy/chart/templates/networkpolicy.yaml).
Inspect the rendered output with `helm template` to see the precise
shape on your release.

## Disable

```yaml
networkPolicy:
  enabled: false
```

## Things to verify on a custom cluster

- The Ingress controller's namespace selector matches the policy's
  ingress allow rule. Many controllers run in `ingress-nginx` or
  `traefik`; adjust the selector or namespace label as needed.
- The egress allow list reaches the upstream S3 endpoints. If the
  upstream is on a private network, make sure the policy permits
  the right CIDR.
- DNS (port 53 UDP/TCP) is allowed; otherwise discovery fails.

## Combining with a service mesh

A service mesh (Istio, Linkerd) usually overrides Pod-level network
behaviour. NetworkPolicy still applies; the mesh adds its own
authorization on top. The chart's policies don't conflict with mesh
configuration but you may find it simpler to disable
`networkPolicy.enabled` and let the mesh handle isolation.
