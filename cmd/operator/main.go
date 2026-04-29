// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"flag"
	"os"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	webhookserver "sigs.k8s.io/controller-runtime/pkg/webhook"

	brokerv1a1 "github.com/stowage-dev/stowage/internal/operator/api/v1alpha1"
	"github.com/stowage-dev/stowage/internal/operator/controller"
	"github.com/stowage-dev/stowage/internal/operator/credentials"
	"github.com/stowage-dev/stowage/internal/operator/vcstore"
	"github.com/stowage-dev/stowage/internal/operator/webhook"
)

var (
	scheme   = ctrl.GetConfigOrDie
	setupLog = ctrl.Log.WithName("setup")
	version  = "dev"
)

func init() {
	_ = scheme
}

func main() {
	var (
		metricsAddr    string
		probeAddr      string
		leaderElect    bool
		opsNamespace   string
		proxyURL       string
		webhookPort    int
		webhookCertDir string
		enableWebhooks bool
	)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":9090", "metrics listen address")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "probe listen address")
	flag.BoolVar(&leaderElect, "leader-elect", true, "enable leader election")
	flag.StringVar(&opsNamespace, "operator-namespace", "stowage-system", "namespace holding internal virtual-credential Secrets")
	flag.StringVar(&proxyURL, "proxy-endpoint", "http://stowage-proxy.stowage-system.svc.cluster.local:8080", "in-cluster proxy URL written into consumer Secrets")
	flag.BoolVar(&enableWebhooks, "enable-webhooks", true, "serve the admission webhook")
	flag.IntVar(&webhookPort, "webhook-port", 9443, "webhook server port")
	flag.StringVar(&webhookCertDir, "webhook-cert-dir", "/etc/stowage-operator/webhook/certs", "directory containing tls.crt and tls.key")

	opts := zap.Options{Development: false}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	sch := clientgoscheme.Scheme
	utilruntime.Must(brokerv1a1.AddToScheme(sch))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 sch,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         leaderElect,
		LeaderElectionID:       "stowage-operator.broker.stowage.io",
		WebhookServer: webhookserver.NewServer(webhookserver.Options{
			Port:    webhookPort,
			CertDir: webhookCertDir,
		}),
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	resolver := &credentials.Resolver{Client: mgr.GetClient()}
	writer := &vcstore.Writer{Client: mgr.GetClient(), Namespace: opsNamespace}

	var recorder record.EventRecorder = mgr.GetEventRecorderFor("stowage-operator")

	if err := (&controller.S3BackendReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Resolver: resolver,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "S3Backend")
		os.Exit(1)
	}
	if err := (&controller.BucketClaimReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Resolver: resolver,
		Writer:   writer,
		Recorder: recorder,
		ProxyURL: proxyURL,
		OpsNS:    opsNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "BucketClaim")
		os.Exit(1)
	}

	if enableWebhooks {
		if err := (&webhook.S3BackendValidator{OpsNamespace: opsNamespace}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to register webhook", "webhook", "S3Backend")
			os.Exit(1)
		}
		if err := (&webhook.BucketClaimValidator{}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to register webhook", "webhook", "BucketClaim")
			os.Exit(1)
		}
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager", "version", version)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "manager terminated with error")
		os.Exit(1)
	}
}
