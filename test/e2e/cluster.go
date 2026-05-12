// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	brokerv1a1 "github.com/stowage-dev/stowage/internal/operator/api/v1alpha1"
)

// Cluster bundles everything a test needs to talk to the e2e cluster: a
// REST config, a typed client, and the registered scheme. It is built once
// per test binary by TestMain and reused across all tests.
type Cluster struct {
	Cfg    *rest.Config
	Client client.Client
	Scheme *k8sruntime.Scheme
}

var (
	shared     *Cluster
	sharedOnce sync.Once
	sharedErr  error
	loggerOnce sync.Once
)

// Connect returns the shared cluster handle, lazily wiring it on first call.
// Connect is safe to call from any goroutine.
//
// On any failure Connect returns an error and subsequent calls return the
// same error — TestMain inspects this and fails the run with an actionable
// message.
func Connect() (*Cluster, error) {
	sharedOnce.Do(func() {
		loggerOnce.Do(func() {
			logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(os.Stderr)))
		})
		shared, sharedErr = connect()
	})
	return shared, sharedErr
}

// MustConnect is the t.Helper-friendly variant for tests that have already
// passed TestMain's reachability gate.
func MustConnect(t *testing.T) *Cluster {
	t.Helper()
	c, err := Connect()
	if err != nil {
		t.Fatalf("e2e cluster not reachable: %v", err)
	}
	return c
}

func connect() (*Cluster, error) {
	cfg, err := loadRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("load REST config: %w", err)
	}

	scheme := k8sruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apiextv1.AddToScheme(scheme))
	utilruntime.Must(brokerv1a1.AddToScheme(scheme))

	cl, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("build client: %w", err)
	}

	// Hit the API once to surface "no cluster" / "stale kubeconfig" errors
	// here instead of inside individual tests.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var ns apiextv1.CustomResourceDefinitionList
	if err := cl.List(ctx, &ns); err != nil {
		return nil, fmt.Errorf("ping apiserver: %w", err)
	}

	return &Cluster{Cfg: cfg, Client: cl, Scheme: scheme}, nil
}

// loadRESTConfig honours KUBECONFIG, then $HOME/.kube/config, then in-cluster
// — matching kubectl's default search order.
func loadRESTConfig() (*rest.Config, error) {
	loader := clientcmd.NewDefaultClientConfigLoadingRules()
	if v := os.Getenv("KUBECONFIG"); v != "" {
		loader.ExplicitPath = v
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loader, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err == nil {
		return cfg, nil
	}
	// Fall back to in-cluster — useful when tests run inside a Pod (e.g. a
	// dedicated e2e Job in CI).
	if inCluster, icErr := rest.InClusterConfig(); icErr == nil {
		return inCluster, nil
	}
	return nil, err
}

// EnsureCRDs verifies the broker.stowage.io CRDs are present on the cluster
// and, if not, applies them from deploy/chart/crds/. The bootstrap script
// normally does this, but running it from TestMain too lets a dev iterate
// on a long-lived kind cluster without re-running bootstrap after edits.
func (c *Cluster) EnsureCRDs(ctx context.Context) error {
	for _, name := range []string{"s3backends.broker.stowage.io", "bucketclaims.broker.stowage.io"} {
		var crd apiextv1.CustomResourceDefinition
		err := c.Client.Get(ctx, types.NamespacedName{Name: name}, &crd)
		if err == nil {
			continue
		}
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get CRD %s: %w", name, err)
		}
		return fmt.Errorf("CRD %s missing — run `make e2e-bootstrap` or apply deploy/chart/crds/", name)
	}
	return nil
}

// LocateCRDs walks up from this file to the repo root and returns the
// absolute path to the chart's CRD directory.
func LocateCRDs() (string, error) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	dir := filepath.Dir(here)
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, "deploy", "chart", "crds")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("could not find deploy/chart/crds walking up from %s", here)
}

// opsNamespaceName is the namespace the in-process operator writes its
// internal Secrets into during tests. Override with STOWAGE_E2E_OPS_NS for
// shared-cluster scenarios; default keeps tests away from the chart's
// stowage-system namespace.
func opsNamespaceName() string {
	if v := os.Getenv("STOWAGE_E2E_OPS_NS"); v != "" {
		return v
	}
	return "stowage-e2e-ops"
}
