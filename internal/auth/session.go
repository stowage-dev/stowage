// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

const (
	SessionCookieName = "stowage_session"
	CSRFCookieName    = "stowage_csrf"
	CSRFHeaderName    = "X-CSRF-Token"
)

var (
	ErrNoSession      = errors.New("no session")
	ErrSessionExpired = errors.New("session expired")
)

// SessionManager handles issuing, resolving, and revoking sessions. It is
// the single choke-point for all auth modes.
type SessionManager struct {
	Store       *sqlite.Store
	Lifetime    time.Duration
	IdleTimeout time.Duration
	// CookieSecure controls the Secure flag on outbound cookies. Nil = auto
	// detect (HTTPS request or X-Forwarded-Proto=https on a trusted peer).
	CookieSecure *bool
	// Proxies decides whether to honour X-Forwarded-* headers on the
	// request. Nil-safe via methods returning false / RemoteAddr.
	Proxies *ProxyTrust
	// IdentityCache memoises sessionID → Identity for a short window so
	// authenticated requests don't perform GetSession+GetUserByID on every
	// hit. Optional; nil disables caching.
	Cache *IdentityCache
	// Touch coalesces last_seen_at writes so the sliding-refresh path is
	// off the request hot loop. Optional; nil falls back to synchronous
	// TouchSession calls.
	Touch *TouchBatcher
}

// IssueSession creates a new session, persists it, and writes the session +
// CSRF cookies onto w. Returns the session record.
func (m *SessionManager) Issue(ctx context.Context, w http.ResponseWriter, r *http.Request, userID, source string, flags sqlite.SessionFlags) (*sqlite.Session, error) {
	sid, err := RandomToken()
	if err != nil {
		return nil, err
	}
	csrf, err := RandomToken()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	sess := &sqlite.Session{
		ID:             sid,
		UserID:         userID,
		IdentitySource: source,
		CSRFToken:      csrf,
		IP:             m.clientIP(r),
		UserAgent:      r.UserAgent(),
		Flags:          flags,
		CreatedAt:      now,
		LastSeenAt:     now,
		ExpiresAt:      now.Add(m.Lifetime),
	}
	if err := m.Store.CreateSession(ctx, sess); err != nil {
		return nil, err
	}

	m.writeCookies(w, r, sess)
	return sess, nil
}

// Resolve returns the session and its cookie bits, refreshing last_seen_at
// and extending expiry. It returns ErrNoSession if no cookie is present and
// ErrSessionExpired if the session has aged out.
func (m *SessionManager) Resolve(ctx context.Context, w http.ResponseWriter, r *http.Request) (*sqlite.Session, error) {
	c, err := r.Cookie(SessionCookieName)
	if err != nil || c.Value == "" {
		return nil, ErrNoSession
	}
	sess, err := m.Store.GetSession(ctx, c.Value)
	if err != nil {
		return nil, ErrNoSession
	}
	now := time.Now().UTC()
	if now.After(sess.ExpiresAt) {
		_ = m.Store.DeleteSession(ctx, sess.ID)
		m.Cache.Invalidate(sess.ID)
		m.clearCookies(w, r)
		return nil, ErrSessionExpired
	}
	// Sliding refresh: push expiry forward only if we're past half the lifetime.
	if now.Sub(sess.LastSeenAt) > m.IdleTimeout/2 {
		newExp := now.Add(m.Lifetime)
		m.persistTouch(ctx, sess.ID, now, newExp)
		sess.LastSeenAt = now
		sess.ExpiresAt = newExp
		m.writeCookies(w, r, sess)
	}
	return sess, nil
}

// persistTouch writes last_seen_at + expires_at, preferring the async batcher
// so the synchronous request path doesn't pay the SQLite round-trip.
func (m *SessionManager) persistTouch(ctx context.Context, id string, now, expiresAt time.Time) {
	if m.Touch != nil {
		m.Touch.Enqueue(id, now, expiresAt)
		return
	}
	_ = m.Store.TouchSession(ctx, id, now, expiresAt)
}

// Revoke deletes the session row and clears cookies.
func (m *SessionManager) Revoke(ctx context.Context, w http.ResponseWriter, r *http.Request, id string) error {
	if id != "" {
		_ = m.Store.DeleteSession(ctx, id)
		m.Cache.Invalidate(id)
	}
	m.clearCookies(w, r)
	return nil
}

func (m *SessionManager) writeCookies(w http.ResponseWriter, r *http.Request, sess *sqlite.Session) {
	secure := m.secureFlag(r)
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    sess.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  sess.ExpiresAt,
	})
	// CSRF cookie: NOT HttpOnly, so JS can read it for the X-CSRF-Token header.
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    sess.CSRFToken,
		Path:     "/",
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  sess.ExpiresAt,
	})
}

func (m *SessionManager) clearCookies(w http.ResponseWriter, r *http.Request) {
	secure := m.secureFlag(r)
	for _, name := range []string{SessionCookieName, CSRFCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: name == SessionCookieName,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		})
	}
}

func (m *SessionManager) secureFlag(r *http.Request) bool {
	if m.CookieSecure != nil {
		return *m.CookieSecure
	}
	if r.TLS != nil {
		return true
	}
	// X-Forwarded-Proto is only meaningful when the immediate peer is a
	// trusted proxy. Outside that, it's attacker-controlled.
	if m.Proxies != nil {
		return m.Proxies.IsHTTPS(r)
	}
	return false
}

// clientIP returns the originating IP for r. The package-level function is
// the legacy callsite shape (no proxy trust). New code should use
// SessionManager.clientIP via the Proxies field.
func clientIP(r *http.Request) string {
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return h
	}
	return r.RemoteAddr
}

// clientIP is the trust-aware variant: when no ProxyTrust is configured,
// behaves identically to the package-level helper.
func (m *SessionManager) clientIP(r *http.Request) string {
	if m.Proxies != nil {
		return m.Proxies.ClientIP(r)
	}
	return clientIP(r)
}
