#!/usr/bin/env bash
# Copyright (C) 2026 Damian van der Merwe
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# e2e-teardown.sh — delete the kind cluster brought up by
# e2e-bootstrap.sh and remove the repo-local kubeconfig the bootstrap
# wrote. Idempotent: a missing cluster is not an error.

set -euo pipefail

CLUSTER="${KIND_CLUSTER_NAME:-stowage-e2e}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KUBECONFIG_DIR="${REPO_ROOT}/.e2e"

if ! command -v kind >/dev/null 2>&1; then
  echo "e2e-teardown: kind not on PATH; nothing to do" >&2
else
  if kind get clusters 2>/dev/null | grep -qx "${CLUSTER}"; then
    kind delete cluster --name "${CLUSTER}"
  else
    echo "e2e-teardown: cluster '${CLUSTER}' not present" >&2
  fi
fi

# Clean up the per-cluster kubeconfig so a follow-up `make e2e` starts
# from a known state. The directory may already be missing.
rm -rf "${KUBECONFIG_DIR}"
