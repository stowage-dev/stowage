// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build e2e

package e2e

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	webhookserver "sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/stowage-dev/stowage/internal/operator/controller"
	"github.com/stowage-dev/stowage/internal/operator/credentials"
	"github.com/stowage-dev/stowage/internal/operator/vcstore"
)

// errCacheSync is returned by RunPersistent when the manager's cache
// fails its initial sync; the caller is expected to surface it as a
// fixture-init failure.
var errCacheSync = errors.New("manager cache failed initial sync")

// ManagerOptions configures the in-process operator manager started by a
// test. Zero-valued fields fall back to sensible defaults; the only
// genuinely required value is OpsNamespace.
type ManagerOptions struct {
	// OpsNamespace is the operator namespace where admin Secrets and
	// internal virtual-credential Secrets live. Must be created by the
	// caller before Start.
	OpsNamespace string

	// WatchNamespaces narrows the manager's cache to a finite set of
	// namespaces. Empty means cluster-wide. Tests share a single kind
	// cluster, so scoping the cache keeps reconciles from observing
	// resources from unrelated tests.
	WatchNamespaces []string

	// ProxyURL is the URL stamped into status.endpoint of bound claims.
	// Tests that don't inspect this can leave it empty.
	ProxyURL string

	// WithWebhookServer, when non-nil, wires the supplied webhook server
	// into the manager (used by the webhook suite). Otherwise the manager
	// runs without a webhook listener.
	WithWebhookServer webhookserver.Server
}

// ManagerHandle is the running manager plus the helpers tests need to
// drive it. Stop is registered as a t.Cleanup, but is exposed so tests can
// stop the manager early if they need to assert finalizer-driven cleanup
// before the test ends.
type ManagerHandle struct {
	Manager  ctrl.Manager
	Client   client.Client
	Recorder *record.FakeRecorder

	t        *testing.T
	cancel   context.CancelFunc
	exitErr  chan error
	stopped  bool
}

// StartOperatorManager wires both reconcilers into a manager backed by the
// e2e cluster's apiserver and starts it. The manager is stopped on test
// cleanup.
//
// The returned handle exposes the manager's client (cache-backed) so tests
// that need read-after-write consistency can fall back to the direct
// Cluster.Client.
func StartOperatorManager(t *testing.T, c *Cluster, opts ManagerOptions) *ManagerHandle {
	t.Helper()

	if opts.OpsNamespace == "" {
		t.Fatalf("StartOperatorManager: OpsNamespace required")
	}

	var cacheOpts cache.Options
	if len(opts.WatchNamespaces) > 0 {
		// Always watch the ops namespace too — the reconcilers Get admin
		// Secrets out of it.
		nsSet := map[string]cache.Config{}
		for _, ns := range append([]string{opts.OpsNamespace}, opts.WatchNamespaces...) {
			nsSet[ns] = cache.Config{}
		}
		cacheOpts.DefaultNamespaces = nsSet
	}

	mgr, err := ctrl.NewManager(c.Cfg, ctrl.Options{
		Scheme:  c.Scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
		// Each test starts a fresh manager but controller-runtime's metrics
		// registry is process-global; without SkipNameValidation the second
		// test's reconciler registration collides on prometheus metric
		// names.
		Controller:    config.Controller{SkipNameValidation: ptr.To(true)},
		Cache:         cacheOpts,
		WebhookServer: opts.WithWebhookServer,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	resolver := &credentials.Resolver{Client: mgr.GetClient()}
	writer := &vcstore.Writer{Client: mgr.GetClient(), Namespace: opts.OpsNamespace}
	recorder := record.NewFakeRecorder(64)

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
		Recorder: recorder,
		ProxyURL: opts.ProxyURL,
		OpsNS:    opts.OpsNamespace,
	}).SetupWithManager(mgr); err != nil {
		t.Fatalf("setup BucketClaim reconciler: %v", err)
	}

	return startAndAttach(t, mgr, recorder)
}

// AddRunnable adds a manager.Runnable to a manager that hasn't been started
// yet. Useful for vcstore Reader/AnonReader's Start methods, which want to
// piggy-back on the manager's lifecycle.
func AddRunnable(t *testing.T, mgr ctrl.Manager, name string, fn func(ctx context.Context) error) {
	t.Helper()
	if err := mgr.Add(runnable(fn)); err != nil {
		t.Fatalf("add runnable %s: %v", name, err)
	}
}

// NewBareManager builds a manager without any reconcilers. Callers wire
// their own Runnables before calling Run. Used by the vcstore and
// kubernetes-source suites.
func NewBareManager(t *testing.T, c *Cluster) ctrl.Manager {
	t.Helper()
	mgr, err := ctrl.NewManager(c.Cfg, ctrl.Options{
		Scheme:     c.Scheme,
		Metrics:    metricsserver.Options{BindAddress: "0"},
		Controller: config.Controller{SkipNameValidation: ptr.To(true)},
	})
	if err != nil {
		t.Fatalf("new bare manager: %v", err)
	}
	return mgr
}

// Run starts a manager that was built via NewBareManager. It registers a
// t.Cleanup that cancels and waits for exit. Use this when the caller has
// added all desired Runnables and wants the manager up.
func Run(t *testing.T, mgr ctrl.Manager) *ManagerHandle {
	t.Helper()
	return startAndAttach(t, mgr, nil)
}

// RunPersistent starts a manager whose lifetime is the test binary, not
// any individual test. The returned handle does NOT register a
// t.Cleanup — the manager runs until process exit. Use this for
// expensive shared fixtures like the admission-webhook server, where
// reinstalling ValidatingWebhookConfigurations between tests would race
// the apiserver's CA-bundle cache.
//
// The persistent manager's cache is synced before this returns, matching
// Run's contract.
func RunPersistent(mgr ctrl.Manager) (*ManagerHandle, error) {
	ctx, cancel := context.WithCancel(context.Background())
	exit := make(chan error, 1)
	go func() { exit <- mgr.Start(ctx) }()

	h := &ManagerHandle{
		Manager: mgr,
		Client:  mgr.GetClient(),
		cancel:  cancel,
		exitErr: exit,
	}
	syncCtx, syncCancel := context.WithTimeout(ctx, 60*time.Second)
	defer syncCancel()
	if !mgr.GetCache().WaitForCacheSync(syncCtx) {
		cancel()
		return nil, errCacheSync
	}
	return h, nil
}

func startAndAttach(t *testing.T, mgr ctrl.Manager, recorder *record.FakeRecorder) *ManagerHandle {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	exit := make(chan error, 1)
	go func() { exit <- mgr.Start(ctx) }()

	h := &ManagerHandle{
		Manager:  mgr,
		Client:   mgr.GetClient(),
		Recorder: recorder,
		t:        t,
		cancel:   cancel,
		exitErr:  exit,
	}
	t.Cleanup(h.Stop)

	// Block until the cache is synced — otherwise the first Get/List in a
	// test races the informer warmup and reads stale data.
	if !mgr.GetCache().WaitForCacheSync(ctx) {
		t.Fatalf("manager cache failed initial sync")
	}
	return h
}

// Stop cancels the manager context and waits up to 15s for the manager
// goroutine to exit. Idempotent.
func (h *ManagerHandle) Stop() {
	if h == nil || h.stopped {
		return
	}
	h.stopped = true
	h.cancel()
	select {
	case err := <-h.exitErr:
		if err != nil && !strings.Contains(err.Error(), "context canceled") {
			h.t.Logf("manager exited: %v", err)
		}
	case <-time.After(15 * time.Second):
		h.t.Logf("manager did not exit within 15s after cancel")
	}
}

// runnable adapts a Start(ctx) function into a manager.Runnable.
type runnable func(ctx context.Context) error

func (r runnable) Start(ctx context.Context) error { return r(ctx) }

// compile-time assertion that runnable satisfies manager.Runnable.
var _ manager.Runnable = runnable(nil)
