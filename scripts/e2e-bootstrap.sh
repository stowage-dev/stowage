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

if kind get clusters 2>/dev/null | grep -qx "${CLUSTER}"; then
  echo "e2e-bootstrap: cluster '${CLUSTER}' already exists, reusing" >&2
else
  echo "e2e-bootstrap: creating kind cluster '${CLUSTER}'" >&2
  kind create cluster --name "${CLUSTER}" --image "${KIND_IMAGE}" --wait 120s
fi

# Point kubectl at the cluster for the remainder of this script and emit
# the same kubeconfig path for callers (Makefile sets KUBECONFIG before
# running `go test`).
KUBECONFIG_PATH="${KUBECONFIG:-${HOME}/.kube/config}"
kind get kubeconfig --name "${CLUSTER}" > "${KUBECONFIG_PATH}.tmp"
mv "${KUBECONFIG_PATH}.tmp" "${KUBECONFIG_PATH}"

# Discover the kind network gateway IP. The in-process webhook server
# binds on 0.0.0.0; the apiserver inside the kind container dials the
# gateway to reach the host. Linux docker doesn't ship a working
# host.docker.internal, so we patch /etc/hosts inside each node to point
# host.docker.internal at the gateway IP.
GATEWAY="$(docker network inspect kind --format '{{ (index .IPAM.Config 0).Gateway }}')"
if [[ -z "${GATEWAY}" ]]; then
  echo "e2e-bootstrap: could not discover kind network gateway" >&2
  exit 1
fi
echo "e2e-bootstrap: kind network gateway is ${GATEWAY}" >&2

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

The Makefile \`e2e\` target sets these automatically.
EOF

# Emit shell-eval'able exports for the Makefile to consume.
echo "export KUBECONFIG=${KUBECONFIG_PATH}"
echo "export STOWAGE_E2E_WEBHOOK_EXTERNAL_HOST=${GATEWAY}"
echo "export STOWAGE_E2E_OPS_NS=${OPS_NS}"
