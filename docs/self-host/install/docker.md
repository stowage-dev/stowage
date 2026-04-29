---
type: how-to
---

# Install as a Docker container

The Stowage image is built from
[`deploy/docker/Dockerfile`](https://github.com/stowage-dev/stowage/blob/main/deploy/docker/Dockerfile)
— a multi-stage distroless image containing only the binary, no shell,
no package manager.

## Pull or build

```sh
# Build locally:
make docker
# → stowage:dev

# Or pull a release image (when published):
docker pull ghcr.io/stowage-dev/stowage:v1.0.0
```

## Default command

The image's entrypoint is the `stowage` binary; the default `CMD` is
`quickstart`. Running with no args downloads MinIO into the data
volume and runs everything in-process — useful for kicking the tyres,
not for production.

## Production-shaped invocation

```sh
docker run -d \
  --name stowage \
  -p 8080:8080 \
  -p 8090:8090 \
  -v stowage-data:/var/lib/stowage \
  -v $(pwd)/config.yaml:/etc/stowage/config.yaml:ro \
  -e STOWAGE_SECRET_KEY=$(openssl rand -hex 32) \
  ghcr.io/stowage-dev/stowage:v1.0.0 \
    serve --config /etc/stowage/config.yaml
```

Notes:

- `-p 8090:8090` is only needed if you've enabled the embedded SigV4
  proxy.
- The named volume `stowage-data` should be where your config points
  `db.sqlite.path`.
- `STOWAGE_SECRET_KEY` is a 32-byte key in 64 hex chars or 44 base64
  chars. Lose it and you lose access to sealed endpoint secrets and
  virtual credentials. See
  [Operations → Rotating the key](../operations/key-rotation.md).

## Bootstrapping the first admin

Open a one-off process against the same volume:

```sh
docker run --rm \
  -v stowage-data:/var/lib/stowage \
  -v $(pwd)/config.yaml:/etc/stowage/config.yaml:ro \
  ghcr.io/stowage-dev/stowage:v1.0.0 \
    create-admin --config /etc/stowage/config.yaml \
    --username admin --password 'S3cur3-P@ssw0rd!'
```

## docker-compose

For a stack that includes a backend, see
[Quickstart: Docker Compose](../../getting-started/quickstart-compose.md)
and
[`deploy/compose/docker-compose.yml`](https://github.com/stowage-dev/stowage/blob/main/deploy/compose/docker-compose.yml).

## What the image does NOT include

- No reverse proxy. Run nginx / Caddy / Traefik in front and terminate
  TLS there. See [Reverse proxy → overview](../reverse-proxy/overview.md).
- No log rotation. Stowage logs to stdout in JSON or text; let your
  Docker logging driver or sidecar handle rotation.
- No metrics scraper. `/metrics` is exposed on the dashboard listener;
  point Prometheus at it.
