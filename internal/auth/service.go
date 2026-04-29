// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"errors"
	"time"

	"github.com/stowage-dev/stowage/internal/config"
	"github.com/stowage-dev/stowage/internal/store/sqlite"

	"github.com/oklog/ulid/v2"
)

// Service is the facade that composes auth primitives with the SQLite store.
// HTTP handlers talk to the Service; the Service owns password checks,
// lockout bookkeeping, and user provisioning.
type Service struct {
	Cfg      config.AuthConfig
	Sessions *SessionManager
	Store    *sqlite.Store
	Policy   PasswordPolicy
	// Static user; populated when config has auth.static.enabled=true.
	Static *StaticUser
}

type StaticUser struct {
	Username     string
	PasswordHash string // argon2id encoded
}

const StaticUserID = "static"
const SourceLocal = "local"
const SourceStatic = "static"

// identityCacheTTL caps how long Attach can serve a request from cache. Kept
// short so admin actions take effect quickly even if explicit invalidation
// hooks miss a path. Logout, password change, role change, and disable all
// invalidate explicitly so the common cases are immediate regardless of TTL.
const identityCacheTTL = 30 * time.Second

// NewService builds a Service from config and an open store. The caller is
// responsible for loading any referenced env-var secrets before calling.
func NewService(cfg config.AuthConfig, store *sqlite.Store, static *StaticUser) *Service {
	return &Service{
		Cfg:   cfg,
		Store: store,
		Sessions: &SessionManager{
			Store:       store,
			Lifetime:    cfg.Session.Lifetime,
			IdleTimeout: cfg.Session.IdleTimeout,
			Cache:       NewIdentityCache(identityCacheTTL),
			Touch:       NewTouchBatcher(store, 2*time.Second),
		},
		Policy: PasswordPolicy{
			MinLength:    cfg.Local.Password.MinLength,
			PreventReuse: cfg.Local.Password.PreventReuse,
		},
		Static: static,
	}
}

func (s *Service) ModeEnabled(mode string) bool {
	for _, m := range s.Cfg.Modes {
		if m == mode {
			return true
		}
	}
	return false
}

// ResolveIdentity satisfies the Resolver interface used by Attach middleware.
// The session must already be loaded (Attach hands its result through) so we
// don't repeat a SELECT against the sessions table.
func (s *Service) ResolveIdentity(ctx context.Context, sess *sqlite.Session) (Identity, error) {
	if sess == nil {
		return Identity{}, ErrIdentityInvalid
	}

	id := Identity{
		SessionID:    sess.ID,
		CSRFToken:    sess.CSRFToken,
		Source:       sess.IdentitySource,
		MustChangePW: sess.Flags&sqlite.FlagMustChangePW != 0,
	}

	if sess.IdentitySource == SourceStatic {
		if s.Static == nil {
			return Identity{}, ErrIdentityInvalid
		}
		id.UserID = StaticUserID
		id.Username = s.Static.Username
		id.Role = "admin"
		id.SyntheticUser = true
		return id, nil
	}

	u, err := s.Store.GetUserByID(ctx, sess.UserID)
	if err != nil || !u.Enabled {
		return Identity{}, ErrIdentityInvalid
	}
	id.UserID = u.ID
	id.Username = u.Username
	id.Role = u.Role
	return id, nil
}

// LoginLocal verifies username+password against the local user table, honours
// lockout, and returns a user ID on success. Callers must then Issue a
// session. All failure paths return ErrLoginFailed — never leak which
// property was wrong.
var ErrLoginFailed = errors.New("invalid credentials")
var ErrAccountLocked = errors.New("account locked")
var ErrAccountDisabled = errors.New("account disabled")

type LoginResult struct {
	UserID       string
	MustChangePW bool
}

func (s *Service) LoginLocal(ctx context.Context, username, password string) (LoginResult, error) {
	// Dummy hash so failure for a missing user takes roughly the same time
	// as a real verify. Uniform login errors per §7.15.
	const dummyHash = "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	u, err := s.Store.GetUserByUsername(ctx, username)
	if err != nil {
		_ = VerifyPassword(password, dummyHash)
		return LoginResult{}, ErrLoginFailed
	}
	if !u.Enabled {
		_ = VerifyPassword(password, dummyHash)
		return LoginResult{}, ErrAccountDisabled
	}
	if u.LockedUntil.Valid && u.LockedUntil.Time.After(time.Now().UTC()) {
		return LoginResult{}, ErrAccountLocked
	}
	if u.IdentitySource != SourceLocal {
		// OIDC users must log in via OIDC.
		return LoginResult{}, ErrLoginFailed
	}

	if err := VerifyPassword(password, u.PasswordHash); err != nil {
		maxAttempts := s.Cfg.Local.Lockout.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = 5
		}
		window := s.Cfg.Local.Lockout.Window
		if window <= 0 {
			window = 15 * time.Minute
		}
		if _, _, e := s.Store.RecordLoginFailure(ctx, u.ID, maxAttempts, window); e != nil {
			return LoginResult{}, e
		}
		return LoginResult{}, ErrLoginFailed
	}

	if err := s.Store.RecordLoginSuccess(ctx, u.ID); err != nil {
		return LoginResult{}, err
	}
	return LoginResult{UserID: u.ID, MustChangePW: u.MustChangePW}, nil
}

// LoginStatic verifies against the static bootstrap admin.
func (s *Service) LoginStatic(_ context.Context, username, password string) error {
	if s.Static == nil {
		return ErrLoginFailed
	}
	if username != s.Static.Username {
		_ = VerifyPassword(password, s.Static.PasswordHash)
		return ErrLoginFailed
	}
	if err := VerifyPassword(password, s.Static.PasswordHash); err != nil {
		return ErrLoginFailed
	}
	return nil
}

// ChangeOwnPassword verifies currentPassword, applies policy to newPassword,
// and updates the hash. On success, all other sessions for the user are
// revoked per §7.14.
func (s *Service) ChangeOwnPassword(ctx context.Context, userID, keepSessionID, current, newpw string) error {
	u, err := s.Store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if u.IdentitySource != SourceLocal {
		return errors.New("only local accounts can change passwords here")
	}
	if err := VerifyPassword(current, u.PasswordHash); err != nil {
		return ErrLoginFailed
	}
	if err := s.Policy.Check(newpw); err != nil {
		return err
	}
	if s.Policy.PreventReuse {
		if VerifyPassword(newpw, u.PasswordHash) == nil {
			return &PolicyError{Reason: "new password must differ from current password"}
		}
	}
	hash, err := HashPassword(newpw)
	if err != nil {
		return err
	}
	if err := s.Store.SetPasswordHash(ctx, userID, hash, false); err != nil {
		return err
	}
	if err := s.Store.DeleteUserSessions(ctx, userID, keepSessionID); err != nil {
		return err
	}
	if keepSessionID != "" {
		_ = s.Store.ClearSessionFlags(ctx, keepSessionID, sqlite.FlagMustChangePW)
	}
	// Drop every cached identity for this user so revoked sessions can't
	// keep authenticating off a warm cache, and so the surviving session
	// reflects must_change_pw=false on the very next request.
	s.Sessions.Cache.InvalidateUser(userID)
	return nil
}

// CreateLocalUser provisions a user record for the local identity source.
// When mustChangePW is true the user is forced to rotate on first login.
func (s *Service) CreateLocalUser(ctx context.Context, username, email, password, role, createdBy string, mustChangePW bool) (*sqlite.User, error) {
	if !validRole(role) {
		return nil, errors.New("invalid role")
	}
	if err := s.Policy.Check(password); err != nil {
		return nil, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	u := &sqlite.User{
		ID:             ulid.Make().String(),
		Username:       username,
		PasswordHash:   hash,
		Role:           role,
		IdentitySource: SourceLocal,
		Enabled:        true,
		MustChangePW:   mustChangePW,
		CreatedAt:      now,
		PWChangedAt:    now,
	}
	if email != "" {
		u.Email.String = email
		u.Email.Valid = true
	}
	if createdBy != "" {
		u.CreatedBy.String = createdBy
		u.CreatedBy.Valid = true
	}
	if err := s.Store.CreateUser(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// AdminResetPassword sets a new password on a user account.
//
// For an admin resetting *someone else's* password, pass keepSessionID="" and
// mustChange=true: every session for that user is revoked and they will be
// forced to rotate on next sign-in (the admin chose the password, not them).
//
// For an admin resetting *their own* password through this endpoint, pass the
// caller's session ID and mustChange=false: other sessions are revoked, the
// current one is preserved, and no rotate-on-next-login flag is set — the
// user just chose this password themselves, so forcing them to immediately
// pick another one is nonsensical.
func (s *Service) AdminResetPassword(ctx context.Context, userID, keepSessionID, newpw string, mustChange bool) error {
	if err := s.Policy.Check(newpw); err != nil {
		return err
	}
	hash, err := HashPassword(newpw)
	if err != nil {
		return err
	}
	if err := s.Store.SetPasswordHash(ctx, userID, hash, mustChange); err != nil {
		return err
	}
	if err := s.Store.DeleteUserSessions(ctx, userID, keepSessionID); err != nil {
		return err
	}
	if keepSessionID != "" {
		_ = s.Store.ClearSessionFlags(ctx, keepSessionID, sqlite.FlagMustChangePW)
	}
	s.Sessions.Cache.InvalidateUser(userID)
	return nil
}

func validRole(r string) bool {
	return r == "admin" || r == "user" || r == "readonly"
}
