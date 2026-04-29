---
type: tutorial
---

# Get started

Hand-held walkthroughs that end with you having something working. Each
one is verified end-to-end on a clean machine.

## Pick a path

- **[What is Stowage](./what-is-stowage.md)** — read this first if
  you're evaluating. One page, no commands.
- **[Quickstart: one-liner](./quickstart-oneliner.md)** — fastest path
  to a running dashboard. ~2 minutes. Linux, macOS, Windows.
- **[Quickstart: Docker Compose](./quickstart-compose.md)** — Stowage +
  a MinIO backend in one stack. ~5 minutes.
- **[Quickstart: Kubernetes](./quickstart-kubernetes.md)** — Helm
  chart + operator + a `BucketClaim`. ~15 minutes.
- **[Your first share link](./first-share.md)** — five-minute walkthrough
  of the share UI: upload a file, set expiry / password / download cap,
  open the link in another browser.
- **[Your first virtual S3 credential](./first-credential.md)** —
  five-minute walkthrough of the embedded SigV4 proxy: mint a
  credential, point `aws-cli` at it.

After you've finished a quickstart, jump to:

- **[Self-host →](../self-host/)** if you'll run Stowage outside
  Kubernetes.
- **[Run on Kubernetes →](../kubernetes/)** if you need the operator,
  multi-tenant credentials, or `BucketClaim` CRDs.
- **[Use as an S3 endpoint →](../s3-endpoint/)** if you're a tenant
  developer who just got handed an access key.
