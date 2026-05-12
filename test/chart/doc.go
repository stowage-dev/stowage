// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package chart holds offline tests for the deploy/chart/ Helm chart.
//
// These tests don't need a cluster — they shell out to the `helm` binary
// and assert that:
//
//   - The chart lints clean.
//   - Default rendering produces the expected set of manifests and that
//     each one is valid YAML.
//   - Common value combinations (operator disabled, webhook disabled,
//     cert-manager enabled) flip the right knobs in the rendered output.
//
// The cluster-side counterpart (install the chart, watch Pods become
// Ready) lives under test/e2e/.
package chart
