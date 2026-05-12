// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package manager assembles the controller-runtime manager that runs the
// stowage operator's reconcilers and admission webhooks. It is invoked
// in-process by the main server (when operator.enabled is true) and by the
// "stowage operator" subcommand for headless deployments.
package manager

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/go-logr/logr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	webhookserver "sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/stowage-dev/stowage/internal/backend"
	brokerv1a1 "github.com/stowage-dev/stowage/internal/operator/api/v1alpha1"
	"github.com/stowage-dev/stowage/internal/operator/controller"
	"github.com/stowage-dev/stowage/internal/operator/credentials"
	"github.com/stowage-dev/stowage/internal/operator/vcstore"
	"github.com/stowage-dev/stowage/internal/operator/webhook"
)

// Config governs the embedded operator manager. Stowage runs single-replica
// (SQLite + in-process limiter), so leader election is intentionally absent —
// there is never a second pod to contest a lease.
type Config struct {
	// Kubeconfig is an optional path to a kubeconfig file. Empty means
	// in-cluster configuration.
	Kubeconfig string

	// MetricsAddr is the bind address for controller-runtime's metrics
	// listener (e.g. ":9090"). Empty disables it; the main server's own
	// /metrics endpoint is unaffected.
	MetricsAddr string

	// OpsNamespace is the namespace the operator writes virtual-credential
	// and anonymous-binding Secrets into. Must match the proxy's
	// s3_proxy.kubernetes.namespace.
	OpsNamespace string

	// ProxyURL is the in-cluster URL the operator writes into consumer
	// Secrets so workloads know where to send S3 traffic. Used as the
	// fallback when PublicHostname is empty.
	ProxyURL string

	// PublicHostname, when set, replaces ProxyURL in the consumer-facing
	// AWS_ENDPOINT_URL so workloads outside the cluster (or behind an
	// ingress) reach the proxy via its external hostname. Bare host, no
	// scheme — proxyurl.Resolve assumes https.
	PublicHostname string

	// Registry, when non-nil, opts in to mirroring S3Backend CRs into the
	// in-process backend registry as read-only entries. Headless deployments
	// (the `stowage operator` subcommand) leave it nil — there is no admin
	// UI in that process to surface the entries into.
	Registry *backend.Registry

	Webhook WebhookConfig
}

// WebhookConfig governs the admission webhook half of the manager.
type WebhookConfig struct {
	Enabled bool
	Port    int
	CertDir string
}

// Start builds and runs the manager. Blocks until ctx is cancelled or a
// fatal setup/runtime error occurs. Returns nil on clean shutdown.
func Start(ctx context.Context, cfg Config, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	ctrl.SetLogger(logr.FromSlogHandler(logger.Handler()).WithName("operator"))

	restCfg, err := loadKubeConfig(cfg.Kubeconfig)
	if err != nil {
		return fmt.Errorf("load kubeconfig: %w", err)
	}

	sch := clientgoscheme.Scheme
	utilruntime.Must(brokerv1a1.AddToScheme(sch))

	// controller-runtime treats an empty BindAddress as "use the default
	// :8080", which collides with the main stowage HTTP listener in the
	// integrated single-binary deployment. "0" is the documented way to
	// disable the listener entirely.
	metricsAddr := cfg.MetricsAddr
	if metricsAddr == "" {
		metricsAddr = "0"
	}
	mgrOpts := ctrl.Options{
		Scheme:  sch,
		Metrics: metricsserver.Options{BindAddress: metricsAddr},
	}
	if cfg.Webhook.Enabled {
		mgrOpts.WebhookServer = webhookserver.NewServer(webhookserver.Options{
			Port:    cfg.Webhook.Port,
			CertDir: cfg.Webhook.CertDir,
		})
	}

	mgr, err := ctrl.NewManager(restCfg, mgrOpts)
	if err != nil {
		return fmt.Errorf("new manager: %w", err)
	}

	resolver := &credentials.Resolver{Client: mgr.GetClient()}
	writer := &vcstore.Writer{Client: mgr.GetClient(), Namespace: cfg.OpsNamespace}
	recorder := mgr.GetEventRecorderFor("stowage")

	if err := (&controller.S3BackendReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Resolver: resolver,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setup S3Backend controller: %w", err)
	}
	if err := (&controller.BucketClaimReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Resolver: resolver,
		Writer:   writer,
		Recorder:       recorder,
		ProxyURL:       cfg.ProxyURL,
		PublicHostname: cfg.PublicHostname,
		OpsNS:          cfg.OpsNamespace,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setup BucketClaim controller: %w", err)
	}

	if cfg.Registry != nil {
		if err := (&controller.RegistryReconciler{
			Client:   mgr.GetClient(),
			Scheme:   mgr.GetScheme(),
			Resolver: resolver,
			Registry: cfg.Registry,
		}).SetupWithManager(mgr); err != nil {
			return fmt.Errorf("setup S3Backend registry controller: %w", err)
		}
	}

	if cfg.Webhook.Enabled {
		if err := (&webhook.S3BackendValidator{OpsNamespace: cfg.OpsNamespace}).SetupWithManager(mgr); err != nil {
			return fmt.Errorf("setup S3Backend webhook: %w", err)
		}
		if err := (&webhook.BucketClaimValidator{}).SetupWithManager(mgr); err != nil {
			return fmt.Errorf("setup BucketClaim webhook: %w", err)
		}
	}

	logger.Info("operator manager starting",
		"webhook_enabled", cfg.Webhook.Enabled,
		"ops_namespace", cfg.OpsNamespace)
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("manager: %w", err)
	}
	return nil
}

func loadKubeConfig(path string) (*rest.Config, error) {
	if path != "" {
		return clientcmd.BuildConfigFromFlags("", path)
	}
	return rest.InClusterConfig()
}
