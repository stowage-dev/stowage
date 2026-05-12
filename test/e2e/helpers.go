// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build e2e

package e2e

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
// timeout elapses, whichever happens first.
func WithTimeout(t *testing.T, timeout time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	return ctx
}

// UniqueName returns a short random-suffixed name prefixed by p. It uses 4
// hex bytes — enough entropy for per-test isolation within a single run and
// short enough to keep namespace/secret names under 63 chars.
func UniqueName(p string) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return strings.ToLower(p) + "-" + hex.EncodeToString(b[:])
}

// CreateNamespace creates ns and registers a t.Cleanup that deletes it. The
// cleanup runs with a fresh background context so a failed test doesn't
// leak resources.
func CreateNamespace(t *testing.T, ctx context.Context, c client.Client, name string) {
	t.Helper()
	err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create namespace %s: %v", name, err)
	}
	t.Cleanup(func() {
		bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = c.Delete(bg, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})
	})
}

// NewNamespace generates a unique namespace name, creates it, and returns
// the name. Most tests should use this directly instead of CreateNamespace.
func NewNamespace(t *testing.T, ctx context.Context, c client.Client, prefix string) string {
	t.Helper()
	name := UniqueName(prefix)
	CreateNamespace(t, ctx, c, name)
	return name
}

// EnsureOpsNamespace returns the operator namespace for the current process.
// We use one shared namespace across the test run so the in-process operator
// can find its admin Secrets without colliding with the chart-installed
// system namespace. The default ("stowage-e2e-ops") is overridable via env
// for cluster-sharing scenarios.
//
// The namespace is created lazily; failure to create is fatal because every
// operator test depends on it.
func EnsureOpsNamespace(t *testing.T, ctx context.Context, c client.Client) string {
	t.Helper()
	ns := opsNamespaceName()
	if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create ops namespace %s: %v", ns, err)
	}
	return ns
}

// CleanupSecretsByLabels deletes every Secret in ns matching labels. Used by
// tests that share an ops namespace so leftover virtual-credential Secrets
// from one test don't leak into another.
func CleanupSecretsByLabels(t *testing.T, c client.Client, ns string, labels map[string]string) {
	t.Helper()
	t.Cleanup(func() {
		bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var list corev1.SecretList
		if err := c.List(bg, &list, client.InNamespace(ns), client.MatchingLabels(labels)); err != nil {
			return
		}
		for i := range list.Items {
			_ = c.Delete(bg, &list.Items[i])
		}
	})
}

// GetSecret is a small Get wrapper for tests; returns (nil, false) on
// NotFound so callers can write `if s, ok := GetSecret(...); ok`.
func GetSecret(ctx context.Context, c client.Client, ns, name string) (*corev1.Secret, bool) {
	var s corev1.Secret
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &s); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false
		}
		return nil, false
	}
	return &s, true
}

