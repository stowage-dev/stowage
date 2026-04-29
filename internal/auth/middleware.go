// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

type ctxKey int

const (
	ctxKeyIdentity ctxKey = iota + 1
)

// Identity is the resolved caller. It is attached to request context by
// SessionManager.Attach and read via IdentityFrom. Synthetic identities
// (e.g. the static admin) carry SyntheticUser=true so callers know the user
// row in SQLite may not exist.
type Identity struct {
	UserID        string
	Username      string
	Role          string // "admin" | "user" | "readonly"
	Source        string // "local" | "oidc:<issuer>" | "static"
	SessionID     string
	CSRFToken     string
	MustChangePW  bool
	SyntheticUser bool // true for static mode
}

func (i *Identity) IsAdmin() bool { return i != nil && i.Role == "admin" }

// Resolver is implemented by anything that can turn a session record into an
// Identity. Typically this is the Service wiring together SessionManager +
// user repo + static synth. Kept as an interface so layers don't depend on
// service internals.
type Resolver interface {
	ResolveIdentity(ctx context.Context, sess *sqlite.Session) (Identity, error)
}

// ErrIdentityInvalid is returned by Resolver when the session's user is
// missing/disabled.
var ErrIdentityInvalid = errors.New("identity invalid")

// Attach is middleware that attempts to resolve an identity from the session
// cookie. When a session exists but the identity cannot be resolved, the
// session cookie is cleared. Downstream handlers decide whether the identity
// is required via Require/RequireRole.
//
// Hot-path note: when an IdentityCache is wired, a cache hit serves the
// request without touching SQLite at all. On miss we Resolve (one GetSession)
// and ResolveIdentity (a single GetUserByID for non-static identities), then
// populate the cache. The CSRF middleware downstream reads the cached token
// from the request context rather than re-querying.
func (m *SessionManager) Attach(res Resolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(SessionCookieName)
			if err != nil || c.Value == "" {
				next.ServeHTTP(w, r)
				return
			}

			now := time.Now().UTC()
			if id, sessExp, shouldTouch, ok := m.Cache.Get(c.Value, now, m.IdleTimeout/2); ok {
				if shouldTouch {
					newExp := now.Add(m.Lifetime)
					m.persistTouch(r.Context(), c.Value, now, newExp)
					_ = sessExp // session expiry stays the cached value until next refresh
				}
				ctx := context.WithValue(r.Context(), ctxKeyIdentity, &id)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			sess, err := m.Resolve(r.Context(), w, r)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			id, err := res.ResolveIdentity(r.Context(), sess)
			if err != nil {
				_ = m.Revoke(r.Context(), w, r, sess.ID)
				next.ServeHTTP(w, r)
				return
			}
			m.Cache.Put(sess.ID, id, sess.ExpiresAt, sess.LastSeenAt, now)
			ctx := context.WithValue(r.Context(), ctxKeyIdentity, &id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// IdentityFrom returns the caller's Identity, or nil if unauthenticated.
func IdentityFrom(ctx context.Context) *Identity {
	v, _ := ctx.Value(ctxKeyIdentity).(*Identity)
	return v
}

// ContextWithIdentity is exported for tests that need to bypass the normal
// Attach middleware and inject a fake identity.
func ContextWithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, ctxKeyIdentity, id)
}

// Require is middleware that rejects requests without a resolved identity
// with 401. It is composed below the Attach middleware.
func Require(reject http.HandlerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IdentityFrom(r.Context()) == nil {
				reject(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireRole rejects requests whose identity does not have one of the given
// roles with 403.
func RequireRole(reject http.HandlerFunc, roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := IdentityFrom(r.Context())
			if id == nil {
				reject(w, r)
				return
			}
			for _, role := range roles {
				if id.Role == role {
					next.ServeHTTP(w, r)
					return
				}
			}
			reject(w, r)
		})
	}
}
