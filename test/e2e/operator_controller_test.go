// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	brokerv1a1 "github.com/stowage-dev/stowage/internal/operator/api/v1alpha1"
	"github.com/stowage-dev/stowage/internal/operator/vcstore"
)

// makeAdminSecret creates a Secret with the AWS_* keys the resolver expects.
func makeAdminSecret(t *testing.T, ctx context.Context, c client.Client, ns, name, ak, sk string) {
	t.Helper()
	if err := c.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		StringData: map[string]string{
			"AWS_ACCESS_KEY_ID":     ak,
			"AWS_SECRET_ACCESS_KEY": sk,
		},
	}); err != nil {
		t.Fatalf("create admin secret: %v", err)
	}
}

// readyBackend builds an S3Backend that points at the supplied endpoint and
// reuses the admin Secret named adminSecret in the operator namespace.
//
// S3Backends are cluster-scoped, so the test gives each one a unique name
// to avoid cross-test collisions when running with -parallel.
func readyBackend(name, opsNamespace, endpoint, adminSecret string) *brokerv1a1.S3Backend {
	return &brokerv1a1.S3Backend{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: brokerv1a1.S3BackendSpec{
			Endpoint:           endpoint,
			Region:             "us-east-1",
			AddressingStyle:    brokerv1a1.AddressingStylePath,
			BucketNameTemplate: "{{ .Namespace }}-{{ .Name }}",
			AdminCredentialsSecretRef: brokerv1a1.AdminCredentialsRef{
				Name:      adminSecret,
				Namespace: opsNamespace,
			},
		},
	}
}

// waitBackendReady blocks until the S3Backend's Ready condition flips to
// the requested status. Returns the observed backend so tests can inspect
// detail.
func waitBackendReady(t *testing.T, c client.Client, name string, want metav1.ConditionStatus) *brokerv1a1.S3Backend {
	t.Helper()
	var b brokerv1a1.S3Backend
	Eventually(t, 60*time.Second, 250*time.Millisecond,
		"backend "+name+" Ready="+string(want),
		func() (bool, error) {
			if err := c.Get(context.Background(), types.NamespacedName{Name: name}, &b); err != nil {
				return false, err
			}
			for _, cond := range b.Status.Conditions {
				if cond.Type == brokerv1a1.ConditionReady {
					return cond.Status == want, nil
				}
			}
			return false, nil
		},
	)
	return &b
}

// waitClaimPhase polls the BucketClaim until status.phase matches.
func waitClaimPhase(t *testing.T, c client.Client, ns, name string, phase brokerv1a1.BucketClaimPhase) *brokerv1a1.BucketClaim {
	t.Helper()
	var bc brokerv1a1.BucketClaim
	Eventually(t, 60*time.Second, 250*time.Millisecond,
		"claim "+ns+"/"+name+" phase="+string(phase),
		func() (bool, error) {
			if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, &bc); err != nil {
				return false, err
			}
			return bc.Status.Phase == phase, nil
		},
	)
	return &bc
}

// deleteBackendOnCleanup registers a t.Cleanup that removes the
// cluster-scoped S3Backend. Without this, S3Backends from one test linger
// into the next.
func deleteBackendOnCleanup(t *testing.T, c client.Client, name string) {
	t.Helper()
	t.Cleanup(func() {
		bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = c.Delete(bg, &brokerv1a1.S3Backend{ObjectMeta: metav1.ObjectMeta{Name: name}})
	})
}

// TestS3BackendReconciler_Ready drives the backend probe loop end-to-end.
// The reconciler resolves admin credentials from a Secret, calls
// ListBuckets against the fake S3 server, and writes
// status.conditions[Ready]=True.
func TestS3BackendReconciler_Ready(t *testing.T) {
	c := MustConnect(t)
	ctx := WithTimeout(t, 90*time.Second)

	opsNS := EnsureOpsNamespace(t, ctx, c.Client)
	backendName := UniqueName("backend-ready")
	adminName := UniqueName("admin")

	fake := NewFakeS3(t)
	fake.SeedBucket("preexisting")
	makeAdminSecret(t, ctx, c.Client, opsNS, adminName, "AKIAFAKE", "fakesecret")

	StartOperatorManager(t, c, ManagerOptions{OpsNamespace: opsNS})
	deleteBackendOnCleanup(t, c.Client, backendName)

	if err := c.Client.Create(ctx, readyBackend(backendName, opsNS, fake.URL(), adminName)); err != nil {
		t.Fatalf("create S3Backend: %v", err)
	}

	got := waitBackendReady(t, c.Client, backendName, metav1.ConditionTrue)
	if got.Status.BucketCount != 1 {
		t.Fatalf("expected BucketCount=1, got %d", got.Status.BucketCount)
	}
}

// TestS3BackendReconciler_BadTemplate verifies invalid templates surface
// as Ready=False with reason TemplateInvalid.
func TestS3BackendReconciler_BadTemplate(t *testing.T) {
	c := MustConnect(t)
	ctx := WithTimeout(t, 90*time.Second)

	opsNS := EnsureOpsNamespace(t, ctx, c.Client)
	backendName := UniqueName("backend-badtpl")
	adminName := UniqueName("admin")

	fake := NewFakeS3(t)
	makeAdminSecret(t, ctx, c.Client, opsNS, adminName, "AKIAFAKE", "fakesecret")

	StartOperatorManager(t, c, ManagerOptions{OpsNamespace: opsNS})
	deleteBackendOnCleanup(t, c.Client, backendName)

	b := readyBackend(backendName, opsNS, fake.URL(), adminName)
	b.Spec.BucketNameTemplate = "{{ .DoesNotExist }}"
	if err := c.Client.Create(ctx, b); err != nil {
		t.Fatalf("create S3Backend: %v", err)
	}

	got := waitBackendReady(t, c.Client, backendName, metav1.ConditionFalse)
	var found bool
	for _, cond := range got.Status.Conditions {
		if cond.Type == brokerv1a1.ConditionReady && cond.Reason == brokerv1a1.ReasonTemplateInvalid {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Ready=False reason=TemplateInvalid, got %+v", got.Status.Conditions)
	}
}

// TestBucketClaimReconciler_HappyPath drives the full claim lifecycle and
// verifies all side effects: finalizer added, bucket created on backend,
// internal credential Secret in opsNS, consumer Secret in claim NS, status
// conditions transition to Bound.
func TestBucketClaimReconciler_HappyPath(t *testing.T) {
	c := MustConnect(t)
	ctx := WithTimeout(t, 120*time.Second)

	opsNS := EnsureOpsNamespace(t, ctx, c.Client)
	tenantNS := NewNamespace(t, ctx, c.Client, "tenant")
	backendName := UniqueName("backend-happy")
	adminName := UniqueName("admin")

	fake := NewFakeS3(t)
	makeAdminSecret(t, ctx, c.Client, opsNS, adminName, "AKIAFAKE", "fakesecret")

	StartOperatorManager(t, c, ManagerOptions{
		OpsNamespace:    opsNS,
		WatchNamespaces: []string{tenantNS},
		ProxyURL:        "http://proxy.test.svc:8080",
	})
	deleteBackendOnCleanup(t, c.Client, backendName)

	if err := c.Client.Create(ctx, readyBackend(backendName, opsNS, fake.URL(), adminName)); err != nil {
		t.Fatalf("create S3Backend: %v", err)
	}
	waitBackendReady(t, c.Client, backendName, metav1.ConditionTrue)

	soft := resource.MustParse("100Mi")
	hard := resource.MustParse("1Gi")
	claim := &brokerv1a1.BucketClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "uploads", Namespace: tenantNS},
		Spec: brokerv1a1.BucketClaimSpec{
			BackendRef:     brokerv1a1.BackendRef{Name: backendName},
			DeletionPolicy: brokerv1a1.DeletionPolicyDelete,
			Quota:          &brokerv1a1.BucketQuota{Soft: &soft, Hard: &hard},
		},
	}
	if err := c.Client.Create(ctx, claim); err != nil {
		t.Fatalf("create BucketClaim: %v", err)
	}

	bound := waitClaimPhase(t, c.Client, tenantNS, "uploads", brokerv1a1.PhaseBound)

	expectedBucket := tenantNS + "-uploads"
	if bound.Status.BucketName != expectedBucket {
		t.Fatalf("bucketName: want %q, got %q", expectedBucket, bound.Status.BucketName)
	}
	if !fake.HasBucket(expectedBucket) {
		t.Fatalf("fake S3 missing bucket %q", expectedBucket)
	}
	if bound.Status.BoundSecretName == "" {
		t.Fatalf("status.boundSecretName empty")
	}
	if bound.Status.AccessKeyID == "" {
		t.Fatalf("status.accessKeyId empty")
	}

	hasFin := false
	for _, f := range bound.Finalizers {
		if f == brokerv1a1.Finalizer {
			hasFin = true
		}
	}
	if !hasFin {
		t.Fatalf("expected finalizer %q on claim, got %v", brokerv1a1.Finalizer, bound.Finalizers)
	}

	var consumer corev1.Secret
	if err := c.Client.Get(ctx, types.NamespacedName{Namespace: tenantNS, Name: bound.Status.BoundSecretName}, &consumer); err != nil {
		t.Fatalf("get consumer secret: %v", err)
	}
	if string(consumer.Data[vcstore.EnvAccessKeyID]) != bound.Status.AccessKeyID {
		t.Fatalf("consumer secret AKID mismatch")
	}
	if string(consumer.Data[vcstore.EnvBucketName]) != expectedBucket {
		t.Fatalf("consumer secret bucket mismatch")
	}

	var vcSecret corev1.Secret
	if err := c.Client.Get(ctx, types.NamespacedName{
		Namespace: opsNS,
		Name:      vcstore.InternalSecretName(bound.Status.AccessKeyID),
	}, &vcSecret); err != nil {
		t.Fatalf("get internal VC secret: %v", err)
	}
	if string(vcSecret.Data[vcstore.DataBucketName]) != expectedBucket {
		t.Fatalf("VC secret bucket mismatch: %s", string(vcSecret.Data[vcstore.DataBucketName]))
	}
	if string(vcSecret.Data[vcstore.DataQuotaSoftBytes]) == "" || string(vcSecret.Data[vcstore.DataQuotaHardBytes]) == "" {
		t.Fatalf("VC secret missing quota: %v", vcSecret.Data)
	}
}

// TestBucketClaimReconciler_BackendNotReady confirms a claim against an
// unready backend stays in Failed/Pending with reason BackendNotReady — and
// recovers when the backend flips Ready.
func TestBucketClaimReconciler_BackendNotReady(t *testing.T) {
	c := MustConnect(t)
	ctx := WithTimeout(t, 120*time.Second)

	opsNS := EnsureOpsNamespace(t, ctx, c.Client)
	tenantNS := NewNamespace(t, ctx, c.Client, "tenant")
	backendName := UniqueName("backend-missing")
	adminName := UniqueName("admin")

	fake := NewFakeS3(t)

	StartOperatorManager(t, c, ManagerOptions{
		OpsNamespace:    opsNS,
		WatchNamespaces: []string{tenantNS},
	})
	deleteBackendOnCleanup(t, c.Client, backendName)

	claim := &brokerv1a1.BucketClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "uploads", Namespace: tenantNS},
		Spec: brokerv1a1.BucketClaimSpec{
			BackendRef: brokerv1a1.BackendRef{Name: backendName},
		},
	}
	if err := c.Client.Create(ctx, claim); err != nil {
		t.Fatalf("create claim: %v", err)
	}

	Eventually(t, 30*time.Second, 250*time.Millisecond,
		"claim Ready=False BackendNotReady", func() (bool, error) {
			var bc brokerv1a1.BucketClaim
			if err := c.Client.Get(ctx, types.NamespacedName{Namespace: tenantNS, Name: "uploads"}, &bc); err != nil {
				return false, err
			}
			for _, cond := range bc.Status.Conditions {
				if cond.Type == brokerv1a1.ConditionReady && cond.Status == metav1.ConditionFalse && cond.Reason == brokerv1a1.ReasonBackendNotReady {
					return true, nil
				}
			}
			return false, nil
		})

	makeAdminSecret(t, ctx, c.Client, opsNS, adminName, "AKIAFAKE", "fakesecret")
	if err := c.Client.Create(ctx, readyBackend(backendName, opsNS, fake.URL(), adminName)); err != nil {
		t.Fatalf("create backend: %v", err)
	}
	waitBackendReady(t, c.Client, backendName, metav1.ConditionTrue)
	waitClaimPhase(t, c.Client, tenantNS, "uploads", brokerv1a1.PhaseBound)
}

// TestBucketClaimReconciler_DeletionPolicyDelete verifies that deleting a
// claim with deletionPolicy=Delete removes the bucket from the backend AND
// the internal VC Secret from the operator namespace, then drops the
// finalizer so apiserver GC completes.
func TestBucketClaimReconciler_DeletionPolicyDelete(t *testing.T) {
	c := MustConnect(t)
	ctx := WithTimeout(t, 150*time.Second)

	opsNS := EnsureOpsNamespace(t, ctx, c.Client)
	tenantNS := NewNamespace(t, ctx, c.Client, "tenant")
	backendName := UniqueName("backend-del")
	adminName := UniqueName("admin")

	fake := NewFakeS3(t)
	makeAdminSecret(t, ctx, c.Client, opsNS, adminName, "AKIAFAKE", "fakesecret")

	StartOperatorManager(t, c, ManagerOptions{
		OpsNamespace:    opsNS,
		WatchNamespaces: []string{tenantNS},
	})
	deleteBackendOnCleanup(t, c.Client, backendName)

	if err := c.Client.Create(ctx, readyBackend(backendName, opsNS, fake.URL(), adminName)); err != nil {
		t.Fatalf("create backend: %v", err)
	}
	waitBackendReady(t, c.Client, backendName, metav1.ConditionTrue)

	claim := &brokerv1a1.BucketClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "ephemeral", Namespace: tenantNS},
		Spec: brokerv1a1.BucketClaimSpec{
			BackendRef:     brokerv1a1.BackendRef{Name: backendName},
			DeletionPolicy: brokerv1a1.DeletionPolicyDelete,
		},
	}
	if err := c.Client.Create(ctx, claim); err != nil {
		t.Fatalf("create claim: %v", err)
	}
	bound := waitClaimPhase(t, c.Client, tenantNS, "ephemeral", brokerv1a1.PhaseBound)
	bucket := bound.Status.BucketName

	if err := c.Client.Delete(ctx, bound); err != nil {
		t.Fatalf("delete claim: %v", err)
	}

	Eventually(t, 30*time.Second, 250*time.Millisecond,
		"claim removed from apiserver", func() (bool, error) {
			var bc brokerv1a1.BucketClaim
			err := c.Client.Get(ctx, types.NamespacedName{Namespace: tenantNS, Name: "ephemeral"}, &bc)
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		})

	if fake.HasBucket(bucket) {
		t.Fatalf("expected bucket %q deleted from backend", bucket)
	}

	var list corev1.SecretList
	if err := c.Client.List(ctx, &list, client.InNamespace(opsNS), client.MatchingLabels{
		vcstore.LabelRole:      vcstore.RoleVirtualCredential,
		vcstore.LabelClaimNS:   tenantNS,
		vcstore.LabelClaimName: "ephemeral",
	}); err != nil {
		t.Fatalf("list internal secrets: %v", err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("expected internal VC secrets cleaned up, got %d", len(list.Items))
	}
}

// TestBucketClaimReconciler_AnonymousBindingLifecycle exercises the
// publish/retract path for AnonymousAccess. Toggling Mode None → ReadOnly
// → None must drive the proxy-facing binding Secret correctly.
func TestBucketClaimReconciler_AnonymousBindingLifecycle(t *testing.T) {
	c := MustConnect(t)
	ctx := WithTimeout(t, 150*time.Second)

	opsNS := EnsureOpsNamespace(t, ctx, c.Client)
	tenantNS := NewNamespace(t, ctx, c.Client, "tenant")
	backendName := UniqueName("backend-anon")
	adminName := UniqueName("admin")

	fake := NewFakeS3(t)
	makeAdminSecret(t, ctx, c.Client, opsNS, adminName, "AKIAFAKE", "fakesecret")

	StartOperatorManager(t, c, ManagerOptions{
		OpsNamespace:    opsNS,
		WatchNamespaces: []string{tenantNS},
	})
	deleteBackendOnCleanup(t, c.Client, backendName)

	if err := c.Client.Create(ctx, readyBackend(backendName, opsNS, fake.URL(), adminName)); err != nil {
		t.Fatalf("create backend: %v", err)
	}
	waitBackendReady(t, c.Client, backendName, metav1.ConditionTrue)

	claim := &brokerv1a1.BucketClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "public", Namespace: tenantNS},
		Spec: brokerv1a1.BucketClaimSpec{
			BackendRef:      brokerv1a1.BackendRef{Name: backendName},
			AnonymousAccess: &brokerv1a1.AnonymousAccess{Mode: brokerv1a1.AnonymousModeReadOnly},
		},
	}
	if err := c.Client.Create(ctx, claim); err != nil {
		t.Fatalf("create claim: %v", err)
	}
	waitClaimPhase(t, c.Client, tenantNS, "public", brokerv1a1.PhaseBound)

	bindingName := vcstore.AnonymousBindingSecretName(tenantNS, "public")
	Eventually(t, 30*time.Second, 250*time.Millisecond,
		"anonymous binding secret present", func() (bool, error) {
			var s corev1.Secret
			err := c.Client.Get(ctx, types.NamespacedName{Namespace: opsNS, Name: bindingName}, &s)
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return err == nil, err
		})

	var live brokerv1a1.BucketClaim
	if err := c.Client.Get(ctx, types.NamespacedName{Namespace: tenantNS, Name: "public"}, &live); err != nil {
		t.Fatalf("get claim: %v", err)
	}
	live.Spec.AnonymousAccess.Mode = brokerv1a1.AnonymousModeNone
	if err := c.Client.Update(ctx, &live); err != nil {
		t.Fatalf("update claim: %v", err)
	}
	Eventually(t, 30*time.Second, 250*time.Millisecond,
		"anonymous binding secret deleted", func() (bool, error) {
			var s corev1.Secret
			err := c.Client.Get(ctx, types.NamespacedName{Namespace: opsNS, Name: bindingName}, &s)
			return apierrors.IsNotFound(err), nil
		})
}
