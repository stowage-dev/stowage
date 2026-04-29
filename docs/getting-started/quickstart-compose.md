---
type: tutorial
---

# Quickstart: Docker Compose

Five minutes to a Stowage + MinIO stack. Useful when you want a more
realistic environment than the bundled-MinIO quickstart — both
processes run in their own containers, and the compose file is a good
starting point for adapting to a real deployment.

## Prerequisites

- Docker 24+
- A clone of the Stowage repository (the compose file references the
  `Dockerfile` so it can build from source).

## Bring the stack up

From the repo root:

```sh
docker compose -f deploy/compose/docker-compose.yml up -d
```

This launches:

- `minio` — `quay.io/minio/minio:latest`, root user `minioadmin`,
  exposed on `:9000` (S3 API) and `:9001` (MinIO console).
- `stowage` — built from `deploy/docker/Dockerfile`, configured by
  `config.example.yaml` mounted into the container, exposed on `:8080`.

The Stowage container waits on MinIO's health check before it starts
the dashboard.

## Create the first admin user

```sh
docker compose -f deploy/compose/docker-compose.yml exec stowage \
  stowage create-admin \
    --username admin \
    --password 'S3cur3-P@ssw0rd!'
```

The password rule is at least 12 characters; the demo config relaxes
the zxcvbn strength check, but production deployments should not.

## Log in

Open [http://localhost:8080](http://localhost:8080), log in as `admin`,
and you should land in the dashboard with `local-minio` listed as a
backend.

## Inspect what you have

```sh
docker compose -f deploy/compose/docker-compose.yml ps
docker compose -f deploy/compose/docker-compose.yml logs stowage --tail=20
```

The MinIO console is at [http://localhost:9001](http://localhost:9001)
if you want to look at the underlying bucket directly. Tenants don't
need it — they go through Stowage.

## Tear it down

```sh
docker compose -f deploy/compose/docker-compose.yml down -v
```

The `-v` flag removes the named volume `minio-data`. Drop it to keep
the bucket contents around between runs.

## What's in the compose file

[`deploy/compose/docker-compose.yml`](https://github.com/stowage-dev/stowage/blob/main/deploy/compose/docker-compose.yml)
is intentionally minimal — two services, no reverse proxy, no TLS, no
volume for Stowage's own state. For anything beyond local kicking-of-
tyres, add:

- A real reverse proxy in front of `:8080` (nginx, Caddy, Traefik) —
  see [Self-host → Reverse proxy](../self-host/reverse-proxy/overview.md).
- A persistent volume for Stowage's `stowage.db` and the secret-key
  file — see [Self-host → SQLite](../self-host/configure/sqlite.md).
- Real OIDC, not local username + password — see
  [Self-host → OIDC](../self-host/auth/oidc.md).

## Next step

- [Your first share link](./first-share.md).
- [Self-host →](../self-host/) for everything else.
