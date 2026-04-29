// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build envtest

package webhook_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	admissionregv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	crenvtest "sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	webhookserver "sigs.k8s.io/controller-runtime/pkg/webhook"

	brokerv1a1 "github.com/stowage-dev/stowage/internal/operator/api/v1alpha1"
	envtestharness "github.com/stowage-dev/stowage/internal/operator/test/envtest"
	"github.com/stowage-dev/stowage/internal/operator/webhook"
)

// webhookFixture starts an envtest with the validating webhook server wired up
// inside a controller-runtime manager. Returns a client + the fixture handle
// for cleanup.
//
// We don't go through envtestharness.Start here because WebhookInstallOptions
// has to be configured before the apiserver starts — the apiserver picks up
// the generated CA bundle and writes the webhook config alongside CRDs.
type webhookFixture struct {
	env *crenvtest.Environment
	mgr ctrl.Manager
	cli client.Client
}

func startWebhookEnv(t *testing.T) *webhookFixture {
	t.Helper()

	logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(os.Stderr)))

	// Reuse the harness's setup-envtest discovery so devs only have to set
	// KUBEBUILDER_ASSETS once.
	if v := os.Getenv("KUBEBUILDER_ASSETS"); v == "" {
		// Trigger ensureAssets via a throwaway harness Start. Caller is
		// expected to have run setup-envtest already; if not, the helper
		// skips the test with an actionable message.
		throwaway := envtestharness.Start(t)
		throwaway.Stop()
	}

	scheme := k8sruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(brokerv1a1.AddToScheme(scheme))

	crdDir, err := envtestharness.LocateCRDs()
	if err != nil {
		t.Fatalf("locate CRDs: %v", err)
	}

	env := &crenvtest.Environment{
		Scheme:                scheme,
		CRDDirectoryPaths:     []string{crdDir},
		ErrorIfCRDPathMissing: true,
		WebhookInstallOptions: crenvtest.WebhookInstallOptions{
			ValidatingWebhooks: validatingWebhookConfigs(),
		},
	}

	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("envtest Start: %v", err)
	}
	t.Cleanup(func() { _ = env.Stop() })

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:     scheme,
		Metrics:    metricsserver.Options{BindAddress: "0"},
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
		WebhookServer: webhookserver.NewServer(webhookserver.Options{
			Host:    env.WebhookInstallOptions.LocalServingHost,
			Port:    env.WebhookInstallOptions.LocalServingPort,
			CertDir: env.WebhookInstallOptions.LocalServingCertDir,
		}),
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if err := (&webhook.S3BackendValidator{OpsNamespace: "stowage-system"}).SetupWithManager(mgr); err != nil {
		t.Fatalf("S3Backend validator setup: %v", err)
	}
	if err := (&webhook.BucketClaimValidator{}).SetupWithManager(mgr); err != nil {
		t.Fatalf("BucketClaim validator setup: %v", err)
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

	// Block until the webhook server is bound. The webhook framework opens
	// the listener inside Start; envtestharness.Eventually polls the
	// validator on a known-bad payload — first call may transiently fail
	// while the server warms up.
	cl, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	envtestharness.Eventually(t, 30*time.Second, 200*time.Millisecond, "webhook ready",
		func() (bool, error) {
			err := cl.Create(context.Background(), &brokerv1a1.S3Backend{
				ObjectMeta: metav1.ObjectMeta{Name: "warmup-" + time.Now().Format("150405.000")},
				Spec: brokerv1a1.S3BackendSpec{
					Endpoint:           "http://example.test",
					BucketNameTemplate: "{{ .DoesNotExist }}",
					AdminCredentialsSecretRef: brokerv1a1.AdminCredentialsRef{
						Name: "admin", Namespace: "stowage-system",
					},
				},
			})
			// Once the webhook is reachable we expect the request to be
			// rejected (bad template) — that's the success signal.
			return err != nil && strings.Contains(err.Error(), "bucketNameTemplate"), nil
		})

	return &webhookFixture{env: env, mgr: mgr, cli: cl}
}

// validatingWebhookConfigs builds the ValidatingWebhookConfiguration list
// envtest installs. envtest substitutes the generated CABundle and patches
// the URL/Service host:port at install time, so leaving CABundle nil and the
// URL pointing at a placeholder is intentional and matches the kubebuilder
// idiom.
func validatingWebhookConfigs() []*admissionregv1.ValidatingWebhookConfiguration {
	failurePolicy := admissionregv1.Fail
	sideEffects := admissionregv1.SideEffectClassNone
	scopeAll := admissionregv1.AllScopes

	pathBC := "/validate-broker-stowage-io-v1alpha1-bucketclaim"
	pathBE := "/validate-broker-stowage-io-v1alpha1-s3backend"

	return []*admissionregv1.ValidatingWebhookConfiguration{{
		ObjectMeta: metav1.ObjectMeta{Name: "stowage-operator-validator"},
		Webhooks: []admissionregv1.ValidatingWebhook{
			{
				Name:                    "bucketclaim.stowage.test",
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
				Name:                    "s3backend.stowage.test",
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

// TestWebhook_S3Backend_RejectsBadTemplate proves the validator runs on the
// real admission path: an invalid bucketNameTemplate is rejected by the
// apiserver before the object reaches etcd.
func TestWebhook_S3Backend_RejectsBadTemplate(t *testing.T) {
	f := startWebhookEnv(t)
	ctx := envtestharness.WithTimeout(t, 30*time.Second)

	bad := &brokerv1a1.S3Backend{
		ObjectMeta: metav1.ObjectMeta{Name: "bad"},
		Spec: brokerv1a1.S3BackendSpec{
			Endpoint:           "http://example.test:9000",
			BucketNameTemplate: "{{ .DoesNotExist }}",
			AdminCredentialsSecretRef: brokerv1a1.AdminCredentialsRef{
				Name: "admin", Namespace: "stowage-system",
			},
		},
	}
	err := f.cli.Create(ctx, bad)
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
	f := startWebhookEnv(t)
	ctx := envtestharness.WithTimeout(t, 30*time.Second)

	bad := &brokerv1a1.S3Backend{
		ObjectMeta: metav1.ObjectMeta{Name: "bad-ns"},
		Spec: brokerv1a1.S3BackendSpec{
			Endpoint:           "http://example.test:9000",
			BucketNameTemplate: "{{ .Namespace }}-{{ .Name }}",
			AdminCredentialsSecretRef: brokerv1a1.AdminCredentialsRef{
				Name:      "admin",
				Namespace: "wrong-namespace",
			},
		},
	}
	err := f.cli.Create(ctx, bad)
	if err == nil || !strings.Contains(err.Error(), "operator namespace") {
		t.Fatalf("expected operator-namespace rejection, got: %v", err)
	}
}

// TestWebhook_BucketClaim_ImmutableBucketName confirms updates that change
// spec.bucketName are rejected by the validator on the wire. We also verify
// the validator runs on update (not just create) by toggling forceDelete.
func TestWebhook_BucketClaim_ImmutableBucketName(t *testing.T) {
	f := startWebhookEnv(t)
	ctx := envtestharness.WithTimeout(t, 30*time.Second)

	if err := f.cli.Create(ctx, &brokerv1a1.BucketClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "claim", Namespace: "default"},
		Spec: brokerv1a1.BucketClaimSpec{
			BackendRef:     brokerv1a1.BackendRef{Name: "primary"},
			BucketName:     "stable-name",
			DeletionPolicy: brokerv1a1.DeletionPolicyRetain,
		},
	}); err != nil {
		t.Fatalf("create initial claim: %v", err)
	}

	var live brokerv1a1.BucketClaim
	if err := f.cli.Get(ctx, client.ObjectKey{Namespace: "default", Name: "claim"}, &live); err != nil {
		t.Fatalf("get claim: %v", err)
	}
	live.Spec.BucketName = "changed"
	err := f.cli.Update(ctx, &live)
	if err == nil || !strings.Contains(err.Error(), "bucketName is immutable") {
		t.Fatalf("expected immutable rejection, got: %v", err)
	}

	// Validator runs on update — flipping ForceDelete on with policy=Retain
	// must be rejected.
	if err := f.cli.Get(ctx, client.ObjectKey{Namespace: "default", Name: "claim"}, &live); err != nil {
		t.Fatalf("get claim: %v", err)
	}
	live.Spec.ForceDelete = true
	err = f.cli.Update(ctx, &live)
	if err == nil || !strings.Contains(err.Error(), "forceDelete") {
		t.Fatalf("expected forceDelete rejection, got: %v", err)
	}
}

// TestWebhook_BucketClaim_RotationInterval defends the rotationInterval >= 7
// rule on the wire — either via the CRD CEL XValidation or the webhook;
// either is acceptable.
func TestWebhook_BucketClaim_RotationInterval(t *testing.T) {
	f := startWebhookEnv(t)
	ctx := envtestharness.WithTimeout(t, 30*time.Second)

	bad := &brokerv1a1.BucketClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "rot", Namespace: "default"},
		Spec: brokerv1a1.BucketClaimSpec{
			BackendRef: brokerv1a1.BackendRef{Name: "primary"},
			RotationPolicy: &brokerv1a1.RotationPolicy{
				Mode:         brokerv1a1.RotationModeTimeBased,
				IntervalDays: 3,
			},
		},
	}
	err := f.cli.Create(ctx, bad)
	if err == nil {
		t.Fatalf("expected rejection, got nil")
	}
	if !strings.Contains(err.Error(), "intervalDays") &&
		!strings.Contains(err.Error(), "TimeBased") &&
		!strings.Contains(err.Error(), ">= 7") {
		t.Fatalf("unexpected error message: %v", err)
	}

	// Sanity: rejected object should not exist.
	var x brokerv1a1.BucketClaim
	getErr := f.cli.Get(ctx, client.ObjectKey{Namespace: "default", Name: "rot"}, &x)
	if !apierrors.IsNotFound(getErr) {
		t.Fatalf("rejected object should not exist; got %v", getErr)
	}
}
