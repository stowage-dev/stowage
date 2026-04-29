// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package shares implements the proxy-layer sharing feature (spec §5 Phase 5).
//
// Every share is recorded in the SQLite store; the proxy mints a short code
// and serves GET /s/:code itself, streaming bytes from the underlying S3
// backend. This is the differentiating feature versus raw presigned URLs:
// the proxy enforces expiry, download caps, password gating, and
// revocation — none of which presigned URLs support.
package shares

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/backend"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// Typed errors surfaced by Resolve. Handlers map these to HTTP statuses.
var (
	ErrNotFound         = errors.New("share not found")
	ErrRevoked          = errors.New("share revoked")
	ErrExpired          = errors.New("share expired")
	ErrExhausted        = errors.New("share download limit reached")
	ErrPasswordRequired = errors.New("share requires a password")
	ErrPasswordMismatch = errors.New("wrong password")
	ErrBackendGone      = errors.New("share target backend is unavailable")
	ErrInvalidParams    = errors.New("invalid share parameters")
)

// Service owns the create/resolve/revoke flows. HTTP handlers call it;
// it composes the SQLite repo, backend registry, and (when wired) an audit
// recorder.
type Service struct {
	Store    *sqlite.Store
	Backends *backend.Registry
	Logger   *slog.Logger
	// Clock is injectable so tests don't have to wait real time for expiry
	// checks. Defaults to time.Now when nil.
	Clock func() time.Time
}

func (s *Service) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

// CreateParams is the input for Service.Create — one record's worth of
// user-supplied configuration plus the context the caller must supply.
type CreateParams struct {
	BackendID    string
	Bucket       string
	Key          string
	ExpiresAt    *time.Time // nil = never
	Password     string     // empty = unprotected
	MaxDownloads int64      // 0 = unlimited
	Disposition  string     // "" defaults to "attachment"; accepts "inline"
}

func (p CreateParams) validate(now time.Time) error {
	if p.BackendID == "" || p.Bucket == "" || p.Key == "" {
		return fmt.Errorf("%w: backend_id, bucket and key are required", ErrInvalidParams)
	}
	if p.ExpiresAt != nil && !p.ExpiresAt.After(now) {
		return fmt.Errorf("%w: expires_at must be in the future", ErrInvalidParams)
	}
	if p.MaxDownloads < 0 {
		return fmt.Errorf("%w: max_downloads must be >= 0", ErrInvalidParams)
	}
	if p.Disposition != "" && p.Disposition != "attachment" && p.Disposition != "inline" {
		return fmt.Errorf("%w: disposition must be attachment or inline", ErrInvalidParams)
	}
	return nil
}

// Create mints a new share. The object is HEAD-checked up front so typos
// don't yield a broken link — we'd rather fail at create time than on first
// recipient click.
func (s *Service) Create(ctx context.Context, createdBy string, p CreateParams) (*sqlite.Share, error) {
	now := s.now()
	if err := p.validate(now); err != nil {
		return nil, err
	}
	b, ok := s.Backends.Get(p.BackendID)
	if !ok {
		return nil, ErrBackendGone
	}
	if _, err := b.HeadObject(ctx, p.Bucket, p.Key, ""); err != nil {
		return nil, fmt.Errorf("%w: target object not reachable", ErrInvalidParams)
	}

	hash := ""
	if p.Password != "" {
		h, err := auth.HashPassword(p.Password)
		if err != nil {
			return nil, fmt.Errorf("hash password: %w", err)
		}
		hash = h
	}
	disposition := p.Disposition
	if disposition == "" {
		disposition = "attachment"
	}

	sh := &sqlite.Share{
		ID:           ulid.Make().String(),
		BackendID:    p.BackendID,
		Bucket:       p.Bucket,
		Key:          p.Key,
		CreatedBy:    createdBy,
		CreatedAt:    now,
		PasswordHash: hash,
		Disposition:  disposition,
	}
	if p.ExpiresAt != nil {
		sh.ExpiresAt = sql.NullTime{Time: p.ExpiresAt.UTC(), Valid: true}
	}
	if p.MaxDownloads > 0 {
		sh.MaxDownloads = sql.NullInt64{Int64: p.MaxDownloads, Valid: true}
	}

	// Retry on astronomically-unlikely code collisions. Ten bytes of entropy
	// makes this practically unreachable, but the loop costs nothing.
	for attempt := 0; attempt < 5; attempt++ {
		code, err := newShareCode()
		if err != nil {
			return nil, fmt.Errorf("generate code: %w", err)
		}
		sh.Code = code
		err = s.Store.InsertShare(ctx, sh)
		if err == nil {
			return sh, nil
		}
		if !errors.Is(err, sqlite.ErrShareCodeTaken) {
			return nil, err
		}
	}
	return nil, errors.New("shares: could not mint a unique code after retries")
}

// Lookup runs all gate checks except password verification. The handler
// uses this when the recipient has already proven they know the password
// (via an HMAC-signed unlock cookie) so we don't have to keep the cleartext
// around between requests.
//
// Callers should NOT use Lookup as a way to bypass password protection on
// fresh requests — only as a continuation after a successful Resolve.
func (s *Service) Lookup(ctx context.Context, code string) (*sqlite.Share, error) {
	sh, err := s.Store.GetShareByCode(ctx, code)
	if err != nil {
		if errors.Is(err, sqlite.ErrShareNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if sh.Revoked {
		return nil, ErrRevoked
	}
	now := s.now()
	if sh.ExpiresAt.Valid && !sh.ExpiresAt.Time.After(now) {
		return nil, ErrExpired
	}
	if sh.MaxDownloads.Valid && sh.DownloadCount >= sh.MaxDownloads.Int64 {
		return nil, ErrExhausted
	}
	return sh, nil
}

// Resolve looks up a share by code and runs all gate checks. On success the
// share is returned for the caller to stream; the caller must subsequently
// call RecordAccess to atomically bump the counter. (Splitting resolution
// from recording lets the caller stream bytes first and only count the
// download once delivery actually started.)
//
// password is ignored when the share is unprotected. Pass "" for the first
// probe — if ErrPasswordRequired is returned the caller should prompt.
func (s *Service) Resolve(ctx context.Context, code, password string) (*sqlite.Share, error) {
	sh, err := s.Lookup(ctx, code)
	if err != nil {
		return nil, err
	}
	if sh.PasswordHash != "" {
		if password == "" {
			return nil, ErrPasswordRequired
		}
		if err := auth.VerifyPassword(password, sh.PasswordHash); err != nil {
			return nil, ErrPasswordMismatch
		}
	}
	return sh, nil
}

// RecordAccess atomically increments the download count. Must be called
// after bytes start flowing to the recipient — the update guards against
// racing past max_downloads via a conditional SQL clause.
func (s *Service) RecordAccess(ctx context.Context, id string) error {
	return s.Store.IncrementShareAccess(ctx, id, s.now())
}

// Revoke flips the revoked flag. Owners may revoke their own shares; admins
// may revoke any.
func (s *Service) Revoke(ctx context.Context, actingUserID, actingRole, shareID string) error {
	sh, err := s.Store.GetShareByID(ctx, shareID)
	if err != nil {
		if errors.Is(err, sqlite.ErrShareNotFound) {
			return ErrNotFound
		}
		return err
	}
	if actingRole != "admin" && sh.CreatedBy != actingUserID {
		return ErrNotFound // don't leak existence to unauthorised users
	}
	if sh.Revoked {
		return nil
	}
	return s.Store.RevokeShare(ctx, shareID, s.now())
}

// ListMine returns shares created by the given user, newest first.
func (s *Service) ListMine(ctx context.Context, userID string) ([]*sqlite.Share, error) {
	return s.Store.ListSharesByUser(ctx, userID)
}

// ListAll returns every share in the system. Admin-only at the handler layer.
func (s *Service) ListAll(ctx context.Context) ([]*sqlite.Share, error) {
	return s.Store.ListAllShares(ctx)
}

// OpenTarget fetches the object body for a resolved share. rng forwards
// an HTTP Range to the backend so video / audio elements can stream
// without buffering the whole object. Pass nil for a full-body read.
func (s *Service) OpenTarget(ctx context.Context, sh *sqlite.Share, rng *backend.Range) (backend.ObjectReader, error) {
	b, ok := s.Backends.Get(sh.BackendID)
	if !ok {
		return nil, ErrBackendGone
	}
	return b.GetObject(ctx, sh.Bucket, sh.Key, "", rng)
}

// HeadTarget fetches metadata for a resolved share's object — used by the
// landing page to decide how to preview it without reading the body.
func (s *Service) HeadTarget(ctx context.Context, sh *sqlite.Share) (backend.ObjectInfo, error) {
	b, ok := s.Backends.Get(sh.BackendID)
	if !ok {
		return backend.ObjectInfo{}, ErrBackendGone
	}
	return b.HeadObject(ctx, sh.Bucket, sh.Key, "")
}

// newShareCode returns a cryptographically-random ~14-char URL-safe code.
// 10 bytes of entropy is enough — collisions before insertion are caught
// by the UNIQUE index anyway.
func newShareCode() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
