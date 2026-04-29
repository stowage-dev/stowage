// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build envtest

// Package envtest provides a shared harness for envtest-based integration
// tests. The build tag keeps this code (and its tests' transitive imports of
// kube-apiserver/etcd binaries) out of the default `go test ./...` matrix.
//
// All envtest suites in stowage funnel through Start to keep CRD discovery,
// scheme registration, manager wiring, and binary-asset bootstrap consistent.
package envtest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crenvtest "sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	brokerv1a1 "github.com/stowage-dev/stowage/internal/operator/api/v1alpha1"
)

// Suite bundles everything an envtest-based test needs: the running control
// plane, a typed scheme, a direct (uncached) client, and the REST config so
// callers can wire their own informers / managers on top.
type Suite struct {
	Env    *crenvtest.Environment
	Cfg    *rest.Config
	Client client.Client
	Scheme *k8sruntime.Scheme

	t       *testing.T
	stopped bool
}

// loggerOnce ensures we install the controller-runtime logger only once even
// if multiple suites Start in the same `go test` invocation.
var loggerOnce sync.Once

// Start brings up an envtest control plane preloaded with the operator's CRDs
// and brokerv1a1 registered against client-go's scheme. The control plane is
// torn down on test cleanup.
//
// If the kubebuilder asset binaries (etcd, kube-apiserver) cannot be located,
// Start calls t.Skip with an actionable message — never t.Fatal — so a
// developer running `go test ./...` without envtest installed gets a clear
// "skip" rather than a hard failure.
func Start(t *testing.T) *Suite {
	t.Helper()

	loggerOnce.Do(func() {
		logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(os.Stderr)))
	})

	if err := ensureAssets(t); err != nil {
		t.Skipf("envtest skipped: %v", err)
	}

	scheme := k8sruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(brokerv1a1.AddToScheme(scheme))

	crdDir, err := LocateCRDs()
	if err != nil {
		t.Fatalf("locate CRDs: %v", err)
	}

	env := &crenvtest.Environment{
		CRDDirectoryPaths:     []string{crdDir},
		ErrorIfCRDPathMissing: true,
		Scheme:                scheme,
	}

	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("envtest Start: %v", err)
	}

	cl, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		_ = env.Stop()
		t.Fatalf("build client: %v", err)
	}

	s := &Suite{
		Env:    env,
		Cfg:    cfg,
		Client: cl,
		Scheme: scheme,
		t:      t,
	}
	t.Cleanup(s.Stop)
	return s
}

// Stop tears down the envtest control plane. Idempotent — safe to call from
// both deferred cleanups and inline error branches.
func (s *Suite) Stop() {
	if s == nil || s.stopped {
		return
	}
	s.stopped = true
	stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = stopCtx
	if err := s.Env.Stop(); err != nil {
		s.t.Logf("envtest Stop: %v", err)
	}
}

// LocateCRDs walks up from this file to the repo root and returns the absolute
// path to the chart's CRD directory. The chart copies are the canonical,
// hand-curated CRDs.
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

// ensureAssets verifies KUBEBUILDER_ASSETS is set or, failing that, asks
// `setup-envtest` for a default version. Returns a non-nil error to signal
// "skip" — Start translates it into t.Skip.
func ensureAssets(t *testing.T) error {
	t.Helper()
	if v := os.Getenv("KUBEBUILDER_ASSETS"); v != "" {
		if _, err := os.Stat(filepath.Join(v, "kube-apiserver")); err == nil {
			return nil
		}
	}

	bin, err := exec.LookPath("setup-envtest")
	if err != nil {
		gopath := os.Getenv("GOPATH")
		if gopath == "" {
			home, _ := os.UserHomeDir()
			gopath = filepath.Join(home, "go")
		}
		candidate := filepath.Join(gopath, "bin", "setup-envtest")
		if _, statErr := os.Stat(candidate); statErr == nil {
			bin = candidate
		} else {
			return fmt.Errorf("KUBEBUILDER_ASSETS unset and setup-envtest not found on PATH; install with `go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest` then run `setup-envtest use 1.32.0`")
		}
	}

	version := os.Getenv("ENVTEST_K8S_VERSION")
	if version == "" {
		version = "1.32.0"
	}

	out, err := exec.Command(bin, "use", version, "-p", "path").CombinedOutput()
	if err != nil {
		return fmt.Errorf("setup-envtest use %s failed: %v: %s", version, err, strings.TrimSpace(string(out)))
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return fmt.Errorf("setup-envtest returned empty path")
	}
	if _, err := os.Stat(filepath.Join(path, "kube-apiserver")); err != nil {
		return fmt.Errorf("setup-envtest path %q has no kube-apiserver: %w", path, err)
	}
	if err := os.Setenv("KUBEBUILDER_ASSETS", path); err != nil {
		return fmt.Errorf("set KUBEBUILDER_ASSETS: %w", err)
	}
	return nil
}
