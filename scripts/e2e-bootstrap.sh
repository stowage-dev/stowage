#!/usr/bin/env bash
# Copyright (C) 2026 Damian van der Merwe
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# e2e-bootstrap.sh — bring up a kind cluster suitable for running the
# Stowage e2e suite under test/e2e/. The cluster is named "stowage-e2e"
# unless overridden via KIND_CLUSTER_NAME. The script is idempotent: if
# the cluster already exists it just refreshes CRDs and prints the
# environment exports a developer needs.
#
# What this installs:
#   - a single-node kind cluster (image v1.32.0)
#   - the broker.stowage.io CRDs from deploy/chart/crds/
#   - host.docker.internal mapped to the kind network gateway, so the
#     in-process webhook server in the test binary is reachable from the
#     apiserver
#
# What it does NOT install: the Stowage chart itself. The e2e tests run
# the operator's reconcilers + webhook in-process; the chart-install lane
# is exercised by test/chart/.

set -euo pipefail

# Trace every command — the stderr trace is the easiest way to see which
# step blew up in CI (where attaching to the container isn't possible).
# Local devs running `make e2e` also get the trace, which is short
# enough not to be noisy.
set -x

CLUSTER="${KIND_CLUSTER_NAME:-stowage-e2e}"
KIND_IMAGE="${KIND_NODE_IMAGE:-kindest/node:v1.32.0}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CRD_DIR="${REPO_ROOT}/deploy/chart/crds"

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "e2e-bootstrap: required tool '$1' not on PATH" >&2
    exit 1
  }
}

need kind
need kubectl
need docker

# Dump the existing kind cluster list to stderr so a CI-side mismatch
# (kind-action naming the cluster differently than we asked for) is
# obvious from the trace. Critically this MUST NOT touch stdout: CI
# parses the script's stdout into $GITHUB_ENV, and a stray cluster name
# on stdout would land there as a malformed env line.
kind get clusters >&2 2>&1 || true

if kind get clusters 2>/dev/null | grep -qx "${CLUSTER}"; then
  echo "e2e-bootstrap: cluster '${CLUSTER}' already exists, reusing" >&2
else
  echo "e2e-bootstrap: creating kind cluster '${CLUSTER}'" >&2
  kind create cluster --name "${CLUSTER}" --image "${KIND_IMAGE}" --wait 120s
fi

# Write the cluster's kubeconfig to a repo-local file so the script
# never clobbers a developer's $HOME/.kube/config. STOWAGE_E2E_KUBECONFIG
# overrides the default; CI runners typically point it at RUNNER_TEMP so
# successive jobs don't share state.
#
# We deliberately do NOT honour an externally-set KUBECONFIG here: the
# whole point of the script is to produce a fresh, isolated kubeconfig
# for the e2e suite. The exports we emit at the end of the script point
# callers (Makefile / CI) at this path.
KUBECONFIG_DIR="${REPO_ROOT}/.e2e"
KUBECONFIG_PATH="${STOWAGE_E2E_KUBECONFIG:-${KUBECONFIG_DIR}/kubeconfig}"
mkdir -p "$(dirname "${KUBECONFIG_PATH}")"
kind get kubeconfig --name "${CLUSTER}" > "${KUBECONFIG_PATH}.tmp"
mv "${KUBECONFIG_PATH}.tmp" "${KUBECONFIG_PATH}"
export KUBECONFIG="${KUBECONFIG_PATH}"

# Discover the gateway IP the kind apiserver should dial to reach the
# host. The in-process webhook server binds on 0.0.0.0; we patch
# host.docker.internal -> $GATEWAY into each kind node so the apiserver's
# webhook config (using `url:` not `service:`) resolves there.
#
# We deliberately do NOT inspect the "kind" docker network directly —
# helm/kind-action and other wrappers occasionally rename it, and IPAM
# config layouts vary across docker engine versions. Reading the gateway
# off the control-plane container's own networking is the canonical
# pattern from the kind docs.
CONTROL_PLANE="${CLUSTER}-control-plane"
GATEWAY="$(docker inspect "${CONTROL_PLANE}" --format '{{range .NetworkSettings.Networks}}{{.Gateway}}{{end}}' 2>/dev/null || true)"
if [[ -z "${GATEWAY}" ]]; then
  echo "e2e-bootstrap: could not discover gateway from container ${CONTROL_PLANE}" >&2
  echo "e2e-bootstrap: docker inspect output follows for triage:" >&2
  docker inspect "${CONTROL_PLANE}" >&2 || true
  exit 1
fi
echo "e2e-bootstrap: kind gateway (from ${CONTROL_PLANE}) is ${GATEWAY}" >&2

for node in $(kind get nodes --name "${CLUSTER}"); do
  # Avoid duplicate /etc/hosts entries on re-runs.
  docker exec "${node}" sh -c "grep -q 'host.docker.internal' /etc/hosts || echo '${GATEWAY} host.docker.internal' >> /etc/hosts"
done

# Apply the chart's hand-curated CRDs so the operator's API types are
# registered before any test creates an S3Backend or BucketClaim.
echo "e2e-bootstrap: applying CRDs from ${CRD_DIR}" >&2
kubectl apply -f "${CRD_DIR}"
kubectl wait --for=condition=Established crd/s3backends.broker.stowage.io --timeout=60s
kubectl wait --for=condition=Established crd/bucketclaims.broker.stowage.io --timeout=60s

# Pre-create the operator namespace the in-process manager writes
# internal Secrets into. Tests will reuse it across the run.
OPS_NS="${STOWAGE_E2E_OPS_NS:-stowage-e2e-ops}"
kubectl get namespace "${OPS_NS}" >/dev/null 2>&1 || kubectl create namespace "${OPS_NS}"

cat >&2 <<EOF

e2e-bootstrap: ready. Export the following before \`go test -tags e2e\`:
  KUBECONFIG=${KUBECONFIG_PATH}
  STOWAGE_E2E_WEBHOOK_EXTERNAL_HOST=${GATEWAY}
  STOWAGE_E2E_OPS_NS=${OPS_NS}

The Makefile \`e2e\` target sets these automatically. Your default
~/.kube/config is not touched; teardown wipes ${KUBECONFIG_DIR}.
EOF

# Emit shell-eval'able exports for the Makefile to consume.
echo "export KUBECONFIG=${KUBECONFIG_PATH}"
echo "export STOWAGE_E2E_WEBHOOK_EXTERNAL_HOST=${GATEWAY}"
echo "export STOWAGE_E2E_OPS_NS=${OPS_NS}"
