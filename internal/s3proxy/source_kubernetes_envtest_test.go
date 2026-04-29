// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build envtest

package s3proxy_test

import (
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stowage-dev/stowage/internal/operator/test/envtest"
	"github.com/stowage-dev/stowage/internal/operator/vcstore"
	"github.com/stowage-dev/stowage/internal/s3proxy"
)

// kubeconfigForCfg writes a kubeconfig file pointing at the envtest control
// plane and returns the path. NewKubernetesSource takes a path because the
// proxy production wiring doesn't have rest.Config in scope; this round-trips
// through the same loader for parity.
func kubeconfigForCfg(t *testing.T, suite *envtest.Suite) string {
	t.Helper()
	api := &clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{
			"envtest": {
				Server:                   suite.Cfg.Host,
				CertificateAuthorityData: suite.Cfg.CAData,
				InsecureSkipTLSVerify:    suite.Cfg.Insecure,
			},
		},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{
			"envtest": {
				ClientCertificateData: suite.Cfg.CertData,
				ClientKeyData:         suite.Cfg.KeyData,
				Token:                 suite.Cfg.BearerToken,
				Username:              suite.Cfg.Username,
				Password:              suite.Cfg.Password,
			},
		},
		Contexts: map[string]*clientcmdapi.Context{
			"envtest": {Cluster: "envtest", AuthInfo: "envtest"},
		},
		CurrentContext: "envtest",
	}
	dir := t.TempDir()
	path := dir + "/kubeconfig"
	if err := clientcmd.WriteToFile(*api, path); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	return path
}

// fakeLimitObserver records SetLimit / DeleteLimit calls so the test can
// assert the operator-side quota field flows through to the proxy.
type fakeLimitObserver struct {
	mu      sync.Mutex
	limits  map[string][2]int64 // backend|bucket -> {soft, hard}
	deletes map[string]int      // backend|bucket -> count
}

func newFakeLimitObserver() *fakeLimitObserver {
	return &fakeLimitObserver{
		limits:  map[string][2]int64{},
		deletes: map[string]int{},
	}
}

func (f *fakeLimitObserver) key(backend, bucket string) string { return backend + "|" + bucket }

func (f *fakeLimitObserver) SetLimit(backend, bucket string, soft, hard int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limits[f.key(backend, bucket)] = [2]int64{soft, hard}
}

func (f *fakeLimitObserver) DeleteLimit(backend, bucket string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.limits, f.key(backend, bucket))
	f.deletes[f.key(backend, bucket)]++
}

func (f *fakeLimitObserver) get(backend, bucket string) ([2]int64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.limits[f.key(backend, bucket)]
	return v, ok
}

func (f *fakeLimitObserver) deleteCount(backend, bucket string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deletes[f.key(backend, bucket)]
}

// TestKubernetesSource_WireContract proves the operator → proxy informer
// contract end-to-end against a real apiserver: write a Secret with the
// operator's vcstore.Writer, then verify the proxy's KubernetesSource picks
// it up via Lookup, exactly the way the production proxy will.
//
// This is the critical integration test for the wire contract documented in
// CLAUDE.md: "The operator and stowage share a wire contract on the K8s
// Secret data fields … Changing one without the other breaks the informer
// integration silently."
func TestKubernetesSource_WireContract(t *testing.T) {
	suite := envtest.Start(t)
	ctx := envtest.WithTimeout(t, 90*time.Second)

	const opsNS = "stowage-system"
	if err := suite.Client.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: opsNS}}); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create ns: %v", err)
	}

	observer := newFakeLimitObserver()
	src, err := s3proxy.NewKubernetesSource(s3proxy.KubernetesSourceConfig{
		Namespace:     opsNS,
		Kubeconfig:    kubeconfigForCfg(t, suite),
		ResyncPeriod:  0,
		LimitObserver: observer,
	}, nil)
	if err != nil {
		t.Fatalf("new k8s source: %v", err)
	}
	if err := src.Start(ctx); err != nil {
		t.Fatalf("start source: %v", err)
	}

	// Write a virtual credential through the operator's writer — this is the
	// only way to guarantee parity with what the operator actually puts on
	// the wire.
	w := &vcstore.Writer{Client: suite.Client, Namespace: opsNS}
	vc := vcstore.VirtualCredential{
		AccessKeyID:     "AKIAWIRE0001",
		SecretAccessKey: "wiresecret",
		BucketName:      "wired-bucket",
		BackendName:     "primary",
		ClaimNamespace:  "tenant-wire",
		ClaimName:       "claim",
		ClaimUID:        "uid-wire",
		QuotaSoftBytes:  10 << 20,
		QuotaHardBytes:  100 << 20,
	}
	if err := w.WriteInternal(ctx, vc); err != nil {
		t.Fatalf("write VC: %v", err)
	}

	// Lookup should resolve.
	envtest.Eventually(t, 15*time.Second, 100*time.Millisecond, "proxy source resolves AKID",
		func() (bool, error) {
			got, ok := src.Lookup(vc.AccessKeyID)
			if !ok {
				return false, nil
			}
			if got.SecretAccessKey != vc.SecretAccessKey {
				return false, nil
			}
			if len(got.BucketScopes) != 1 || got.BucketScopes[0].BucketName != vc.BucketName {
				return false, nil
			}
			if got.BackendName != vc.BackendName {
				return false, nil
			}
			if got.Source != "kubernetes" {
				return false, nil
			}
			return true, nil
		})

	// LimitObserver should have observed the quota.
	envtest.Eventually(t, 15*time.Second, 100*time.Millisecond, "limit observer sees quota",
		func() (bool, error) {
			v, ok := observer.get(vc.BackendName, vc.BucketName)
			return ok && v[0] == vc.QuotaSoftBytes && v[1] == vc.QuotaHardBytes, nil
		})

	// Delete the Secret — Lookup must miss and the limit observer must see
	// the corresponding DeleteLimit.
	if err := w.DeleteInternalByAccessKey(ctx, vc.AccessKeyID); err != nil {
		t.Fatalf("delete VC: %v", err)
	}
	envtest.Eventually(t, 15*time.Second, 100*time.Millisecond, "proxy source loses AKID",
		func() (bool, error) {
			_, ok := src.Lookup(vc.AccessKeyID)
			return !ok, nil
		})
	envtest.Eventually(t, 15*time.Second, 100*time.Millisecond, "limit observer saw delete",
		func() (bool, error) {
			return observer.deleteCount(vc.BackendName, vc.BucketName) > 0, nil
		})
}

// TestKubernetesSource_AnonymousBinding wires the anonymous-binding side of
// the contract: the proxy must surface bindings keyed by bucket name once
// the operator writes the Secret.
func TestKubernetesSource_AnonymousBinding(t *testing.T) {
	suite := envtest.Start(t)
	ctx := envtest.WithTimeout(t, 60*time.Second)

	const opsNS = "stowage-system"
	if err := suite.Client.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: opsNS}}); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create ns: %v", err)
	}

	src, err := s3proxy.NewKubernetesSource(s3proxy.KubernetesSourceConfig{
		Namespace:    opsNS,
		Kubeconfig:   kubeconfigForCfg(t, suite),
		ResyncPeriod: 0,
	}, nil)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}
	if err := src.Start(ctx); err != nil {
		t.Fatalf("start source: %v", err)
	}

	w := &vcstore.Writer{Client: suite.Client, Namespace: opsNS}
	binding := vcstore.AnonymousBinding{
		BucketName:     "PublicReadOnly",
		BackendName:    "primary",
		Mode:           "ReadOnly",
		PerSourceIPRPS: 10,
		ClaimNamespace: "tenant-x",
		ClaimName:      "pub",
		ClaimUID:       "uid-pub",
	}
	if err := w.WriteAnonymousBinding(ctx, binding); err != nil {
		t.Fatalf("write binding: %v", err)
	}

	envtest.Eventually(t, 15*time.Second, 100*time.Millisecond, "proxy source resolves anon",
		func() (bool, error) {
			a, ok := src.LookupAnon("publicreadonly") // case-insensitive
			if !ok {
				return false, nil
			}
			if a.Mode != "ReadOnly" || a.PerSourceIPRPS != 10 {
				return false, nil
			}
			return true, nil
		})

	if err := w.DeleteAnonymousBindingByClaim(ctx, "tenant-x", "pub"); err != nil {
		t.Fatalf("delete binding: %v", err)
	}
	envtest.Eventually(t, 15*time.Second, 100*time.Millisecond, "proxy source loses anon",
		func() (bool, error) {
			_, ok := src.LookupAnon("publicreadonly")
			return !ok, nil
		})
}

// TestKubernetesSource_BucketScopesJSON verifies the multi-bucket grant path:
// a Secret with a bucket_scopes JSON field should expose every scope via
// Lookup and publish a quota row per scope.
func TestKubernetesSource_BucketScopesJSON(t *testing.T) {
	suite := envtest.Start(t)
	ctx := envtest.WithTimeout(t, 60*time.Second)

	const opsNS = "stowage-system"
	if err := suite.Client.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: opsNS}}); err != nil && !apierrors.IsAlreadyExists(err) {
		t.Fatalf("create ns: %v", err)
	}

	observer := newFakeLimitObserver()
	src, err := s3proxy.NewKubernetesSource(s3proxy.KubernetesSourceConfig{
		Namespace:     opsNS,
		Kubeconfig:    kubeconfigForCfg(t, suite),
		ResyncPeriod:  0,
		LimitObserver: observer,
	}, nil)
	if err != nil {
		t.Fatalf("new source: %v", err)
	}
	if err := src.Start(ctx); err != nil {
		t.Fatalf("start source: %v", err)
	}

	// Build the Secret manually — this path is what the operator's
	// multi-bucket writer (not yet exposed) would emit. Until that path
	// exists in production, we exercise the proxy-side parser directly.
	const akid = "AKIAMULTI001"
	if err := suite.Client.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      vcstore.InternalSecretName(akid),
			Namespace: opsNS,
			Labels: map[string]string{
				vcstore.LabelRole:        vcstore.RoleVirtualCredential,
				vcstore.LabelClaimNS:     "tenant-multi",
				vcstore.LabelClaimName:   "multi",
				vcstore.LabelClaimUID:    "uid-m",
				vcstore.LabelAccessKeyID: akid,
				vcstore.LabelBackendName: "primary",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			vcstore.DataAccessKeyID:     []byte(akid),
			vcstore.DataSecretAccessKey: []byte("multisecret"),
			vcstore.DataBucketName:      []byte("primary-bucket"),
			vcstore.DataBackend:         []byte("primary"),
			vcstore.DataClaimUID:        []byte("uid-m"),
			vcstore.DataBucketScopes:    []byte(`[{"bucket":"primary-bucket","backend":"primary"},{"bucket":"secondary-bucket","backend":"primary"}]`),
			vcstore.DataQuotaSoftBytes:  []byte("1024"),
			vcstore.DataQuotaHardBytes:  []byte("4096"),
		},
	}); err != nil {
		t.Fatalf("create multi VC secret: %v", err)
	}

	envtest.Eventually(t, 15*time.Second, 100*time.Millisecond, "proxy source surfaces both scopes",
		func() (bool, error) {
			got, ok := src.Lookup(akid)
			if !ok {
				return false, nil
			}
			return len(got.BucketScopes) == 2, nil
		})

	// Both scopes should land in the limit observer.
	envtest.Eventually(t, 15*time.Second, 100*time.Millisecond, "limits per scope",
		func() (bool, error) {
			a, okA := observer.get("primary", "primary-bucket")
			b, okB := observer.get("primary", "secondary-bucket")
			return okA && okB && a == b && a[0] == 1024 && a[1] == 4096, nil
		})

	// Drop the Secret — both scope keys must be retracted from the observer.
	if err := suite.Client.Delete(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: opsNS, Name: vcstore.InternalSecretName(akid)},
	}); err != nil {
		t.Fatalf("delete multi VC secret: %v", err)
	}
	envtest.Eventually(t, 15*time.Second, 100*time.Millisecond, "limits retracted per scope",
		func() (bool, error) {
			return observer.deleteCount("primary", "primary-bucket") > 0 &&
				observer.deleteCount("primary", "secondary-bucket") > 0, nil
		})

	// Sanity: ensure the suite client truly removed the object.
	var s corev1.Secret
	err = suite.Client.Get(ctx, client.ObjectKey{Namespace: opsNS, Name: vcstore.InternalSecretName(akid)}, &s)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected NotFound after delete, got %v", err)
	}
}
