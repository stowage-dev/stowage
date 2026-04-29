#!/bin/sh
# Idempotently seed the bench admin then exec stowage serve.
set -eu

CFG="${STOWAGE_CONFIG:-/etc/stowage/config.yaml}"
ADMIN_USER="${BENCH_ADMIN_USER:-admin}"
ADMIN_PASS="${BENCH_ADMIN_PASS:-B3nchm@rk-Pa55w0rd}"

# create-admin is idempotent-ish: on a re-run it errors with "username
# already exists" which we ignore. It also runs all migrations as a
# side-effect, so on a fresh volume this is what initialises the schema.
echo "bench-entry: ensuring admin user '$ADMIN_USER' exists" >&2
/usr/local/bin/stowage create-admin \
    --config "$CFG" \
    --username "$ADMIN_USER" \
    --password "$ADMIN_PASS" 2>&1 | grep -v 'already exists' >&2 || true

echo "bench-entry: starting stowage serve" >&2
exec /usr/local/bin/stowage serve --config "$CFG"
