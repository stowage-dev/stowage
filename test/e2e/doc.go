// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build e2e

// Package e2e holds Stowage's Kubernetes integration tests. Suites run
// against a real cluster — typically a kind cluster brought up by
// scripts/e2e-bootstrap.sh — and drive the operator's reconcilers,
// admission webhooks, vcstore Reader/Writer, and the s3proxy
// KubernetesSource informer in-process against that apiserver.
//
// This package replaces the previous envtest harness under
// internal/operator/test/envtest. The shift away from envtest's local
// kube-apiserver+etcd binaries to a real cluster gives us:
//
//   - parity with how the operator and chart actually run in production
//     (real RBAC, real cert-manager when used, real DNS),
//   - first-class coverage of the Helm chart's CRDs, since those are the
//     ones we install at bootstrap,
//   - a single set of integration tests instead of two parallel matrices.
//
// The "e2e" build tag keeps tests out of `go test ./...` so a developer
// without a cluster handy can still run the unit-test matrix.
//
// Tests assume the KUBECONFIG environment variable (or the default
// kubeconfig path) points at a cluster that already has the broker
// CRDs from deploy/chart/crds applied. The Makefile `e2e` target
// handles the bootstrap; CI does the same via helm/kind-action.
package e2e
