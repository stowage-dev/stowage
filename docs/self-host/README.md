---
type: how-to
---

# Self-host

Task-oriented recipes for running Stowage outside Kubernetes — on a
VM, a bare-metal box, or a single container. Each page assumes you've
done a [quickstart](../getting-started/) and now want to do something
specific.

## Install

- [Using the install script](./install/script.md)
- [From a release binary](./install/release-binary.md)
- [From source](./install/from-source.md)
- [As a Docker container](./install/docker.md)
- [As a systemd unit](./install/systemd.md)

## Configure

- [The config file and env-var overrides](./configure/config-file.md)
- [Logging](./configure/logging.md)
- [SQLite path and lifecycle](./configure/sqlite.md)

## Authentication

- [Local username + password](./auth/local.md)
- [Static config-defined account](./auth/static.md)
- [OIDC](./auth/oidc.md)
- [Sessions, idle timeout, lockout](./auth/sessions.md)

## Reverse proxy and TLS

- [Why Stowage runs HTTP-only behind a proxy](./reverse-proxy/overview.md)
- [nginx](./reverse-proxy/nginx.md)
- [Caddy](./reverse-proxy/caddy.md)
- [Traefik](./reverse-proxy/traefik.md)

## Backends

- [Connecting an S3-compatible backend in YAML](./backends/yaml.md)
- [Managing endpoints from the dashboard](./backends/ui-managed.md)
- Per-vendor recipes:
  - [MinIO](./backends/minio.md)
  - [Garage](./backends/garage.md)
  - [SeaweedFS](./backends/seaweedfs.md)
  - [AWS S3](./backends/aws-s3.md)
  - [Backblaze B2](./backends/b2.md)
  - [Cloudflare R2](./backends/r2.md)
  - [Wasabi](./backends/wasabi.md)

## Bucket administration

- [Versioning](./buckets/versioning.md)
- [Bucket policy](./buckets/policy.md)
- [CORS](./buckets/cors.md)
- [Lifecycle rules](./buckets/lifecycle.md)
- [Quotas](./buckets/quotas.md)

## Sharing

- [Creating and revoking shares](./sharing/create-and-revoke.md)
- [Share semantics: passwords, expiry, download caps](./sharing/semantics.md)
- [The recipient page (`/s/<code>`)](./sharing/recipient.md)

## Cross-backend transfers

- [Copying between backends](./transfers.md)

## Audit

- [Querying and exporting the audit log](./audit.md)

## Operations

- [Health probes](./operations/health.md)
- [Backup and restore](./operations/backup.md)
- [Rotating `STOWAGE_SECRET_KEY`](./operations/key-rotation.md)
- [Upgrading between releases](./operations/upgrade.md)

## Migrating to Stowage

- [From the MinIO Console](./migrations/from-minio-console.md)
