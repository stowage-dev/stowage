// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build envtest

package controller_test

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	brokerv1a1 "github.com/stowage-dev/stowage/internal/operator/api/v1alpha1"
	"github.com/stowage-dev/stowage/internal/operator/controller"
	"github.com/stowage-dev/stowage/internal/operator/credentials"
	envtestharness "github.com/stowage-dev/stowage/internal/operator/test/envtest"
	"github.com/stowage-dev/stowage/internal/operator/vcstore"
)

// startManager wires both reconcilers into a manager backed by the envtest
// control plane and starts it. The manager is stopped on test cleanup.
//
// opsNamespace is the synthetic operator namespace where internal Secrets
// (virtual credentials, anonymous bindings) land. Tests pre-create it.
func startManager(t *testing.T, suite *envtestharness.Suite, opsNamespace, fakeS3URL string) {
	t.Helper()

	mgr, err := ctrl.NewManager(suite.Cfg, ctrl.Options{
		Scheme:  suite.Scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
		// Each test starts a fresh manager but controller-runtime's metrics
		// registry is process-global; without SkipNameValidation the second
		// test's "s3backend" controller registration collides on prometheus
		// metric names.
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	resolver := &credentials.Resolver{Client: mgr.GetClient()}
	writer := &vcstore.Writer{Client: mgr.GetClient(), Namespace: opsNamespace}

	if err := (&controller.S3BackendReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Resolver: resolver,
	}).SetupWithManager(mgr); err != nil {
		t.Fatalf("setup S3Backend reconciler: %v", err)
	}
	if err := (&controller.BucketClaimReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Resolver: resolver,
		Writer:   writer,
		Recorder: record.NewFakeRecorder(64),
		ProxyURL: "http://proxy.test.svc:8080",
		OpsNS:    opsNamespace,
	}).SetupWithManager(mgr); err != nil {
		t.Fatalf("setup BucketClaim reconciler: %v", err)
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
			t.Logf("manager did not exit within 15s after cancel")
		}
	})
}

// ensureNamespace creates ns if it does not already exist.
func ensureNamespace(t *testing.T, ctx context.Context, c client.Client, ns string) {
	t.Helper()
	err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create namespace %s: %v", ns, err)
	}
}

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

// readyBackend builds an S3Backend that points at the fake S3 server and
// reuses the admin Secret named adminSecret in the operator namespace.
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

// waitBackendReady blocks until the S3Backend's Ready condition flips to the
// requested status. Returns the observed backend so tests can inspect detail.
func waitBackendReady(t *testing.T, c client.Client, name string, want metav1.ConditionStatus) *brokerv1a1.S3Backend {
	t.Helper()
	var b brokerv1a1.S3Backend
	envtestharness.Eventually(t, 30*time.Second, 250*time.Millisecond,
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
	envtestharness.Eventually(t, 30*time.Second, 250*time.Millisecond,
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

// TestS3BackendReconciler_Ready drives the backend probe loop end-to-end. The
// reconciler resolves admin credentials from a Secret, calls ListBuckets
// against the fake S3 server, and writes status.conditions[Ready]=True.
func TestS3BackendReconciler_Ready(t *testing.T) {
	suite := envtestharness.Start(t)
	ctx := envtestharness.WithTimeout(t, 60*time.Second)

	const opsNS = "stowage-system"
	ensureNamespace(t, ctx, suite.Client, opsNS)

	fake := envtestharness.NewFakeS3(t)
	fake.SeedBucket("preexisting")
	makeAdminSecret(t, ctx, suite.Client, opsNS, "backend-admin", "AKIAFAKE", "fakesecret")

	startManager(t, suite, opsNS, fake.URL())

	if err := suite.Client.Create(ctx, readyBackend("primary", opsNS, fake.URL(), "backend-admin")); err != nil {
		t.Fatalf("create S3Backend: %v", err)
	}

	got := waitBackendReady(t, suite.Client, "primary", metav1.ConditionTrue)
	if got.Status.BucketCount != 1 {
		t.Fatalf("expected BucketCount=1, got %d", got.Status.BucketCount)
	}
}

// TestS3BackendReconciler_BadTemplate verifies invalid templates surface as
// Ready=False with reason TemplateInvalid.
func TestS3BackendReconciler_BadTemplate(t *testing.T) {
	suite := envtestharness.Start(t)
	ctx := envtestharness.WithTimeout(t, 60*time.Second)

	const opsNS = "stowage-system"
	ensureNamespace(t, ctx, suite.Client, opsNS)
	fake := envtestharness.NewFakeS3(t)
	makeAdminSecret(t, ctx, suite.Client, opsNS, "backend-admin", "AKIAFAKE", "fakesecret")

	startManager(t, suite, opsNS, fake.URL())

	b := readyBackend("primary", opsNS, fake.URL(), "backend-admin")
	b.Spec.BucketNameTemplate = "{{ .DoesNotExist }}"
	if err := suite.Client.Create(ctx, b); err != nil {
		t.Fatalf("create S3Backend: %v", err)
	}

	got := waitBackendReady(t, suite.Client, "primary", metav1.ConditionFalse)
	var found bool
	for _, c := range got.Status.Conditions {
		if c.Type == brokerv1a1.ConditionReady && c.Reason == brokerv1a1.ReasonTemplateInvalid {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Ready=False reason=TemplateInvalid, got %+v", got.Status.Conditions)
	}
}

// TestBucketClaimReconciler_HappyPath drives the full claim lifecycle and
// verifies all the side effects: finalizer added, bucket created on backend,
// internal credential Secret in opsNS, consumer Secret in claim NS, status
// conditions transition to Bound.
func TestBucketClaimReconciler_HappyPath(t *testing.T) {
	suite := envtestharness.Start(t)
	ctx := envtestharness.WithTimeout(t, 90*time.Second)

	const (
		opsNS    = "stowage-system"
		tenantNS = "tenant-a"
	)
	ensureNamespace(t, ctx, suite.Client, opsNS)
	ensureNamespace(t, ctx, suite.Client, tenantNS)

	fake := envtestharness.NewFakeS3(t)
	makeAdminSecret(t, ctx, suite.Client, opsNS, "backend-admin", "AKIAFAKE", "fakesecret")

	startManager(t, suite, opsNS, fake.URL())

	// 1. Backend must be Ready before claims can bind.
	if err := suite.Client.Create(ctx, readyBackend("primary", opsNS, fake.URL(), "backend-admin")); err != nil {
		t.Fatalf("create S3Backend: %v", err)
	}
	waitBackendReady(t, suite.Client, "primary", metav1.ConditionTrue)

	// 2. Submit the claim with a generous quota.
	soft := resource.MustParse("100Mi")
	hard := resource.MustParse("1Gi")
	claim := &brokerv1a1.BucketClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "uploads", Namespace: tenantNS},
		Spec: brokerv1a1.BucketClaimSpec{
			BackendRef:     brokerv1a1.BackendRef{Name: "primary"},
			DeletionPolicy: brokerv1a1.DeletionPolicyDelete,
			Quota:          &brokerv1a1.BucketQuota{Soft: &soft, Hard: &hard},
		},
	}
	if err := suite.Client.Create(ctx, claim); err != nil {
		t.Fatalf("create BucketClaim: %v", err)
	}

	// 3. Wait for Bound.
	bound := waitClaimPhase(t, suite.Client, tenantNS, "uploads", brokerv1a1.PhaseBound)

	// 4. Side effects.
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

	// Finalizer attached.
	hasFin := false
	for _, f := range bound.Finalizers {
		if f == brokerv1a1.Finalizer {
			hasFin = true
		}
	}
	if !hasFin {
		t.Fatalf("expected finalizer %q on claim, got %v", brokerv1a1.Finalizer, bound.Finalizers)
	}

	// Consumer Secret in tenant namespace.
	var consumer corev1.Secret
	if err := suite.Client.Get(ctx, types.NamespacedName{Namespace: tenantNS, Name: bound.Status.BoundSecretName}, &consumer); err != nil {
		t.Fatalf("get consumer secret: %v", err)
	}
	if string(consumer.Data[vcstore.EnvAccessKeyID]) != bound.Status.AccessKeyID {
		t.Fatalf("consumer secret AKID mismatch")
	}
	if string(consumer.Data[vcstore.EnvBucketName]) != expectedBucket {
		t.Fatalf("consumer secret bucket mismatch")
	}

	// Internal VC Secret in operator namespace, with quota.
	var vcSecret corev1.Secret
	if err := suite.Client.Get(ctx, types.NamespacedName{
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
	suite := envtestharness.Start(t)
	ctx := envtestharness.WithTimeout(t, 90*time.Second)

	const (
		opsNS    = "stowage-system"
		tenantNS = "tenant-b"
	)
	ensureNamespace(t, ctx, suite.Client, opsNS)
	ensureNamespace(t, ctx, suite.Client, tenantNS)

	fake := envtestharness.NewFakeS3(t)
	// Don't create the backend yet — the claim's reconciler should report
	// BackendNotReady with reason "backend not found".

	startManager(t, suite, opsNS, fake.URL())

	claim := &brokerv1a1.BucketClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "uploads", Namespace: tenantNS},
		Spec: brokerv1a1.BucketClaimSpec{
			BackendRef: brokerv1a1.BackendRef{Name: "missing"},
		},
	}
	if err := suite.Client.Create(ctx, claim); err != nil {
		t.Fatalf("create claim: %v", err)
	}

	envtestharness.Eventually(t, 30*time.Second, 250*time.Millisecond,
		"claim Ready=False BackendNotReady", func() (bool, error) {
			var bc brokerv1a1.BucketClaim
			if err := suite.Client.Get(ctx, types.NamespacedName{Namespace: tenantNS, Name: "uploads"}, &bc); err != nil {
				return false, err
			}
			for _, c := range bc.Status.Conditions {
				if c.Type == brokerv1a1.ConditionReady && c.Status == metav1.ConditionFalse && c.Reason == brokerv1a1.ReasonBackendNotReady {
					return true, nil
				}
			}
			return false, nil
		})

	// Now create the backend + admin secret. Claim should converge to Bound.
	makeAdminSecret(t, ctx, suite.Client, opsNS, "backend-admin", "AKIAFAKE", "fakesecret")
	b := readyBackend("missing", opsNS, fake.URL(), "backend-admin")
	if err := suite.Client.Create(ctx, b); err != nil {
		t.Fatalf("create backend: %v", err)
	}
	waitBackendReady(t, suite.Client, "missing", metav1.ConditionTrue)
	waitClaimPhase(t, suite.Client, tenantNS, "uploads", brokerv1a1.PhaseBound)
}

// TestBucketClaimReconciler_DeletionPolicyDelete verifies that deleting a
// claim with deletionPolicy=Delete removes the bucket from the backend AND
// the internal VC Secret from the operator namespace, then drops the
// finalizer so apiserver GC completes.
func TestBucketClaimReconciler_DeletionPolicyDelete(t *testing.T) {
	suite := envtestharness.Start(t)
	ctx := envtestharness.WithTimeout(t, 120*time.Second)

	const (
		opsNS    = "stowage-system"
		tenantNS = "tenant-c"
	)
	ensureNamespace(t, ctx, suite.Client, opsNS)
	ensureNamespace(t, ctx, suite.Client, tenantNS)

	fake := envtestharness.NewFakeS3(t)
	makeAdminSecret(t, ctx, suite.Client, opsNS, "backend-admin", "AKIAFAKE", "fakesecret")

	startManager(t, suite, opsNS, fake.URL())

	if err := suite.Client.Create(ctx, readyBackend("primary", opsNS, fake.URL(), "backend-admin")); err != nil {
		t.Fatalf("create backend: %v", err)
	}
	waitBackendReady(t, suite.Client, "primary", metav1.ConditionTrue)

	claim := &brokerv1a1.BucketClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "ephemeral", Namespace: tenantNS},
		Spec: brokerv1a1.BucketClaimSpec{
			BackendRef:     brokerv1a1.BackendRef{Name: "primary"},
			DeletionPolicy: brokerv1a1.DeletionPolicyDelete,
		},
	}
	if err := suite.Client.Create(ctx, claim); err != nil {
		t.Fatalf("create claim: %v", err)
	}
	bound := waitClaimPhase(t, suite.Client, tenantNS, "ephemeral", brokerv1a1.PhaseBound)
	bucket := bound.Status.BucketName

	// Delete the claim — the finalizer must drive cleanup.
	if err := suite.Client.Delete(ctx, bound); err != nil {
		t.Fatalf("delete claim: %v", err)
	}

	envtestharness.Eventually(t, 30*time.Second, 250*time.Millisecond,
		"claim removed from apiserver", func() (bool, error) {
			var bc brokerv1a1.BucketClaim
			err := suite.Client.Get(ctx, types.NamespacedName{Namespace: tenantNS, Name: "ephemeral"}, &bc)
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		})

	if fake.HasBucket(bucket) {
		t.Fatalf("expected bucket %q deleted from backend", bucket)
	}

	// Internal VC Secret should be gone too.
	var list corev1.SecretList
	if err := suite.Client.List(ctx, &list, client.InNamespace(opsNS), client.MatchingLabels{
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
// publish/retract path for AnonymousAccess. Toggling Mode None ⇒ ReadOnly ⇒
// None must drive the proxy-facing binding Secret correctly.
func TestBucketClaimReconciler_AnonymousBindingLifecycle(t *testing.T) {
	suite := envtestharness.Start(t)
	ctx := envtestharness.WithTimeout(t, 120*time.Second)

	const (
		opsNS    = "stowage-system"
		tenantNS = "tenant-d"
	)
	ensureNamespace(t, ctx, suite.Client, opsNS)
	ensureNamespace(t, ctx, suite.Client, tenantNS)

	fake := envtestharness.NewFakeS3(t)
	makeAdminSecret(t, ctx, suite.Client, opsNS, "backend-admin", "AKIAFAKE", "fakesecret")
	startManager(t, suite, opsNS, fake.URL())

	if err := suite.Client.Create(ctx, readyBackend("primary", opsNS, fake.URL(), "backend-admin")); err != nil {
		t.Fatalf("create backend: %v", err)
	}
	waitBackendReady(t, suite.Client, "primary", metav1.ConditionTrue)

	claim := &brokerv1a1.BucketClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "public", Namespace: tenantNS},
		Spec: brokerv1a1.BucketClaimSpec{
			BackendRef:      brokerv1a1.BackendRef{Name: "primary"},
			AnonymousAccess: &brokerv1a1.AnonymousAccess{Mode: brokerv1a1.AnonymousModeReadOnly},
		},
	}
	if err := suite.Client.Create(ctx, claim); err != nil {
		t.Fatalf("create claim: %v", err)
	}
	waitClaimPhase(t, suite.Client, tenantNS, "public", brokerv1a1.PhaseBound)

	bindingName := vcstore.AnonymousBindingSecretName(tenantNS, "public")
	envtestharness.Eventually(t, 15*time.Second, 250*time.Millisecond,
		"anonymous binding secret present", func() (bool, error) {
			var s corev1.Secret
			err := suite.Client.Get(ctx, types.NamespacedName{Namespace: opsNS, Name: bindingName}, &s)
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return err == nil, err
		})

	// Flip back to None — binding must be removed.
	var live brokerv1a1.BucketClaim
	if err := suite.Client.Get(ctx, types.NamespacedName{Namespace: tenantNS, Name: "public"}, &live); err != nil {
		t.Fatalf("get claim: %v", err)
	}
	live.Spec.AnonymousAccess.Mode = brokerv1a1.AnonymousModeNone
	if err := suite.Client.Update(ctx, &live); err != nil {
		t.Fatalf("update claim: %v", err)
	}
	envtestharness.Eventually(t, 15*time.Second, 250*time.Millisecond,
		"anonymous binding secret deleted", func() (bool, error) {
			var s corev1.Secret
			err := suite.Client.Get(ctx, types.NamespacedName{Namespace: opsNS, Name: bindingName}, &s)
			return apierrors.IsNotFound(err), nil
		})
}
