#!/usr/bin/env bash
# End-to-end benchmark driver: builds + starts the constrained stowage
# stack, runs the Go benchmark client against it, and writes results.md.
set -euo pipefail

cd "$(dirname "$0")/.."

COMPOSE="docker compose -f benchmarks/docker-compose.bench.yml"
PASS="${BENCH_ADMIN_PASS:-B3nchm@rk-Pa55w0rd}"
DURATION="${BENCH_DURATION:-15s}"
CONCURRENCY="${BENCH_CONCURRENCY:-16}"
LOGIN_DURATION="${BENCH_LOGIN_DURATION:-10s}"
LOGIN_CONCURRENCY="${BENCH_LOGIN_CONCURRENCY:-1}"

echo "==> building stack (this can take a few minutes on first run)"
$COMPOSE build

echo "==> starting stack"
$COMPOSE up -d

cleanup() {
  echo "==> tearing down stack"
  $COMPOSE down -v >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "==> waiting for stowage /readyz"
for i in $(seq 1 60); do
  if curl -fsS http://localhost:18080/readyz >/dev/null 2>&1; then
    echo "  stowage is ready"
    break
  fi
  sleep 1
done

echo "==> running dashboard bench"
go run ./benchmarks \
  -base http://localhost:18080 \
  -username admin \
  -password "$PASS" \
  -duration "$DURATION" \
  -concurrency "$CONCURRENCY" \
  -login-duration "$LOGIN_DURATION" \
  -login-concurrency "$LOGIN_CONCURRENCY" \
  -output benchmarks/results.md

echo "==> running S3 proxy bench"
go run ./benchmarks/s3proxybench \
  -dashboard http://localhost:18080 \
  -proxy http://localhost:18090 \
  -username admin \
  -password "$PASS" \
  -duration "$DURATION" \
  -concurrency "$CONCURRENCY" \
  -output benchmarks/results-s3proxy.md \
  -json benchmarks/results-s3proxy.json

echo "==> done; results in benchmarks/results.md and benchmarks/results-s3proxy.md"
