// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestMain wires the shared cluster handle and aborts the run with an
// actionable message if the cluster isn't reachable or the broker CRDs
// aren't applied yet. The rest of the suite assumes both pre-conditions
// hold; failing fast here keeps individual tests readable.
func TestMain(m *testing.M) {
	c, err := Connect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: cluster unreachable: %v\n", err)
		fmt.Fprintln(os.Stderr, "e2e: run `make e2e-bootstrap` (or set KUBECONFIG to a ready cluster) before `go test -tags e2e`.")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := c.EnsureCRDs(ctx); err != nil {
		cancel()
		fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
		os.Exit(2)
	}
	cancel()

	os.Exit(m.Run())
}
