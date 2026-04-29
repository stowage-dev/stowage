# syntax=docker/dockerfile:1.7

# FRONTEND selects how web/dist gets populated:
#   - source    (default) build the SvelteKit bundle in-image with bun
#   - prebuilt  copy an already-built web/dist/ from the build context
# CI passes FRONTEND=prebuilt so the bun build runs once and is shared
# across the per-architecture image builds. Local `make docker` uses the
# default and builds the frontend itself.
ARG FRONTEND=source

# --- frontend stage (source) ---------------------------------------------
# Builds the SvelteKit static bundle into /web/dist using bun
# (the project ships a bun.lock).
FROM oven/bun:1-alpine AS frontend-source
WORKDIR /web
COPY web/package.json web/bun.lock* ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun run build

# --- frontend stage (prebuilt) -------------------------------------------
# Pulls a pre-built web/dist/ straight from the build context. Only
# referenced when FRONTEND=prebuilt; the COPY is skipped otherwise so a
# missing local web/dist/ doesn't break source-mode builds.
FROM scratch AS frontend-prebuilt
COPY web/dist /web/dist

# Alias the chosen stage so the backend stage doesn't care which path ran.
FROM frontend-${FRONTEND} AS frontend

# --- backend build stage --------------------------------------------------
FROM golang:1.26-alpine AS backend
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
COPY --from=frontend /web/dist ./web/dist
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/stowage \
    ./cmd/stowage

# --- runtime stage --------------------------------------------------------
# Alpine gives us tini (for clean signal handling) and ca-certificates
# (so the in-binary quickstart can fetch MinIO over TLS). Everything else
# the quickstart needs — download, sha256 verify, subprocess management —
# is handled inside the stowage binary.
FROM alpine:3.20
# Explicit numeric UID/GID so Kubernetes' runAsNonRoot admission can
# verify the image runs as non-root without resolving /etc/passwd.
RUN apk add --no-cache ca-certificates tini \
 && addgroup -S -g 65532 stowage \
 && adduser -S -u 65532 -G stowage -h /home/stowage stowage \
 && mkdir -p /data \
 && chown -R 65532:65532 /data /home/stowage

COPY --from=backend /out/stowage /usr/local/bin/stowage

USER 65532:65532
WORKDIR /data
VOLUME ["/data"]

# 8080 = stowage dashboard, 9000 = MinIO S3, 9001 = MinIO console
EXPOSE 8080 9000 9001

ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/stowage"]
CMD ["quickstart", "--data-dir=/data"]
