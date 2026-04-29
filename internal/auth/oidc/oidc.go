// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package oidc implements the OIDC authorization-code + PKCE flow for
// stowage. First-login auto-provisions a user row in the local users table
// with identity_source='oidc:<issuer>'.
package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/config"
	"github.com/stowage-dev/stowage/internal/store/sqlite"

	goidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/oklog/ulid/v2"
	"golang.org/x/oauth2"
)

// Provider wraps the go-oidc provider + oauth2.Config + role-mapping for a
// single configured issuer.
type Provider struct {
	Issuer      string
	ClientID    string
	provider    *goidc.Provider
	oauth2      *oauth2.Config
	verifier    *goidc.IDTokenVerifier
	roleClaim   string
	roleMapping map[string][]string
	// Proxies decides whether to honour X-Forwarded-Proto when picking
	// the Secure flag for the short-lived state/verifier cookies. Optional;
	// nil falls back to "trust nothing" (Secure only on direct TLS).
	Proxies *auth.ProxyTrust
}

// New builds a Provider by fetching the issuer's discovery document.
func New(ctx context.Context, cfg config.OIDCConfig, clientSecret, redirectURL string, proxies *auth.ProxyTrust) (*Provider, error) {
	if cfg.Issuer == "" || cfg.ClientID == "" {
		return nil, errors.New("oidc: issuer and client_id are required")
	}
	prov, err := goidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{goidc.ScopeOpenID, "profile", "email"}
	}

	return &Provider{
		Issuer:      cfg.Issuer,
		ClientID:    cfg.ClientID,
		provider:    prov,
		verifier:    prov.Verifier(&goidc.Config{ClientID: cfg.ClientID}),
		roleClaim:   cfg.RoleClaim,
		roleMapping: cfg.RoleMapping,
		Proxies:     proxies,
		oauth2: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: clientSecret,
			Endpoint:     prov.Endpoint(),
			RedirectURL:  redirectURL,
			Scopes:       scopes,
		},
	}, nil
}

func (p *Provider) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if p.Proxies != nil {
		return p.Proxies.IsHTTPS(r)
	}
	return false
}

const (
	stateCookie    = "stowage_oidc_state"
	verifierCookie = "stowage_oidc_verifier"
	flowCookieTTL  = 10 * time.Minute
)

// StartLogin generates PKCE + state, stores them in short-lived cookies,
// and writes a redirect to the authorize endpoint.
func (p *Provider) StartLogin(w http.ResponseWriter, r *http.Request) {
	state, err := auth.RandomToken()
	if err != nil {
		http.Error(w, "oidc: state error", http.StatusInternalServerError)
		return
	}
	verifier, err := pkceVerifier()
	if err != nil {
		http.Error(w, "oidc: pkce error", http.StatusInternalServerError)
		return
	}
	challenge := pkceChallenge(verifier)

	secure := p.isHTTPS(r)
	setFlowCookie(w, stateCookie, state, secure)
	setFlowCookie(w, verifierCookie, verifier, secure)

	authURL := p.oauth2.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// CallbackResult is the output of Callback: a validated, freshly-provisioned
// or reused local user row ready for session issuance.
type CallbackResult struct {
	User *sqlite.User
}

// Callback validates the auth code, exchanges it for tokens, verifies the ID
// token, and upserts a user record. It clears the flow cookies on exit.
func (p *Provider) Callback(ctx context.Context, r *http.Request, w http.ResponseWriter, store *sqlite.Store) (*CallbackResult, error) {
	defer clearFlowCookies(w, r, p.isHTTPS(r))

	if e := r.URL.Query().Get("error"); e != "" {
		return nil, fmt.Errorf("oidc: provider returned error %q: %s", e, r.URL.Query().Get("error_description"))
	}

	stateGot := r.URL.Query().Get("state")
	stateCk, err := r.Cookie(stateCookie)
	if err != nil || stateCk.Value == "" || stateCk.Value != stateGot {
		return nil, errors.New("oidc: state mismatch")
	}
	verCk, err := r.Cookie(verifierCookie)
	if err != nil || verCk.Value == "" {
		return nil, errors.New("oidc: pkce verifier missing")
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		return nil, errors.New("oidc: no code")
	}

	tok, err := p.oauth2.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", verCk.Value),
	)
	if err != nil {
		return nil, fmt.Errorf("oidc: exchange: %w", err)
	}
	rawIDToken, _ := tok.Extra("id_token").(string)
	if rawIDToken == "" {
		return nil, errors.New("oidc: no id_token in response")
	}
	idTok, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("oidc: verify id_token: %w", err)
	}

	var claims struct {
		Sub               string   `json:"sub"`
		Email             string   `json:"email"`
		EmailVerified     bool     `json:"email_verified"`
		PreferredUsername string   `json:"preferred_username"`
		Name              string   `json:"name"`
		Groups            []string `json:"groups"`
	}
	// First, basic known-shape claims.
	if err := idTok.Claims(&claims); err != nil {
		return nil, fmt.Errorf("oidc: parse claims: %w", err)
	}
	// Then the raw map so we can pull out the configured role claim even if
	// it is a nested path like "realm_access.roles".
	var rawClaims map[string]any
	_ = idTok.Claims(&rawClaims)

	if claims.Sub == "" {
		return nil, errors.New("oidc: id_token missing sub claim")
	}
	username := firstNonEmpty(claims.PreferredUsername, claims.Email, claims.Sub)
	if username == "" {
		return nil, errors.New("oidc: no usable username in id_token")
	}

	role := p.mapRole(claims.Groups, rawClaims)
	source := "oidc:" + shortIssuer(p.Issuer)

	user, err := upsertOIDCUser(ctx, store, claims.Sub, username, claims.Email, role, source)
	if err != nil {
		return nil, err
	}
	return &CallbackResult{User: user}, nil
}

// EndSessionURL returns the issuer's end_session_endpoint with id_token_hint
// set, if advertised. Callers use it to RP-initiated logout on the IdP.
func (p *Provider) EndSessionURL(idTokenHint, postLogoutRedirect string) string {
	var claims struct {
		EndSession string `json:"end_session_endpoint"`
	}
	if err := p.provider.Claims(&claims); err != nil || claims.EndSession == "" {
		return ""
	}
	u, err := url.Parse(claims.EndSession)
	if err != nil {
		return ""
	}
	q := u.Query()
	if idTokenHint != "" {
		q.Set("id_token_hint", idTokenHint)
	}
	if postLogoutRedirect != "" {
		q.Set("post_logout_redirect_uri", postLogoutRedirect)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func (p *Provider) mapRole(groups []string, raw map[string]any) string {
	// The role_claim config may point at a custom or nested claim.
	values := groups
	if p.roleClaim != "" && p.roleClaim != "groups" {
		values = extractStringSlice(raw, p.roleClaim)
	}
	for role, want := range p.roleMapping {
		for _, w := range want {
			for _, v := range values {
				if v == w {
					return role
				}
			}
		}
	}
	return "user"
}

func extractStringSlice(raw map[string]any, path string) []string {
	parts := strings.Split(path, ".")
	var cur any = raw
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[p]
	}
	switch v := cur.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	case string:
		return []string{v}
	}
	return nil
}

// upsertOIDCUser links the IdP-issued (source, sub) pair to a stowage user
// row. The sub is the immutable identifier; we never key off the username
// across sessions.
//
// Lookup order:
//  1. (source, sub) — the canonical path. Returns the existing row.
//  2. username under the same source AND oidc_subject still NULL — used
//     once, the first time a pre-migration-v6 OIDC user signs in. We pin
//     the sub to the row and from then on path 1 wins.
//  3. fall through to creating a new row, suffixing the username if a
//     local user already owns the bare username.
//
// IMPORTANT: we never adopt an existing row whose oidc_subject is non-NULL
// based on username — doing so is the F-2 takeover vector this fix exists
// to close.
func upsertOIDCUser(ctx context.Context, store *sqlite.Store, sub, username, email, role, source string) (*sqlite.User, error) {
	// 1. Canonical lookup by (source, sub).
	u, err := store.GetUserByOIDCSubject(ctx, source, sub)
	if err == nil {
		return applyOIDCPatch(ctx, store, u, username, email, role)
	}
	if !errors.Is(err, sqlite.ErrUserNotFound) {
		return nil, err
	}

	// 2. Legacy linking: a row with the same (source, username) and no sub
	// recorded yet predates migration v6 — adopt it once.
	if existing, err := store.GetUserByUsername(ctx, username); err == nil &&
		existing.IdentitySource == source && !existing.OIDCSubject.Valid {
		if err := store.SetOIDCSubject(ctx, existing.ID, sub); err != nil {
			return nil, err
		}
		existing.OIDCSubject = sql.NullString{String: sub, Valid: true}
		return applyOIDCPatch(ctx, store, existing, username, email, role)
	}

	// 3. Provision a new row. If the bare username collides with anyone
	// (local user or a different OIDC user), suffix it. We never reuse an
	// existing row at this point — the (source, sub) lookup above already
	// would have found one if the IdP knew us.
	if other, err := store.GetUserByUsername(ctx, username); err == nil && other != nil {
		username = username + "@" + strings.TrimPrefix(source, "oidc:")
	}

	now := time.Now().UTC()
	nu := &sqlite.User{
		ID:             ulid.Make().String(),
		Username:       username,
		Role:           role,
		IdentitySource: source,
		Enabled:        true,
		CreatedAt:      now,
		PWChangedAt:    now,
		OIDCSubject:    sql.NullString{String: sub, Valid: true},
	}
	if email != "" {
		nu.Email.String = email
		nu.Email.Valid = true
	}
	if err := store.CreateUser(ctx, nu); err != nil {
		return nil, err
	}
	return nu, nil
}

// applyOIDCPatch syncs an existing row's role/email to the latest claim
// values. Username is also synced when it's drifted, so admins can rename
// a user upstream and have it reflect locally — this is safe because the
// row is keyed by (source, sub), not username.
func applyOIDCPatch(ctx context.Context, store *sqlite.Store, u *sqlite.User, username, email, role string) (*sqlite.User, error) {
	patch := sqlite.UserPatch{Role: &role}
	if email != "" {
		ns := sql.NullString{String: email, Valid: true}
		patch.Email = &ns
	}
	if err := store.UpdateUser(ctx, u.ID, patch); err != nil {
		return nil, err
	}
	if username != "" && username != u.Username {
		// Best-effort rename. Conflict on the unique index → keep old
		// username; the row is still pinned by sub so the session is correct.
		_ = store.RenameUser(ctx, u.ID, username)
	}
	return store.GetUserByID(ctx, u.ID)
}

func shortIssuer(issuer string) string {
	u, err := url.Parse(issuer)
	if err != nil {
		return issuer
	}
	return u.Host
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func pkceVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func setFlowCookie(w http.ResponseWriter, name, value string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(flowCookieTTL),
	})
}

func clearFlowCookies(w http.ResponseWriter, _ *http.Request, secure bool) {
	for _, name := range []string{stateCookie, verifierCookie} {
		http.SetCookie(w, &http.Cookie{
			Name: name, Value: "", Path: "/", MaxAge: -1,
			HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
		})
	}
}
