// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof/* on http.DefaultServeMux when STOWAGE_PPROF_LISTEN is set
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"

	"github.com/stowage-dev/stowage/internal/api"
	"github.com/stowage-dev/stowage/internal/audit"
	"github.com/stowage-dev/stowage/internal/auth"
	authoidc "github.com/stowage-dev/stowage/internal/auth/oidc"
	"github.com/stowage-dev/stowage/internal/backend"
	"github.com/stowage-dev/stowage/internal/config"
	"github.com/stowage-dev/stowage/internal/metrics"
	opmgr "github.com/stowage-dev/stowage/internal/operator/manager"
	"github.com/stowage-dev/stowage/internal/quotas"
	"github.com/stowage-dev/stowage/internal/s3proxy"
	"github.com/stowage-dev/stowage/internal/secrets"
	"github.com/stowage-dev/stowage/internal/shares"
	"github.com/stowage-dev/stowage/internal/sizes"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
	"github.com/stowage-dev/stowage/web"
)

type Server struct {
	cfg      config.Config
	logger   *slog.Logger
	store    *sqlite.Store
	registry *backend.Registry
	quotas   *quotas.Service
	sizes    *sizes.Service
	auth     *auth.Service
	audit    *audit.AsyncRecorder
	http     *http.Server

	// s3http is the second listener: the embedded SigV4 proxy. nil when
	// cfg.S3Proxy.Enabled is false.
	s3http     *http.Server
	s3sqlite   *s3proxy.SQLiteSource
	s3kube     *s3proxy.KubernetesSource
	s3reloadIv time.Duration
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Server, error) {
	store, err := sqlite.Open(ctx, cfg.DB.SQLite.Path)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}

	staticUser, err := buildStaticUser(cfg.Auth)
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	proxies, err := auth.NewProxyTrust(cfg.Server.TrustedProxies)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("server.trusted_proxies: %w", err)
	}

	svc := auth.NewService(cfg.Auth, store, staticUser)
	svc.Sessions.Proxies = proxies

	var oidcProv *authoidc.Provider
	if containsMode(cfg.Auth.Modes, "oidc") {
		oidcProv, err = initOIDC(ctx, cfg, proxies)
		if err != nil {
			logger.Warn("oidc disabled: init failed", "err", err.Error())
		}
	}

	registry, err := buildRegistry(ctx, cfg.Backends)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("build backends: %w", err)
	}

	frontend, err := fs.Sub(web.Assets, "dist")
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	var sessionRL *auth.RateLimiter
	if cfg.RateLimit.APIPerMinute > 0 {
		sessionRL = auth.NewRateLimiter(cfg.RateLimit.APIPerMinute, time.Minute)
	}
	syncAudit := audit.NewSQLiteRecorder(store, logger, proxies)
	auditRec := audit.NewAsyncRecorder(syncAudit, logger, 4096)
	authDeps := &api.AuthDeps{
		Service:        svc,
		OIDC:           oidcProv,
		RateLim:        auth.NewRateLimiter(10, 15*time.Minute),
		SessionRateLim: sessionRL,
		Logger:         logger,
		Audit:          auditRec,
	}
	// Limits flow through a merged source so K8s-managed quotas
	// (BucketClaim.spec.quota) shadow dashboard-managed ones. The K8s
	// limit source is populated by the s3proxy.KubernetesSource via a
	// LimitObserver — see buildS3ProxyAssets below.
	sqliteLimits := quotas.NewSQLiteLimitSource(store, logger)
	if err := sqliteLimits.Reload(ctx); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("prime quota limits: %w", err)
	}
	var (
		k8sLimits    *quotas.KubernetesLimitSource
		mergedLimits *quotas.MergedLimitSource
	)
	if cfg.S3Proxy.Enabled && cfg.S3Proxy.Kubernetes.Enabled {
		k8sLimits = quotas.NewKubernetesLimitSource(logger)
		mergedLimits = quotas.NewMergedLimitSource(logger, k8sLimits, sqliteLimits)
	} else {
		mergedLimits = quotas.NewMergedLimitSource(logger, sqliteLimits)
	}
	quotaSvc := quotas.New(mergedLimits, store, registry, logger)
	sizeSvc := sizes.New(store, registry, logger)

	// The root sealer key is optional at boot — without it, the legacy
	// YAML-driven flows still work, only the admin endpoint-management
	// API is disabled (handlers return 503 secret_key_unset).
	//
	// Resolution order: STOWAGE_SECRET_KEY env var first, then a key file
	// (server.secret_key_file / STOWAGE_SECRET_KEY_FILE). If the file path
	// is set but the file is absent, we generate a fresh key and write it
	// at 0600 — operator convenience at the cost of key-and-ciphertext
	// living on the same disk.
	sealer, err := secrets.LoadFromEnv("STOWAGE_SECRET_KEY")
	if err != nil && !errors.Is(err, secrets.ErrNoKey) {
		_ = store.Close()
		return nil, fmt.Errorf("STOWAGE_SECRET_KEY: %w", err)
	}
	if sealer == nil && cfg.Server.SecretKeyFile != "" {
		var generated bool
		sealer, generated, err = secrets.LoadOrGenerateFile(cfg.Server.SecretKeyFile)
		if err != nil {
			_ = store.Close()
			return nil, fmt.Errorf("secret key file: %w", err)
		}
		if generated {
			logger.Warn("generated new secret key file; back this up — losing it makes UI-managed endpoint secrets unrecoverable",
				"path", cfg.Server.SecretKeyFile)
		}
	}
	if sealer == nil {
		logger.Warn("no secret key configured; UI endpoint management is disabled (set STOWAGE_SECRET_KEY or server.secret_key_file)")
	}

	// Layer DB-managed endpoints on top of YAML. YAML wins on id collisions
	// — see hydrateFromStore for the logic. Failing here is fail-fast: a
	// boot that silently dropped UI-added endpoints would be worse than a
	// loud refusal.
	if err := hydrateFromStore(ctx, registry, store, sealer, logger); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("hydrate db backends: %w", err)
	}

	backendDeps := &api.BackendDeps{
		Registry: registry,
		Logger:   logger,
		Quotas:   quotaSvc,
		Sizes:    sizeSvc,
		Audit:    auditRec,
		Store:    store,
		Sealer:   sealer,
	}
	shareSvc := &shares.Service{
		Store:    store,
		Backends: registry,
		Logger:   logger,
	}
	shareDeps := &api.ShareDeps{
		Service: shareSvc,
		// Spec §7.6: rate-limit unauthenticated endpoints. 10/min per IP
		// matches the login cap — the public resolver is the only other
		// unauthenticated surface.
		RateLim: auth.NewRateLimiter(10, time.Minute),
		Logger:  logger,
		Unlock:  api.MustNewUnlockSigner(),
		Audit:   auditRec,
		Proxies: proxies,
	}

	mtx := metrics.New()
	mtx.Prom = metrics.NewProm(cfg.DB.SQLite.Path)

	// Build the S3 proxy sources up front (when enabled) so the router's
	// admin handlers can share them with the embedded proxy. The rest of
	// the proxy wiring (listener, resolver) happens in buildS3Proxy below.
	var (
		s3sqliteSource  *s3proxy.SQLiteSource
		s3kubeSource    *s3proxy.KubernetesSource
		s3CredDeps      *api.S3CredentialDeps
		s3AnonDeps      *api.S3AnonymousDeps
		s3ProxyViewDeps *api.S3ProxyViewDeps
	)
	if cfg.S3Proxy.Enabled {
		if sealer == nil {
			_ = store.Close()
			return nil, fmt.Errorf("s3 proxy enabled but no secret key configured (set STOWAGE_SECRET_KEY or server.secret_key_file)")
		}
		s3sqliteSource = s3proxy.NewSQLiteSource(store, sealer, logger)
		if err := s3sqliteSource.Reload(ctx); err != nil {
			_ = store.Close()
			return nil, fmt.Errorf("prime sqlite credential source: %w", err)
		}
		if cfg.S3Proxy.Kubernetes.Enabled {
			ns := cfg.S3Proxy.Kubernetes.Namespace
			if ns == "" {
				ns = "stowage-system"
			}
			var observer s3proxy.LimitObserver
			if k8sLimits != nil {
				observer = &k8sQuotaObserver{src: k8sLimits}
			}
			ks, kerr := s3proxy.NewKubernetesSource(s3proxy.KubernetesSourceConfig{
				Namespace:     ns,
				Kubeconfig:    cfg.S3Proxy.Kubernetes.Kubeconfig,
				ResyncPeriod:  5 * time.Minute,
				LimitObserver: observer,
			}, logger)
			if kerr != nil {
				_ = store.Close()
				return nil, fmt.Errorf("kubernetes source: %w", kerr)
			}
			s3kubeSource = ks
		}
		s3CredDeps = &api.S3CredentialDeps{
			Store:    store,
			Sealer:   sealer,
			Reloader: s3sqliteSource,
			Audit:    auditRec,
			Logger:   logger,
		}
		s3AnonDeps = &api.S3AnonymousDeps{
			Store:    store,
			Reloader: s3sqliteSource,
			Audit:    auditRec,
			Logger:   logger,
		}
		s3ProxyViewDeps = &api.S3ProxyViewDeps{
			Store:  store,
			Logger: logger,
		}
		if s3kubeSource != nil {
			s3ProxyViewDeps.OperatorSource = s3kubeSource
		}
	}

	handler := api.NewRouter(api.Deps{
		Logger:      logger,
		FrontendFS:  frontend,
		Auth:        authDeps,
		Backends:    backendDeps,
		Shares:      shareDeps,
		Metrics:     mtx,
		Audit:       &api.AuditDeps{Store: store},
		S3Cred:      s3CredDeps,
		S3Anon:      s3AnonDeps,
		S3ProxyView: s3ProxyViewDeps,
		Proxies:     proxies,
	})

	srv := &Server{
		cfg:      cfg,
		logger:   logger,
		store:    store,
		registry: registry,
		quotas:   quotaSvc,
		sizes:    sizeSvc,
		auth:     svc,
		audit:    auditRec,
		http: &http.Server{
			Addr:              cfg.Server.Listen,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       60 * time.Second,
		},
		s3sqlite:   s3sqliteSource,
		s3kube:     s3kubeSource,
		s3reloadIv: 30 * time.Second,
	}

	if cfg.S3Proxy.Enabled {
		if err := srv.buildS3Proxy(registry, mtx, auditRec, quotaSvc, proxies); err != nil {
			_ = store.Close()
			return nil, fmt.Errorf("build s3 proxy: %w", err)
		}
	}

	return srv, nil
}

// k8sQuotaObserver bridges the s3proxy.LimitObserver interface to a
// *quotas.KubernetesLimitSource so the K8s informer that powers the
// credential cache also feeds the merged limit cache.
type k8sQuotaObserver struct{ src *quotas.KubernetesLimitSource }

func (o *k8sQuotaObserver) SetLimit(backendID, bucket string, soft, hard int64) {
	o.src.Set(backendID, bucket, quotas.Limit{
		SoftBytes: soft,
		HardBytes: hard,
		// Source is overwritten to "kubernetes" by KubernetesLimitSource.Set.
	})
}

func (o *k8sQuotaObserver) DeleteLimit(backendID, bucket string) {
	o.src.Delete(backendID, bucket)
}

// buildS3Proxy wires the embedded SigV4 proxy on top of the pre-built
// credential sources (s.s3sqlite, optionally s.s3kube), and starts the
// secondary http.Server listening on cfg.S3Proxy.Listen. Cache prime for
// the Kubernetes informer happens in Run() so we don't block New() on
// network I/O.
func (s *Server) buildS3Proxy(
	registry *backend.Registry,
	mtx *metrics.Service,
	audit *audit.AsyncRecorder,
	quotaSvc *quotas.Service,
	proxies *auth.ProxyTrust,
) error {
	cfg := s.cfg.S3Proxy

	var kubeSource s3proxy.Source
	if s.s3kube != nil {
		kubeSource = s.s3kube
	}

	merged := s3proxy.NewMergedSource(s.logger, kubeSource, s.s3sqlite)

	trustedProxies, err := s3proxy.ParseCIDRs(s.cfg.Server.TrustedProxies)
	if err != nil {
		return fmt.Errorf("trusted proxies: %w", err)
	}
	_ = proxies // reserved for future per-tenant gating

	resolver := s3proxy.NewBackendResolver(registry)

	// Bridge the slog logger into the logr API the proxy uses internally.
	proxyLog := logr.FromSlogHandler(s.logger.Handler()).WithName("s3proxy")

	proxyServer := s3proxy.NewServer(s3proxy.Config{
		Source:               merged,
		Backends:             resolver,
		Limiter:              s3proxy.NewLimiter(cfg.GlobalRPS, cfg.PerKeyRPS),
		IPLimiter:            s3proxy.NewIPLimiter(cfg.AnonymousRPS),
		Metrics:              s3proxy.NewMetrics(mtx.Prom.Registry),
		Log:                  proxyLog,
		HostSuffixes:         cfg.HostSuffixes,
		BucketCreated:        time.Now().UTC(),
		AnonymousEnabled:     cfg.AnonymousEnabled,
		TrustedProxies:       trustedProxies,
		Audit:                audit,
		Quotas:               quotaSvc,
		SuccessReadAuditRate: s.cfg.Audit.Sampling.ProxySuccessReadRate,
	})

	// Drop cached SigV4 signing keys whenever the SQLite source rebuilds
	// its in-memory map. Required because the cache key is (akid, date,
	// region, service); a credential that was deleted or disabled would
	// otherwise have its derived key serve until the date rolls over.
	if s.s3sqlite != nil {
		s.s3sqlite.SetOnReload(proxyServer.InvalidateSigningKeys)
	}

	s.s3http = &http.Server{
		Addr:              cfg.Listen,
		Handler:           proxyServer,
		ReadHeaderTimeout: 10 * time.Second,
		// SigV4 uploads can be slow on cold backends; give them room.
		WriteTimeout: 5 * time.Minute,
		IdleTimeout:  2 * time.Minute,
	}
	return nil
}

func (s *Server) Run(ctx context.Context) error {
	// Opt-in pprof listener. Off unless STOWAGE_PPROF_LISTEN is set —
	// existing in production code paths so the bench harness can profile
	// the running binary without a separate build, but never bound by
	// default because pprof exposes memory/goroutine internals to anyone
	// who can reach the socket.
	if addr := strings.TrimSpace(os.Getenv("STOWAGE_PPROF_LISTEN")); addr != "" {
		go func() {
			s.logger.Warn("pprof listener enabled (set STOWAGE_PPROF_LISTEN to empty to disable)", "addr", addr)
			if err := http.ListenAndServe(addr, nil); err != nil && !errors.Is(err, http.ErrServerClosed) {
				s.logger.Warn("pprof listener exited", "err", err.Error())
			}
		}()
	}

	// Long-running goroutines (HTTP, optional S3 proxy listener, optional
	// operator manager) share one error channel: a fatal exit from any tears
	// the process down. http.ErrServerClosed (shutdown path) is normalised
	// to nil so we don't surface it as a failure.
	errCh := make(chan error, 3)
	pending := 1
	go func() {
		err := s.http.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()
	if s.cfg.Operator.Enabled {
		pending++
		go func() {
			errCh <- opmgr.Start(ctx, opmgr.Config{
				Kubeconfig:   s.cfg.Operator.Kubeconfig,
				MetricsAddr:  s.cfg.Operator.MetricsAddr,
				OpsNamespace: s.cfg.Operator.OpsNamespace,
				ProxyURL:     s.cfg.Operator.ProxyURL,
				Registry:     s.registry,
				Webhook: opmgr.WebhookConfig{
					Enabled: s.cfg.Operator.Webhook.Enabled,
					Port:    s.cfg.Operator.Webhook.Port,
					CertDir: s.cfg.Operator.Webhook.CertDir,
				},
			}, s.logger)
		}()
	}
	if s.s3http != nil {
		// Start K8s informer first so the cache is primed before we accept
		// proxy traffic. The informer's lifecycle is tied to ctx so it keeps
		// receiving Secret events for the life of the server; the initial
		// sync deadline lives inside Start.
		if s.s3kube != nil {
			if err := s.s3kube.Start(ctx); err != nil {
				s.logger.Warn("s3 proxy: kubernetes source start failed; continuing with sqlite-only", "err", err.Error())
			}
		}
		go s.s3sqlite.Run(ctx, s.s3reloadIv)
		pending++
		go func() {
			s.logger.Info("s3 proxy listening", "addr", s.s3http.Addr)
			err := s.s3http.ListenAndServe()
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			errCh <- err
		}()
	}

	// Probe backends at startup so /api/backends reflects reality within a
	// few seconds of boot. Rerun periodically so the UI's health indicator
	// stays fresh.
	go func() {
		probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		s.registry.ProbeAll(probeCtx, 5*time.Second)
	}()
	go s.rePollBackends(ctx)

	go s.reapSessions(ctx)

	// Drain the auth touch-batcher so deferred last_seen_at writes hit
	// SQLite. Survives a shutdown — Run flushes once on ctx cancel.
	if s.auth != nil && s.auth.Sessions != nil {
		if s.auth.Sessions.Touch != nil {
			go s.auth.Sessions.Touch.Run(ctx)
		}
		if s.auth.Sessions.Cache != nil {
			go s.reapIdentityCache(ctx, s.auth.Sessions.Cache)
		}
	}

	// Audit drainer. Run flushes the queue on shutdown so events queued
	// in the last second of the process don't get lost.
	if s.audit != nil {
		go s.audit.Run(ctx)
	}

	// Quota scanner — set ScanInterval to a negative value in config to
	// disable. The scanner is best-effort: failures are logged and the
	// next tick retries. Same dial controls the size-tracking scanner;
	// rationale: both walk the same listings, and admins who want to
	// quiet down listings traffic want both off.
	if s.cfg.Quotas.ScanInterval >= 0 {
		go s.quotas.Run(ctx, s.cfg.Quotas.ScanInterval)
		go s.sizes.Run(ctx, s.cfg.Quotas.ScanInterval)
	}

	select {
	case <-ctx.Done():
		s.logger.Info("shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.Server.ShutdownTimeout)
		defer cancel()
		// Shut down both listeners under one deadline so the process exits
		// promptly even if one half is wedged. Closing the second is
		// best-effort — its error joins the first via errCh below.
		var shutdownErr error
		if err := s.http.Shutdown(shutdownCtx); err != nil {
			shutdownErr = err
		}
		if s.s3http != nil {
			if err := s.s3http.Shutdown(shutdownCtx); err != nil && shutdownErr == nil {
				shutdownErr = err
			}
		}
		_ = s.store.Close()
		if shutdownErr != nil {
			return shutdownErr
		}
		// Drain remaining background-goroutine errors. The operator
		// manager exits when ctx is done; both listeners exit when
		// Shutdown returns. All publish nil on clean exit.
		for i := 0; i < pending; i++ {
			<-errCh
		}
		return nil
	case err := <-errCh:
		_ = s.store.Close()
		return err
	}
}

func (s *Server) rePollBackends(ctx context.Context) {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.registry.ProbeAll(ctx, 5*time.Second)
		}
	}
}

// reapIdentityCache periodically evicts stale entries so a process that has
// been running for a long time doesn't accumulate dead sessions.
func (s *Server) reapIdentityCache(ctx context.Context, cache *auth.IdentityCache) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cache.Reap(time.Now().UTC())
		}
	}
}

func (s *Server) reapSessions(ctx context.Context) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := s.store.PurgeExpiredSessions(ctx, time.Now().UTC()); err == nil && n > 0 {
				s.logger.Debug("purged expired sessions", "count", n)
			}
		}
	}
}

// Store exposes the SQLite store so the CLI can share the same initialised
// database (migrations applied) for subcommands like create-admin.
func (s *Server) Store() *sqlite.Store { return s.store }

func buildStaticUser(cfg config.AuthConfig) (*auth.StaticUser, error) {
	if !cfg.Static.Enabled {
		return nil, nil
	}
	if cfg.Static.Username == "" {
		return nil, fmt.Errorf("auth.static.username is required when static is enabled")
	}
	if cfg.Static.PasswordHashEnv == "" {
		return nil, fmt.Errorf("auth.static.password_hash_env is required when static is enabled")
	}
	hash := os.Getenv(cfg.Static.PasswordHashEnv)
	if hash == "" {
		return nil, fmt.Errorf("env %s is empty; static auth needs the argon2id hash", cfg.Static.PasswordHashEnv)
	}
	return &auth.StaticUser{Username: cfg.Static.Username, PasswordHash: hash}, nil
}

func initOIDC(ctx context.Context, cfg config.Config, proxies *auth.ProxyTrust) (*authoidc.Provider, error) {
	o := cfg.Auth.OIDC
	secret := ""
	if o.ClientSecretEnv != "" {
		secret = os.Getenv(o.ClientSecretEnv)
	}
	redirect := ""
	if cfg.Server.PublicURL != "" {
		redirect = cfg.Server.PublicURL + "/auth/callback"
	}
	return authoidc.New(ctx, o, secret, redirect, proxies)
}

func containsMode(modes []string, want string) bool {
	for _, m := range modes {
		if m == want {
			return true
		}
	}
	return false
}
