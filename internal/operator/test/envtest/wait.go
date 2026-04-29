// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build envtest

package envtest

import (
	"context"
	"testing"
	"time"
)

// Eventually polls fn at the given interval until it returns true or timeout
// fires. The last error reported by fn is included in the t.Fatalf message.
//
// It deliberately does not use stretchr/testify's Eventually because that
// helper logs goroutine dumps on failure that drown out the actual reason.
func Eventually(t *testing.T, timeout, interval time.Duration, msg string, fn func() (bool, error)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		ok, err := fn()
		if ok {
			return
		}
		lastErr = err
		if time.Now().After(deadline) {
			if lastErr != nil {
				t.Fatalf("%s: timed out after %s: last error: %v", msg, timeout, lastErr)
			}
			t.Fatalf("%s: timed out after %s", msg, timeout)
		}
		time.Sleep(interval)
	}
}

// WithTimeout returns a context that's cancelled when the test ends or when
// timeout elapses, whichever happens first. Saves writing the same boilerplate
// in every envtest case.
func WithTimeout(t *testing.T, timeout time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	return ctx
}
