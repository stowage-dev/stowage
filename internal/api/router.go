// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/metrics"
)

// Deps groups the collaborators the router needs.
type Deps struct {
	Logger     *slog.Logger
	FrontendFS fs.FS
	Auth       *AuthDeps        // required once auth is wired (always, post Phase 1)
	Backends   *BackendDeps     // required once backends land (Phase 2+)
	Shares     *ShareDeps       // required once Phase 5 is wired
	Metrics    *metrics.Service // optional; nil disables request counting
	Audit      *AuditDeps       // optional; nil disables the viewer endpoint
	// S3Cred / S3Anon are optional; non-nil only when the embedded SigV4
	// proxy (cfg.S3Proxy.Enabled) is on. S3ProxyView fans the SQLite store
	// + optional Kubernetes informer cache out to the read-only admin
	// dashboard; it's also wired only when the proxy is enabled.
	S3Cred      *S3CredentialDeps
	S3Anon      *S3AnonymousDeps
	S3ProxyView *S3ProxyViewDeps
	// Proxies decides whether to honour X-Forwarded-* headers. Optional;
	// when nil the router behaves as if no proxy is trusted.
	Proxies *auth.ProxyTrust
}

// NewRouter wires up the HTTP handlers.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()

	reject401 := func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required", "")
	}
	reject403 := func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusForbidden, "forbidden", "insufficient privileges", "")
	}
	rejectCSRF := func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusForbidden, "csrf_invalid", "csrf token missing or invalid", "")
	}
	reject429 := func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts; try again later", "")
	}

	attach := d.Auth.Service.Sessions.Attach(d.Auth.Service)
	csrf := d.Auth.Service.Sessions.CSRF(rejectCSRF)
	rateLimited := d.Auth.RateLim.Middleware(nil, reject429)
	require := auth.Require(reject401)
	requireAdmin := auth.RequireRole(reject403, "admin")
	// requireWriter denies role=readonly any mutating operation. Used on every
	// endpoint with a write side-effect that isn't already admin-gated: object
	// CRUD, multipart, tags/metadata, folder/bulk delete, share grants, and
	// per-user pins. Readonly users keep full read access (lists, gets,
	// search, audit views are admin-only anyway).
	requireWriter := auth.RequireRole(reject403, "admin", "user")
	sessionRL := sessionRateLimit(d.Auth.SessionRateLim)

	r.Use(middleware.RequestID)
	// Replaces chi/middleware.RealIP, which trusts X-Forwarded-* on every
	// request. Our gate honours those headers from every immediate peer by
	// default, and narrows to server.trusted_proxies once that list is set.
	if d.Proxies != nil {
		r.Use(d.Proxies.Middleware)
	}
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)
	r.Use(requestLogger(d.Logger))
	// Attach runs on every request so /api/me reflects the current session.
	r.Use(attach)

	r.Get("/healthz", healthz)
	r.Get("/readyz", readyz)
	if d.Metrics != nil && d.Metrics.Prom != nil {
		// Prometheus scrape endpoint. Public by design (operators run it
		// behind a reverse proxy / network policy if they want to gate it);
		// avoids tying scrape success to session lifecycle.
		r.Method("GET", "/metrics", d.Metrics.Prom.Handler())
	}

	// Public auth endpoints (rate-limited, no session required).
	r.Route("/auth", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(rateLimited)
			r.Post("/login/local", d.Auth.handleLoginLocal)
			r.Get("/login/oidc", d.Auth.handleLoginOIDCStart)
			r.Get("/callback", d.Auth.handleCallback)
		})
		r.Post("/logout", d.Auth.handleLogout)
	})

	// /api: everything JSON. /api/auth/config is public; everything else
	// requires a session. Mutations require CSRF.
	r.Route("/api", func(r chi.Router) {
		// Count every API request for the admin dashboard. Wraps the
		// inner handler so it observes the final status code.
		if d.Metrics != nil {
			r.Use(d.Metrics.Middleware)
		}
		r.Get("/auth/config", d.Auth.handleAuthConfig)

		r.Group(func(r chi.Router) {
			r.Use(require)
			r.Use(csrf)
			// Force users with must_change_pw=true through the rotation
			// flow before letting them touch any other API surface.
			r.Use(requirePasswordRotated)
			// Per-session rate limit. Sits after Require so unauthenticated
			// requests don't pollute the bucket map.
			if sessionRL != nil {
				r.Use(sessionRL)
			}

			r.Get("/me", d.Auth.handleMe)
			r.Post("/me/password", d.Auth.handleChangeOwnPassword)

			// Unified search across all backends — Phase 8.
			r.Get("/search", d.Backends.handleSearch)

			// Pinned buckets — Phase 8. Mutations gated to admin|user;
			// readonly keeps the read endpoint.
			r.Route("/me/pins", func(r chi.Router) {
				r.Get("/", d.Auth.handleListPins)
				r.With(requireWriter).Post("/", d.Auth.handleCreatePin)
				r.With(requireWriter).Delete("/{bid}/{bucket}", d.Auth.handleDeletePin)
			})

			// Per-user S3 virtual credentials. Self-service: callers see
			// and mutate only the credentials they own. Operator-managed
			// (Kubernetes) credentials never appear here — they're tied
			// to BucketClaims, not stowage user accounts.
			if d.S3Cred != nil {
				r.Route("/me/s3-credentials", func(r chi.Router) {
					r.Get("/", d.S3Cred.handleMyList)
					r.With(requireWriter).Post("/", d.S3Cred.handleMyCreate)
					r.With(requireWriter).Patch("/{akid}", d.S3Cred.handleMyPatch)
					r.With(requireWriter).Delete("/{akid}", d.S3Cred.handleMyDelete)
				})
			}

			r.Route("/admin", func(r chi.Router) {
				r.Use(requireAdmin)
				// Phase 6 Slice C — admin dashboard.
				dash := &DashboardDeps{
					Metrics: d.Metrics,
					Quotas:  d.Backends.Quotas,
				}
				r.Get("/dashboard", dash.handleDashboard)
				r.Get("/backends/health", d.Backends.handleBackendHealth)
				if d.Audit != nil {
					r.Get("/audit", d.Audit.handleListAudit)
					r.Get("/audit.csv", d.Audit.handleAuditCSV)
				}
				r.Route("/users", func(r chi.Router) {
					r.Get("/", d.Auth.handleListUsers)
					r.Post("/", d.Auth.handleCreateUser)
					r.Get("/{id}", d.Auth.handleGetUser)
					r.Patch("/{id}", d.Auth.handlePatchUser)
					r.Post("/{id}/reset-password", d.Auth.handleAdminResetPassword)
					r.Post("/{id}/unlock", d.Auth.handleAdminUnlock)
					r.Delete("/{id}", d.Auth.handleDeleteUser)
				})

				// UI-driven endpoint (S3 backend) management. YAML-defined
				// backends still load at startup and are read-only here —
				// the handlers themselves enforce that policy.
				r.Route("/backends", func(r chi.Router) {
					r.Get("/", d.Backends.handleAdminListBackends)
					r.Post("/", d.Backends.handleAdminCreateBackend)
					r.Post("/test", d.Backends.handleAdminTestBackend)
					r.Get("/{bid}", d.Backends.handleAdminGetBackend)
					r.Patch("/{bid}", d.Backends.handleAdminPatchBackend)
					r.Delete("/{bid}", d.Backends.handleAdminDeleteBackend)
				})

				// Embedded S3 SigV4 proxy: per-tenant virtual credentials and
				// optional anonymous bindings. Wired only when the proxy
				// listener is enabled in config; absent endpoints reflect a
				// disabled feature, not a missing implementation.
				if d.S3Cred != nil {
					r.Route("/s3-credentials", func(r chi.Router) {
						r.Get("/", d.S3Cred.handleList)
						r.Post("/", d.S3Cred.handleCreate)
						r.Patch("/{akid}", d.S3Cred.handlePatch)
						r.Delete("/{akid}", d.S3Cred.handleDelete)
					})
				}
				if d.S3Anon != nil {
					r.Route("/s3-anonymous", func(r chi.Router) {
						r.Get("/", d.S3Anon.handleList)
						r.Post("/", d.S3Anon.handleUpsert)
						r.Delete("/{bid}/{bucket}", d.S3Anon.handleDelete)
					})
				}
				// Read-only merged view across SQLite + Kubernetes sources.
				// Powers the admin S3 Proxy dashboard. No mutating endpoints
				// here — those live under /s3-credentials and /s3-anonymous.
				if d.S3ProxyView != nil {
					r.Route("/s3-proxy", func(r chi.Router) {
						r.Get("/credentials", d.S3ProxyView.handleListCredentials)
						r.Get("/anonymous", d.S3ProxyView.handleListAnonymous)
					})
				}
			})

			r.Route("/shares", func(r chi.Router) {
				r.Get("/", d.Shares.handleListShares)
				r.With(requireWriter).Post("/", d.Shares.handleCreateShare)
				r.With(requireWriter).Delete("/{id}", d.Shares.handleRevokeShare)
			})

			r.Route("/backends", func(r chi.Router) {
				r.Get("/", d.Backends.handleListBackends)
				r.Route("/{bid}", func(r chi.Router) {
					r.Get("/", d.Backends.handleGetBackend)
					r.Get("/health", d.Backends.handleProbeBackend)

					r.Route("/buckets", func(r chi.Router) {
						r.Get("/", d.Backends.handleListBuckets)
						// Admin-only: bucket CRUD.
						r.With(requireAdmin).Post("/", d.Backends.handleCreateBucket)
						r.Route("/{bucket}", func(r chi.Router) {
							r.With(requireAdmin).Delete("/", d.Backends.handleDeleteBucket)

							// Bucket settings (Phase 6 Slice A) — admin-only
							// because they affect bucket-wide behaviour.
							r.Group(func(r chi.Router) {
								r.Use(requireAdmin)
								r.Get("/versioning", d.Backends.handleGetBucketVersioning)
								r.Put("/versioning", d.Backends.handlePutBucketVersioning)
								r.Get("/cors", d.Backends.handleGetBucketCORS)
								r.Put("/cors", d.Backends.handlePutBucketCORS)
								r.Get("/policy", d.Backends.handleGetBucketPolicy)
								r.Put("/policy", d.Backends.handlePutBucketPolicy)
								r.Delete("/policy", d.Backends.handleDeleteBucketPolicy)
								r.Get("/lifecycle", d.Backends.handleGetBucketLifecycle)
								r.Put("/lifecycle", d.Backends.handlePutBucketLifecycle)
								// Proxy-enforced quota (Phase 6 Slice B).
								r.Get("/quota", d.Backends.handleGetQuota)
								r.Put("/quota", d.Backends.handlePutQuota)
								r.Delete("/quota", d.Backends.handleDeleteQuota)
								r.Post("/quota/recompute", d.Backends.handleRecomputeQuota)
								// Per-bucket size-tracking toggle. Admin-only
								// because flipping it changes scanner load
								// and what every user sees in the listing.
								r.Put("/size-tracking", d.Backends.handlePutBucketSizeTracking)
							})

							// Read-only size endpoints — any authenticated
							// user, since the underlying data is already
							// derivable from listing.
							r.Get("/size-tracking", d.Backends.handleGetBucketSizeTracking)
							r.Get("/prefix-size", d.Backends.handlePrefixSize)

							r.Get("/objects", d.Backends.handleListObjects)
							r.With(requireWriter).Post("/objects/delete", d.Backends.handleBulkDelete)
							r.With(requireWriter).Post("/objects/delete-prefix", d.Backends.handleDeletePrefix)
							r.With(requireWriter).Post("/objects/folder", d.Backends.handleCreateFolder)
							r.With(requireWriter).Post("/objects/copy-prefix", d.Backends.handleCopyPrefix)
							r.Get("/objects/zip", d.Backends.handleZipDownload)

							r.Get("/object", d.Backends.handleGetObject)
							r.Head("/object", d.Backends.handleHeadObject)
							r.Get("/object/info", d.Backends.handleHeadObject)
							r.With(requireWriter).Post("/object", d.Backends.handleUploadObject)
							r.With(requireWriter).Delete("/object", d.Backends.handleDeleteObject)
							r.With(requireWriter).Post("/object/copy", d.Backends.handleCopyObject)
							r.Get("/object/tags", d.Backends.handleGetObjectTags)
							r.With(requireWriter).Put("/object/tags", d.Backends.handlePutObjectTags)
							r.With(requireWriter).Put("/object/metadata", d.Backends.handleUpdateObjectMetadata)
							r.Get("/object/versions", d.Backends.handleListObjectVersions)

							r.Route("/multipart", func(r chi.Router) {
								r.Get("/", d.Backends.handleMultipartList)
								r.With(requireWriter).Post("/", d.Backends.handleMultipartCreate)
								r.With(requireWriter).Delete("/", d.Backends.handleMultipartAbort)
								r.With(requireWriter).Post("/complete", d.Backends.handleMultipartComplete)
								r.With(requireWriter).Put("/parts/{part}", d.Backends.handleMultipartUploadPart)
							})
						})
					})
				})
			})
		})
	})

	// Public share endpoints. Rate-limited per IP (10 req/min default) so a
	// leaked code can't be used to probe passwords at scale.
	//
	// The bare /s/{code} URL is intentionally NOT registered here — it falls
	// through to the SvelteKit SPA, which renders the recipient-facing
	// preview page. Only the JSON+bytes plumbing lives in Go.
	shareRL := d.Shares.RateLim.Middleware(nil, reject429)
	r.Group(func(r chi.Router) {
		r.Use(shareRL)
		r.Get("/s/{code}/info", d.Shares.handleShareInfo)
		r.Post("/s/{code}/unlock", d.Shares.handleShareUnlock)
		r.Get("/s/{code}/raw", d.Shares.handleShareRaw)
	})

	if d.FrontendFS != nil {
		r.Handle("/*", spaHandler(d.FrontendFS))
	}

	return r
}

// sessionRateLimit builds a middleware that enforces a per-session req/min
// ceiling on /api/* (Phase 6 spec). Returns nil when rl is nil so callers
// can use the result directly with chi.Use without a guard. The bucket key
// is the resolved session ID; unauthenticated callers won't reach here
// because Require runs first.
//
// On limit, responds with 429 + Retry-After: <window-seconds>. The window
// is coarse — a tight retry is fine for clients that respect it, and a
// non-respecting client gets the same 429 again on the next attempt.
func sessionRateLimit(rl *auth.RateLimiter) func(http.Handler) http.Handler {
	if rl == nil {
		return nil
	}
	retryAfter := strconv.Itoa(int(rl.Window.Seconds()))
	keyFn := func(r *http.Request) string {
		if id := auth.IdentityFrom(r.Context()); id != nil {
			return id.SessionID
		}
		// Fall back to remote IP so we still rate-limit if for some reason
		// a request slipped past Require (defensive — shouldn't happen).
		return r.RemoteAddr
	}
	return rl.Middleware(keyFn, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", retryAfter)
		writeError(w, http.StatusTooManyRequests, "rate_limited",
			"too many requests on this session; slow down or wait a minute", "")
	})
}

// spaHandler serves the SvelteKit static build and falls back to index.html
// for unknown paths so the SPA router can take over.
//
// Only GET and HEAD are honoured. Without this gate the underlying
// http.FileServer responds 200 to TRACE / CONNECT / arbitrary verbs, which
// scanners flag and which has no defensible use case.
//
// Index responses get a per-build CSP that includes sha256 hashes for the
// inline <script> blocks SvelteKit emits; everything else keeps the strict
// baseCSP set by the global securityHeaders middleware.
func spaHandler(root fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(root))
	indexCSPValue := indexCSP(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		path := r.URL.Path
		if path == "/" {
			w.Header().Set("Content-Security-Policy", indexCSPValue)
			fileServer.ServeHTTP(w, r)
			return
		}
		clean := path
		if clean[0] == '/' {
			clean = clean[1:]
		}
		if _, err := fs.Stat(root, clean); err != nil {
			w.Header().Set("Content-Security-Policy", indexCSPValue)
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
