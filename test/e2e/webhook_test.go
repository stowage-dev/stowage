// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build e2e

package e2e

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	admissionregv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	crenvtest "sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	webhookserver "sigs.k8s.io/controller-runtime/pkg/webhook"

	brokerv1a1 "github.com/stowage-dev/stowage/internal/operator/api/v1alpha1"
	"github.com/stowage-dev/stowage/internal/operator/webhook"
)

// webhookEnv is the shared admission-webhook fixture. The cert generation
// and ValidatingWebhookConfiguration install dance is expensive (and racy
// to re-run inside one test binary because the configurations are
// cluster-scoped), so all webhook tests share a single fixture set up on
// first use and torn down by t.Cleanup on the first test to claim it.
type webhookEnv struct {
	opts *crenvtest.WebhookInstallOptions
	mgr  *ManagerHandle
}

var (
	webhookEnvOnce sync.Once
	webhookEnvVal  *webhookEnv
	webhookEnvErr  error
)

// startWebhookEnv brings up the webhook manager once per test binary. We
// can't safely re-install ValidatingWebhookConfigurations between tests
// because the kind apiserver caches authoritative CA bundles per webhook
// name; reinstalling mid-run causes intermittent admission failures while
// the apiserver refreshes its kubeconfig view.
//
// The manager's webhook server binds on 0.0.0.0 so it's reachable from
// the kind apiserver via the docker bridge gateway. WebhookInstallOptions
// generates the CA, writes serving certs, picks a free port, and patches
// the ValidatingWebhookConfigurations to use a URL pointing at
// LocalServingHostExternalName.
//
// The manager runs via RunPersistent so it survives the lifetime of the
// test binary — tying its cleanup to the first test's t.Cleanup would
// tear it down before later webhook tests get a chance to use it.
func startWebhookEnv(t *testing.T) *webhookEnv {
	t.Helper()

	webhookEnvOnce.Do(initWebhookEnv)
	if webhookEnvErr != nil {
		t.Fatalf("start webhook env: %v", webhookEnvErr)
	}

	// Probe the webhook on every test entry. The first test additionally
	// pays the warmup polling cost inside initWebhookEnv; later tests just
	// confirm the listener is still serving.
	c := MustConnect(t)
	ctx := WithTimeout(t, 30*time.Second)
	Eventually(t, 30*time.Second, 200*time.Millisecond, "webhook reachable",
		func() (bool, error) {
			warmupName := "warmup-" + UniqueName("")
			err := c.Client.Create(ctx, &brokerv1a1.S3Backend{
				ObjectMeta: metav1.ObjectMeta{Name: warmupName},
				Spec: brokerv1a1.S3BackendSpec{
					Endpoint:           "http://example.test",
					BucketNameTemplate: "{{ .DoesNotExist }}",
					AdminCredentialsSecretRef: brokerv1a1.AdminCredentialsRef{
						Name: "admin", Namespace: webhookOpsNamespace,
					},
				},
			})
			return err != nil && strings.Contains(err.Error(), "bucketNameTemplate"), nil
		})

	return webhookEnvVal
}

// initWebhookEnv is the sync.Once-protected initializer behind
// startWebhookEnv. It constructs the manager + webhook server +
// ValidatingWebhookConfiguration and leaks them for the rest of the test
// binary's lifetime. Failures are stashed in webhookEnvErr so every
// dependent test surfaces the same error.
func initWebhookEnv() {
	c, err := Connect()
	if err != nil {
		webhookEnvErr = err
		return
	}

	opts := &crenvtest.WebhookInstallOptions{
		ValidatingWebhooks:           validatingWebhookConfigs(),
		LocalServingHost:             "0.0.0.0",
		LocalServingHostExternalName: webhookExternalHost(),
	}
	if err := opts.Install(c.Cfg); err != nil {
		webhookEnvErr = err
		return
	}

	// Build the manager without going through NewBareManager because the
	// helper requires *testing.T; initWebhookEnv runs from sync.Once and
	// doesn't have one available.
	mgr, err := ctrl.NewManager(c.Cfg, ctrl.Options{
		Scheme:     c.Scheme,
		Metrics:    metricsserver.Options{BindAddress: "0"},
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
		WebhookServer: webhookserver.NewServer(webhookserver.Options{
			Host:    opts.LocalServingHost,
			Port:    opts.LocalServingPort,
			CertDir: opts.LocalServingCertDir,
		}),
	})
	if err != nil {
		webhookEnvErr = err
		return
	}
	if err := (&webhook.S3BackendValidator{OpsNamespace: webhookOpsNamespace}).SetupWithManager(mgr); err != nil {
		webhookEnvErr = err
		return
	}
	if err := (&webhook.BucketClaimValidator{}).SetupWithManager(mgr); err != nil {
		webhookEnvErr = err
		return
	}

	handle, err := RunPersistent(mgr)
	if err != nil {
		webhookEnvErr = err
		return
	}
	webhookEnvVal = &webhookEnv{opts: opts, mgr: handle}
}

// validatingWebhookConfigs builds the ValidatingWebhookConfiguration list
// installed into the cluster. We use a synthetic name (not the chart's
// "stowage-validating-webhook") so installing the chart in parallel CI
// jobs doesn't collide with the one we install for in-process testing.
//
// WebhookInstallOptions.Install patches each config's CABundle with the
// generated CA and rewrites ClientConfig.Service to ClientConfig.URL
// pointing at the local serving host:port.
func validatingWebhookConfigs() []*admissionregv1.ValidatingWebhookConfiguration {
	failurePolicy := admissionregv1.Fail
	sideEffects := admissionregv1.SideEffectClassNone
	scopeAll := admissionregv1.AllScopes

	pathBC := "/validate-broker-stowage-io-v1alpha1-bucketclaim"
	pathBE := "/validate-broker-stowage-io-v1alpha1-s3backend"

	return []*admissionregv1.ValidatingWebhookConfiguration{{
		ObjectMeta: metav1.ObjectMeta{Name: "stowage-e2e-validator"},
		Webhooks: []admissionregv1.ValidatingWebhook{
			{
				Name:                    "bucketclaim.e2e.stowage.test",
				FailurePolicy:           &failurePolicy,
				SideEffects:             &sideEffects,
				AdmissionReviewVersions: []string{"v1"},
				ClientConfig: admissionregv1.WebhookClientConfig{
					Service: &admissionregv1.ServiceReference{
						Name:      "webhook",
						Namespace: "default",
						Path:      &pathBC,
					},
				},
				Rules: []admissionregv1.RuleWithOperations{{
					Operations: []admissionregv1.OperationType{admissionregv1.Create, admissionregv1.Update},
					Rule: admissionregv1.Rule{
						APIGroups:   []string{brokerv1a1.GroupVersion.Group},
						APIVersions: []string{brokerv1a1.GroupVersion.Version},
						Resources:   []string{"bucketclaims"},
						Scope:       &scopeAll,
					},
				}},
			},
			{
				Name:                    "s3backend.e2e.stowage.test",
				FailurePolicy:           &failurePolicy,
				SideEffects:             &sideEffects,
				AdmissionReviewVersions: []string{"v1"},
				ClientConfig: admissionregv1.WebhookClientConfig{
					Service: &admissionregv1.ServiceReference{
						Name:      "webhook",
						Namespace: "default",
						Path:      &pathBE,
					},
				},
				Rules: []admissionregv1.RuleWithOperations{{
					Operations: []admissionregv1.OperationType{admissionregv1.Create, admissionregv1.Update},
					Rule: admissionregv1.Rule{
						APIGroups:   []string{brokerv1a1.GroupVersion.Group},
						APIVersions: []string{brokerv1a1.GroupVersion.Version},
						Resources:   []string{"s3backends"},
						Scope:       &scopeAll,
					},
				}},
			},
		},
	}}
}

// webhookOpsNamespace is the namespace the S3Backend validator allows in
// AdminCredentialsSecretRef.Namespace. The webhook fixture is independent
// of the per-test operator manager, so we hard-code it here rather than
// reading from the ops-namespace helper.
const webhookOpsNamespace = "stowage-system"

// webhookExternalHost is the hostname the kind apiserver uses to dial back
// to our in-process webhook server. The bootstrap script populates
// STOWAGE_E2E_WEBHOOK_EXTERNAL_HOST with the kind container network's
// gateway IP; "host.docker.internal" is a sensible default for Docker
// Desktop and for environments where /etc/hosts already routes the name.
func webhookExternalHost() string {
	if v := os.Getenv("STOWAGE_E2E_WEBHOOK_EXTERNAL_HOST"); v != "" {
		return v
	}
	return "host.docker.internal"
}

// TestWebhook_S3Backend_RejectsBadTemplate proves the validator runs on
// the real admission path: an invalid bucketNameTemplate is rejected by
// the apiserver before the object reaches etcd.
func TestWebhook_S3Backend_RejectsBadTemplate(t *testing.T) {
	startWebhookEnv(t)
	c := MustConnect(t)
	ctx := WithTimeout(t, 30*time.Second)

	name := UniqueName("bad-tpl")
	deleteBackendOnCleanup(t, c.Client, name)

	bad := &brokerv1a1.S3Backend{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: brokerv1a1.S3BackendSpec{
			Endpoint:           "http://example.test:9000",
			BucketNameTemplate: "{{ .DoesNotExist }}",
			AdminCredentialsSecretRef: brokerv1a1.AdminCredentialsRef{
				Name: "admin", Namespace: webhookOpsNamespace,
			},
		},
	}
	err := c.Client.Create(ctx, bad)
	if err == nil {
		t.Fatalf("expected webhook rejection, got nil")
	}
	if !strings.Contains(err.Error(), "bucketNameTemplate") {
		t.Fatalf("expected error mentioning bucketNameTemplate, got: %v", err)
	}
}

// TestWebhook_S3Backend_RejectsForeignNamespace verifies the OpsNamespace
// guard fires when the admin Secret reference points at a namespace other
// than the operator's.
func TestWebhook_S3Backend_RejectsForeignNamespace(t *testing.T) {
	startWebhookEnv(t)
	c := MustConnect(t)
	ctx := WithTimeout(t, 30*time.Second)

	name := UniqueName("bad-ns")
	deleteBackendOnCleanup(t, c.Client, name)

	bad := &brokerv1a1.S3Backend{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: brokerv1a1.S3BackendSpec{
			Endpoint:           "http://example.test:9000",
			BucketNameTemplate: "{{ .Namespace }}-{{ .Name }}",
			AdminCredentialsSecretRef: brokerv1a1.AdminCredentialsRef{
				Name:      "admin",
				Namespace: "wrong-namespace",
			},
		},
	}
	err := c.Client.Create(ctx, bad)
	if err == nil || !strings.Contains(err.Error(), "operator namespace") {
		t.Fatalf("expected operator-namespace rejection, got: %v", err)
	}
}

// TestWebhook_BucketClaim_ImmutableBucketName confirms updates that change
// spec.bucketName are rejected by the validator on the wire. We also
// verify the validator runs on update (not just create) by toggling
// forceDelete on a Retain claim.
func TestWebhook_BucketClaim_ImmutableBucketName(t *testing.T) {
	startWebhookEnv(t)
	c := MustConnect(t)
	ctx := WithTimeout(t, 30*time.Second)

	tenantNS := NewNamespace(t, ctx, c.Client, "tenant-wh")

	if err := c.Client.Create(ctx, &brokerv1a1.BucketClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim", Namespace: tenantNS},
		Spec: brokerv1a1.BucketClaimSpec{
			BackendRef:     brokerv1a1.BackendRef{Name: "primary"},
			BucketName:     "stable-name",
			DeletionPolicy: brokerv1a1.DeletionPolicyRetain,
		},
	}); err != nil {
		t.Fatalf("create initial claim: %v", err)
	}

	var live brokerv1a1.BucketClaim
	if err := c.Client.Get(ctx, client.ObjectKey{Namespace: tenantNS, Name: "claim"}, &live); err != nil {
		t.Fatalf("get claim: %v", err)
	}
	live.Spec.BucketName = "changed"
	err := c.Client.Update(ctx, &live)
	if err == nil || !strings.Contains(err.Error(), "bucketName is immutable") {
		t.Fatalf("expected immutable rejection, got: %v", err)
	}

	if err := c.Client.Get(ctx, client.ObjectKey{Namespace: tenantNS, Name: "claim"}, &live); err != nil {
		t.Fatalf("get claim: %v", err)
	}
	live.Spec.ForceDelete = true
	err = c.Client.Update(ctx, &live)
	if err == nil || !strings.Contains(err.Error(), "forceDelete") {
		t.Fatalf("expected forceDelete rejection, got: %v", err)
	}
}

// TestWebhook_BucketClaim_RotationInterval defends the rotationInterval >= 7
// rule on the wire — either via the CRD CEL XValidation or the webhook;
// either is acceptable.
func TestWebhook_BucketClaim_RotationInterval(t *testing.T) {
	startWebhookEnv(t)
	c := MustConnect(t)
	ctx := WithTimeout(t, 30*time.Second)

	tenantNS := NewNamespace(t, ctx, c.Client, "tenant-rot")

	bad := &brokerv1a1.BucketClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "rot", Namespace: tenantNS},
		Spec: brokerv1a1.BucketClaimSpec{
			BackendRef: brokerv1a1.BackendRef{Name: "primary"},
			RotationPolicy: &brokerv1a1.RotationPolicy{
				Mode:         brokerv1a1.RotationModeTimeBased,
				IntervalDays: 3,
			},
		},
	}
	err := c.Client.Create(ctx, bad)
	if err == nil {
		t.Fatalf("expected rejection, got nil")
	}
	if !strings.Contains(err.Error(), "intervalDays") &&
		!strings.Contains(err.Error(), "TimeBased") &&
		!strings.Contains(err.Error(), ">= 7") {
		t.Fatalf("unexpected error message: %v", err)
	}

	var x brokerv1a1.BucketClaim
	getErr := c.Client.Get(ctx, client.ObjectKey{Namespace: tenantNS, Name: "rot"}, &x)
	if !apierrors.IsNotFound(getErr) {
		t.Fatalf("rejected object should not exist; got %v", getErr)
	}
}
