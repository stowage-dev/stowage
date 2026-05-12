// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stowage-dev/stowage/internal/operator/vcstore"
)

// startVCManager runs a controller-runtime manager so the vcstore Reader
// and AnonReader can attach to a real cache backed by the e2e cluster
// apiserver. The reader/anon-reader are returned to the caller for
// Lookup-style assertions.
func startVCManager(t *testing.T, c *Cluster, opsNS string) (*vcstore.Reader, *vcstore.AnonReader) {
	t.Helper()

	mgr := NewBareManager(t, c)

	reader, err := vcstore.NewReader(mgr, opsNS)
	if err != nil {
		t.Fatalf("new reader: %v", err)
	}
	anon, err := vcstore.NewAnonReader(mgr, opsNS)
	if err != nil {
		t.Fatalf("new anon reader: %v", err)
	}

	AddRunnable(t, mgr, "vcstore-reader", reader.Start)
	AddRunnable(t, mgr, "vcstore-anon", anon.Start)

	Run(t, mgr)
	return reader, anon
}

// vcLabelsForClaim returns the labels the vcstore Writer attaches to every
// internal VC Secret for a given claim — used by t.Cleanup to scrub
// per-test state out of the shared ops namespace.
func vcLabelsForClaim(claimNS, claimName string) map[string]string {
	return map[string]string{
		vcstore.LabelRole:      vcstore.RoleVirtualCredential,
		vcstore.LabelClaimNS:   claimNS,
		vcstore.LabelClaimName: claimName,
	}
}

// TestVCStore_WriterReader_Roundtrip writes a virtual credential through
// the real Writer and verifies the Reader picks it up via the manager's
// cache, then survives an update and a delete.
func TestVCStore_WriterReader_Roundtrip(t *testing.T) {
	c := MustConnect(t)
	ctx := WithTimeout(t, 90*time.Second)

	opsNS := EnsureOpsNamespace(t, ctx, c.Client)
	CleanupSecretsByLabels(t, c.Client, opsNS, vcLabelsForClaim("tenant-rt", "uploads"))

	reader, _ := startVCManager(t, c, opsNS)

	w := &vcstore.Writer{Client: c.Client, Namespace: opsNS}
	vc := vcstore.VirtualCredential{
		AccessKeyID:     "AKIATEST1234567",
		SecretAccessKey: "secrettest",
		BucketName:      "tenant-rt-uploads",
		BackendName:     "primary",
		ClaimNamespace:  "tenant-rt",
		ClaimName:       "uploads",
		ClaimUID:        "uid-1",
		QuotaSoftBytes:  1 << 20,
		QuotaHardBytes:  10 << 20,
	}
	if err := w.WriteInternal(ctx, vc); err != nil {
		t.Fatalf("write internal: %v", err)
	}

	Eventually(t, 15*time.Second, 100*time.Millisecond, "reader observes credential",
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

	vc.BucketName = "tenant-rt-uploads-v2"
	if err := w.WriteInternal(ctx, vc); err != nil {
		t.Fatalf("update internal: %v", err)
	}
	Eventually(t, 15*time.Second, 100*time.Millisecond, "reader observes update",
		func() (bool, error) {
			got, ok := reader.Lookup(vc.AccessKeyID)
			if !ok {
				return false, nil
			}
			return got.BucketName == "tenant-rt-uploads-v2", nil
		})

	if err := w.DeleteInternalByAccessKey(ctx, vc.AccessKeyID); err != nil {
		t.Fatalf("delete by AKID: %v", err)
	}
	Eventually(t, 15*time.Second, 100*time.Millisecond, "reader observes delete",
		func() (bool, error) {
			_, ok := reader.Lookup(vc.AccessKeyID)
			return !ok, nil
		})
}

// TestVCStore_RotationOverlap exercises the rotation path: an "active" VC
// gets an expiry annotation and a fresh VC takes over. Reader.Lookup must
// continue to return the expiring credential until ExpiresAt passes.
func TestVCStore_RotationOverlap(t *testing.T) {
	c := MustConnect(t)
	ctx := WithTimeout(t, 90*time.Second)

	opsNS := EnsureOpsNamespace(t, ctx, c.Client)
	CleanupSecretsByLabels(t, c.Client, opsNS, vcLabelsForClaim("tenant-rot", "claim"))

	reader, _ := startVCManager(t, c, opsNS)
	w := &vcstore.Writer{Client: c.Client, Namespace: opsNS}

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

	Eventually(t, 15*time.Second, 100*time.Millisecond, "reader sees old",
		func() (bool, error) {
			_, ok := reader.Lookup(old.AccessKeyID)
			return ok, nil
		})

	var sec corev1.Secret
	if err := c.Client.Get(ctx, types.NamespacedName{
		Namespace: opsNS, Name: vcstore.InternalSecretName(old.AccessKeyID),
	}, &sec); err != nil {
		t.Fatalf("get old: %v", err)
	}
	if sec.Annotations == nil {
		sec.Annotations = map[string]string{}
	}
	expires := time.Now().Add(2 * time.Second).UTC().Format(time.RFC3339)
	sec.Annotations[vcstore.AnnotationExpiresAt] = expires
	if err := c.Client.Update(ctx, &sec); err != nil {
		t.Fatalf("update old: %v", err)
	}

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

	Eventually(t, 15*time.Second, 100*time.Millisecond, "reader sees both",
		func() (bool, error) {
			_, oldOK := reader.Lookup(old.AccessKeyID)
			_, newOK := reader.Lookup(now.AccessKeyID)
			return oldOK && newOK, nil
		})

	Eventually(t, 30*time.Second, 250*time.Millisecond, "old expires",
		func() (bool, error) {
			_, oldOK := reader.Lookup(old.AccessKeyID)
			return !oldOK, nil
		})
}

// TestVCStore_AnonReader_Lifecycle covers the anonymous binding path. The
// Reader is case-insensitive on bucket names, so we cover that explicitly.
func TestVCStore_AnonReader_Lifecycle(t *testing.T) {
	c := MustConnect(t)
	ctx := WithTimeout(t, 90*time.Second)

	opsNS := EnsureOpsNamespace(t, ctx, c.Client)

	_, anon := startVCManager(t, c, opsNS)
	w := &vcstore.Writer{Client: c.Client, Namespace: opsNS}

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
	t.Cleanup(func() {
		bg, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = w.DeleteAnonymousBindingByClaim(bg, "tenant-pub", "pub")
	})

	Eventually(t, 15*time.Second, 100*time.Millisecond, "anon picks up binding",
		func() (bool, error) {
			b, ok := anon.Lookup("public-bucket")
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
	Eventually(t, 15*time.Second, 100*time.Millisecond, "anon observes delete",
		func() (bool, error) {
			_, ok := anon.Lookup("public-bucket")
			return !ok, nil
		})
}

// TestVCStore_DeleteInternalByClaim_Sweep verifies the bulk delete path:
// the Writer must remove every VC Secret matching a given claim, leaving
// Secrets for other claims untouched.
func TestVCStore_DeleteInternalByClaim_Sweep(t *testing.T) {
	c := MustConnect(t)
	ctx := WithTimeout(t, 60*time.Second)

	opsNS := EnsureOpsNamespace(t, ctx, c.Client)

	// Use uniquely-named claim labels so this test does not race other
	// tests that share the ops namespace.
	alpha := UniqueName("alpha")
	beta := UniqueName("beta")
	CleanupSecretsByLabels(t, c.Client, opsNS, vcLabelsForClaim("t", alpha))
	CleanupSecretsByLabels(t, c.Client, opsNS, vcLabelsForClaim("t", beta))

	w := &vcstore.Writer{Client: c.Client, Namespace: opsNS}

	for _, vc := range []vcstore.VirtualCredential{
		{AccessKeyID: UniqueName("akid-a"), SecretAccessKey: "s", BucketName: "b", BackendName: "p", ClaimNamespace: "t", ClaimName: alpha, ClaimUID: "1"},
		{AccessKeyID: UniqueName("akid-a"), SecretAccessKey: "s", BucketName: "b", BackendName: "p", ClaimNamespace: "t", ClaimName: alpha, ClaimUID: "1"},
		{AccessKeyID: UniqueName("akid-b"), SecretAccessKey: "s", BucketName: "b", BackendName: "p", ClaimNamespace: "t", ClaimName: beta, ClaimUID: "2"},
	} {
		if err := w.WriteInternal(ctx, vc); err != nil {
			t.Fatalf("write %s: %v", vc.AccessKeyID, err)
		}
	}

	if err := w.DeleteInternalByClaim(ctx, "t", alpha); err != nil {
		t.Fatalf("delete by claim: %v", err)
	}

	// alpha credentials gone; beta survives.
	var alphaList corev1.SecretList
	if err := c.Client.List(ctx, &alphaList, client.InNamespace(opsNS), client.MatchingLabels(vcLabelsForClaim("t", alpha))); err != nil {
		t.Fatalf("list alpha: %v", err)
	}
	if len(alphaList.Items) != 0 {
		t.Fatalf("expected 0 alpha VC secrets, got %d", len(alphaList.Items))
	}

	var betaList corev1.SecretList
	if err := c.Client.List(ctx, &betaList, client.InNamespace(opsNS), client.MatchingLabels(vcLabelsForClaim("t", beta))); err != nil {
		t.Fatalf("list beta: %v", err)
	}
	if len(betaList.Items) != 1 {
		t.Fatalf("expected 1 beta VC secret, got %d", len(betaList.Items))
	}
	if betaList.Items[0].Labels[vcstore.LabelClaimName] != beta {
		t.Fatalf("wrong beta secret labels: %v", betaList.Items[0].Labels)
	}
}
