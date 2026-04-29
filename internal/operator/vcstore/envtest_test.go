// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build envtest

package vcstore_test

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	envtestharness "github.com/stowage-dev/stowage/internal/operator/test/envtest"
	"github.com/stowage-dev/stowage/internal/operator/vcstore"
)

// startVCManager runs a controller-runtime manager so the vcstore Reader and
// AnonReader can attach to a real cache backed by the apiserver.
func startVCManager(t *testing.T, suite *envtestharness.Suite, opsNS string) (ctrl.Manager, *vcstore.Reader, *vcstore.AnonReader) {
	t.Helper()

	mgr, err := ctrl.NewManager(suite.Cfg, ctrl.Options{
		Scheme:     suite.Scheme,
		Metrics:    metricsserver.Options{BindAddress: "0"},
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	reader, err := vcstore.NewReader(mgr, opsNS)
	if err != nil {
		t.Fatalf("new reader: %v", err)
	}
	anon, err := vcstore.NewAnonReader(mgr, opsNS)
	if err != nil {
		t.Fatalf("new anon reader: %v", err)
	}
	if err := mgr.Add(runnable(reader.Start)); err != nil {
		t.Fatalf("add reader runnable: %v", err)
	}
	if err := mgr.Add(runnable(anon.Start)); err != nil {
		t.Fatalf("add anon runnable: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	mgrErr := make(chan error, 1)
	go func() { mgrErr <- mgr.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-mgrErr:
			if err != nil && !strings.Contains(err.Error(), "context canceled") {
				t.Logf("manager exited: %v", err)
			}
		case <-time.After(15 * time.Second):
			t.Logf("manager did not exit within 15s")
		}
	})

	if !mgr.GetCache().WaitForCacheSync(ctx) {
		t.Fatalf("cache failed to sync")
	}

	return mgr, reader, anon
}

// runnable adapts a Start(ctx) function into a manager.Runnable.
type runnable func(ctx context.Context) error

func (r runnable) Start(ctx context.Context) error { return r(ctx) }

// TestVCStore_WriterReader_Roundtrip writes a virtual credential through the
// real Writer and verifies the Reader picks it up via the manager's cache.
func TestVCStore_WriterReader_Roundtrip(t *testing.T) {
	suite := envtestharness.Start(t)
	ctx := envtestharness.WithTimeout(t, 60*time.Second)

	const opsNS = "stowage-system"
	if err := suite.Client.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: opsNS}}); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create ns: %v", err)
	}

	mgr, reader, _ := startVCManager(t, suite, opsNS)
	_ = mgr

	w := &vcstore.Writer{Client: suite.Client, Namespace: opsNS}
	vc := vcstore.VirtualCredential{
		AccessKeyID:     "AKIATEST1234567",
		SecretAccessKey: "secrettest",
		BucketName:      "tenant-a-uploads",
		BackendName:     "primary",
		ClaimNamespace:  "tenant-a",
		ClaimName:       "uploads",
		ClaimUID:        "uid-1",
		QuotaSoftBytes:  1 << 20,
		QuotaHardBytes:  10 << 20,
	}
	if err := w.WriteInternal(ctx, vc); err != nil {
		t.Fatalf("write internal: %v", err)
	}

	envtestharness.Eventually(t, 10*time.Second, 100*time.Millisecond, "reader observes credential",
		func() (bool, error) {
			got, ok := reader.Lookup(vc.AccessKeyID)
			if !ok {
				return false, nil
			}
			if got.BucketName != vc.BucketName {
				return false, nil
			}
			if len(got.BucketScopes) != 1 || got.BucketScopes[0].BucketName != vc.BucketName {
				return false, nil
			}
			return true, nil
		})

	// Update the same credential — Reader should reflect the new bucket name.
	vc.BucketName = "tenant-a-uploads-v2"
	if err := w.WriteInternal(ctx, vc); err != nil {
		t.Fatalf("update internal: %v", err)
	}
	envtestharness.Eventually(t, 10*time.Second, 100*time.Millisecond, "reader observes update",
		func() (bool, error) {
			got, ok := reader.Lookup(vc.AccessKeyID)
			if !ok {
				return false, nil
			}
			return got.BucketName == "tenant-a-uploads-v2", nil
		})

	// Delete by access key — Reader's Lookup must miss.
	if err := w.DeleteInternalByAccessKey(ctx, vc.AccessKeyID); err != nil {
		t.Fatalf("delete by AKID: %v", err)
	}
	envtestharness.Eventually(t, 10*time.Second, 100*time.Millisecond, "reader observes delete",
		func() (bool, error) {
			_, ok := reader.Lookup(vc.AccessKeyID)
			return !ok, nil
		})
}

// TestVCStore_RotationOverlap exercises the rotation path: an "active" VC
// gets an expiry annotation and a fresh VC takes over. Reader.Lookup must
// continue to return the expiring credential until ExpiresAt passes.
func TestVCStore_RotationOverlap(t *testing.T) {
	suite := envtestharness.Start(t)
	ctx := envtestharness.WithTimeout(t, 60*time.Second)

	const opsNS = "stowage-system"
	if err := suite.Client.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: opsNS}}); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create ns: %v", err)
	}

	_, reader, _ := startVCManager(t, suite, opsNS)
	w := &vcstore.Writer{Client: suite.Client, Namespace: opsNS}

	old := vcstore.VirtualCredential{
		AccessKeyID:     "AKIAOLD1234",
		SecretAccessKey: "old",
		BucketName:      "rot-bucket",
		BackendName:     "primary",
		ClaimNamespace:  "tenant-rot",
		ClaimName:       "claim",
		ClaimUID:        "uid-rot",
	}
	if err := w.WriteInternal(ctx, old); err != nil {
		t.Fatalf("write old: %v", err)
	}

	envtestharness.Eventually(t, 10*time.Second, 100*time.Millisecond, "reader sees old",
		func() (bool, error) {
			_, ok := reader.Lookup(old.AccessKeyID)
			return ok, nil
		})

	// Mark old as expiring 2s out by patching its annotation directly.
	var sec corev1.Secret
	if err := suite.Client.Get(ctx, types.NamespacedName{
		Namespace: opsNS, Name: vcstore.InternalSecretName(old.AccessKeyID),
	}, &sec); err != nil {
		t.Fatalf("get old: %v", err)
	}
	if sec.Annotations == nil {
		sec.Annotations = map[string]string{}
	}
	expires := time.Now().Add(2 * time.Second).UTC().Format(time.RFC3339)
	sec.Annotations[vcstore.AnnotationExpiresAt] = expires
	if err := suite.Client.Update(ctx, &sec); err != nil {
		t.Fatalf("update old: %v", err)
	}

	// Write the new active VC.
	now := vcstore.VirtualCredential{
		AccessKeyID:     "AKIANEW1234",
		SecretAccessKey: "new",
		BucketName:      "rot-bucket",
		BackendName:     "primary",
		ClaimNamespace:  "tenant-rot",
		ClaimName:       "claim",
		ClaimUID:        "uid-rot",
	}
	if err := w.WriteInternal(ctx, now); err != nil {
		t.Fatalf("write new: %v", err)
	}

	envtestharness.Eventually(t, 10*time.Second, 100*time.Millisecond, "reader sees both",
		func() (bool, error) {
			_, oldOK := reader.Lookup(old.AccessKeyID)
			_, newOK := reader.Lookup(now.AccessKeyID)
			return oldOK && newOK, nil
		})

	// After expiry, Lookup of the old AKID must return false even though the
	// Secret still exists (the Reader honours ExpiresAt).
	envtestharness.Eventually(t, 15*time.Second, 250*time.Millisecond, "old expires",
		func() (bool, error) {
			_, oldOK := reader.Lookup(old.AccessKeyID)
			return !oldOK, nil
		})
}

// TestVCStore_AnonReader_Lifecycle covers the anonymous binding path. The
// Reader is case-insensitive on bucket names, so we cover that explicitly.
func TestVCStore_AnonReader_Lifecycle(t *testing.T) {
	suite := envtestharness.Start(t)
	ctx := envtestharness.WithTimeout(t, 60*time.Second)

	const opsNS = "stowage-system"
	if err := suite.Client.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: opsNS}}); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create ns: %v", err)
	}

	_, _, anon := startVCManager(t, suite, opsNS)
	w := &vcstore.Writer{Client: suite.Client, Namespace: opsNS}

	binding := vcstore.AnonymousBinding{
		BucketName:     "Public-Bucket",
		BackendName:    "primary",
		Mode:           "ReadOnly",
		PerSourceIPRPS: 25,
		ClaimNamespace: "tenant-pub",
		ClaimName:      "pub",
		ClaimUID:       "uid-pub",
	}
	if err := w.WriteAnonymousBinding(ctx, binding); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	envtestharness.Eventually(t, 10*time.Second, 100*time.Millisecond, "anon picks up binding",
		func() (bool, error) {
			b, ok := anon.Lookup("public-bucket") // lowercase lookup
			if !ok {
				return false, nil
			}
			if b.Mode != "ReadOnly" || b.PerSourceIPRPS != 25 {
				return false, nil
			}
			return true, nil
		})

	if err := w.DeleteAnonymousBindingByClaim(ctx, "tenant-pub", "pub"); err != nil {
		t.Fatalf("delete binding: %v", err)
	}
	envtestharness.Eventually(t, 10*time.Second, 100*time.Millisecond, "anon observes delete",
		func() (bool, error) {
			_, ok := anon.Lookup("public-bucket")
			return !ok, nil
		})
}

// TestVCStore_DeleteInternalByClaim_Sweep verifies the bulk delete path: the
// Writer must remove every VC Secret matching a given claim, leaving Secrets
// for other claims untouched.
func TestVCStore_DeleteInternalByClaim_Sweep(t *testing.T) {
	suite := envtestharness.Start(t)
	ctx := envtestharness.WithTimeout(t, 60*time.Second)

	const opsNS = "stowage-system"
	if err := suite.Client.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: opsNS}}); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create ns: %v", err)
	}

	w := &vcstore.Writer{Client: suite.Client, Namespace: opsNS}

	// Two credentials for claim "alpha", one for claim "beta".
	for _, vc := range []vcstore.VirtualCredential{
		{AccessKeyID: "AKIAALPHA1", SecretAccessKey: "s", BucketName: "b", BackendName: "p", ClaimNamespace: "t", ClaimName: "alpha", ClaimUID: "1"},
		{AccessKeyID: "AKIAALPHA2", SecretAccessKey: "s", BucketName: "b", BackendName: "p", ClaimNamespace: "t", ClaimName: "alpha", ClaimUID: "1"},
		{AccessKeyID: "AKIABETA1", SecretAccessKey: "s", BucketName: "b", BackendName: "p", ClaimNamespace: "t", ClaimName: "beta", ClaimUID: "2"},
	} {
		if err := w.WriteInternal(ctx, vc); err != nil {
			t.Fatalf("write %s: %v", vc.AccessKeyID, err)
		}
	}

	if err := w.DeleteInternalByClaim(ctx, "t", "alpha"); err != nil {
		t.Fatalf("delete by claim: %v", err)
	}

	var list corev1.SecretList
	if err := suite.Client.List(ctx, &list, client.InNamespace(opsNS), client.MatchingLabels{
		vcstore.LabelRole: vcstore.RoleVirtualCredential,
	}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Items) != 1 {
		t.Fatalf("expected 1 VC secret left (beta), got %d", len(list.Items))
	}
	if list.Items[0].Labels[vcstore.LabelClaimName] != "beta" {
		t.Fatalf("wrong VC remained: %v", list.Items[0].Labels)
	}
}
