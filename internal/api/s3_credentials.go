// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stowage-dev/stowage/internal/audit"
	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/secrets"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// SourceReloader is the subset of s3proxy.SQLiteSource the admin handlers
// need: a Reload() that rebuilds the in-memory cache from the store. Kept
// as an interface so wiring code (and tests) can plug in a noop.
type SourceReloader interface {
	Reload(ctx context.Context) error
}

// S3CredentialDeps groups the collaborators the credential handlers need.
// Reloader is optional — when nil, mutations write to the DB but the
// in-memory cache won't refresh until the next scheduled tick.
type S3CredentialDeps struct {
	Store    *sqlite.Store
	Sealer   *secrets.Sealer
	Reloader SourceReloader
	Audit    audit.Recorder
	Logger   *slog.Logger
}

// s3CredDTO is the shape returned to the dashboard. SecretKey is populated
// only by the create response (server-generated, shown once).
type s3CredDTO struct {
	AccessKey   string   `json:"access_key"`
	SecretKey   string   `json:"secret_key,omitempty"`
	BackendID   string   `json:"backend_id"`
	Buckets     []string `json:"buckets"`
	UserID      string   `json:"user_id,omitempty"`
	Description string   `json:"description,omitempty"`
	Enabled     bool     `json:"enabled"`
	ExpiresAt   string   `json:"expires_at,omitempty"`
	CreatedAt   string   `json:"created_at"`
	CreatedBy   string   `json:"created_by,omitempty"`
	UpdatedAt   string   `json:"updated_at"`
	UpdatedBy   string   `json:"updated_by,omitempty"`
}

func credToDTO(c *sqlite.S3Credential) s3CredDTO {
	out := s3CredDTO{
		AccessKey:   c.AccessKey,
		BackendID:   c.BackendID,
		Description: c.Description,
		Enabled:     c.Enabled,
		CreatedAt:   c.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   c.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if c.UserID.Valid {
		out.UserID = c.UserID.String
	}
	if c.CreatedBy.Valid {
		out.CreatedBy = c.CreatedBy.String
	}
	if c.UpdatedBy.Valid {
		out.UpdatedBy = c.UpdatedBy.String
	}
	if c.ExpiresAt.Valid {
		out.ExpiresAt = c.ExpiresAt.Time.UTC().Format(time.RFC3339)
	}
	if buckets, err := c.UnmarshalBuckets(); err == nil {
		out.Buckets = buckets
	}
	return out
}

func (d *S3CredentialDeps) handleList(w http.ResponseWriter, r *http.Request) {
	if d.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "s3_proxy_disabled", "s3 proxy is not enabled", "")
		return
	}
	rows, err := d.Store.ListS3Credentials(r.Context())
	if err != nil {
		d.Logger.Warn("list s3 credentials", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "could not list credentials", "")
		return
	}
	out := make([]s3CredDTO, 0, len(rows))
	for _, c := range rows {
		out = append(out, credToDTO(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": out})
}

type createS3CredentialRequest struct {
	BackendID   string   `json:"backend_id"`
	Buckets     []string `json:"buckets"`
	UserID      string   `json:"user_id,omitempty"`
	Description string   `json:"description,omitempty"`
	ExpiresAt   string   `json:"expires_at,omitempty"`
}

func (d *S3CredentialDeps) handleCreate(w http.ResponseWriter, r *http.Request) {
	if d.Sealer == nil {
		writeError(w, http.StatusServiceUnavailable, "secret_key_unset",
			"server has no sealing key; set STOWAGE_SECRET_KEY or server.secret_key_file", "")
		return
	}
	id := auth.IdentityFrom(r.Context())
	if id == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required", "")
		return
	}

	var req createS3CredentialRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	if req.BackendID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "backend_id is required", "")
		return
	}
	if len(req.Buckets) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "buckets must list at least one bucket", "")
		return
	}

	akid, err := generateAccessKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not generate access key", "")
		return
	}
	secret, err := generateSecretKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not generate secret key", "")
		return
	}
	enc, err := d.Sealer.Seal([]byte(secret))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not seal secret", "")
		return
	}
	now := time.Now().UTC()
	c := &sqlite.S3Credential{
		AccessKey:    akid,
		SecretKeyEnc: enc,
		BackendID:    req.BackendID,
		Description:  req.Description,
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
		CreatedBy:    sql.NullString{String: id.UserID, Valid: id.UserID != ""},
		UpdatedBy:    sql.NullString{String: id.UserID, Valid: id.UserID != ""},
	}
	if req.UserID != "" {
		c.UserID = sql.NullString{String: req.UserID, Valid: true}
	}
	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "expires_at must be RFC3339", "")
			return
		}
		c.ExpiresAt = sql.NullTime{Time: t.UTC(), Valid: true}
	}
	if err := c.MarshalBuckets(req.Buckets); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), "")
		return
	}

	if err := d.Store.CreateS3Credential(r.Context(), c); err != nil {
		if errors.Is(err, sqlite.ErrS3AccessKeyTaken) {
			writeError(w, http.StatusConflict, "conflict", "access key already exists (very unlikely — retry)", "")
			return
		}
		d.Logger.Warn("create s3 credential", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "could not create credential", "")
		return
	}
	d.fireReload(r.Context())

	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "s3_credential.create",
		Backend: req.BackendID,
		Detail: map[string]any{
			"access_key":  akid,
			"buckets":     req.Buckets,
			"description": req.Description,
		},
	})

	dto := credToDTO(c)
	dto.SecretKey = secret // visible only on the create response
	writeJSON(w, http.StatusCreated, dto)
}

type patchS3CredentialRequest struct {
	Buckets     *[]string `json:"buckets,omitempty"`
	Description *string   `json:"description,omitempty"`
	Enabled     *bool     `json:"enabled,omitempty"`
	ExpiresAt   *string   `json:"expires_at,omitempty"`
}

func (d *S3CredentialDeps) handlePatch(w http.ResponseWriter, r *http.Request) {
	id := auth.IdentityFrom(r.Context())
	if id == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required", "")
		return
	}
	akid := chi.URLParam(r, "akid")
	if akid == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "access key required", "")
		return
	}

	var req patchS3CredentialRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}

	patch := sqlite.S3CredentialPatch{
		UpdatedBy: sql.NullString{String: id.UserID, Valid: id.UserID != ""},
		UpdatedAt: time.Now().UTC(),
	}
	if req.Buckets != nil {
		raw, err := json.Marshal(*req.Buckets)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "buckets not encodable", "")
			return
		}
		s := string(raw)
		patch.Buckets = &s
	}
	if req.Description != nil {
		patch.Description = req.Description
	}
	if req.Enabled != nil {
		patch.Enabled = req.Enabled
	}
	if req.ExpiresAt != nil {
		if *req.ExpiresAt == "" {
			patch.ExpiresAt = &sql.NullTime{Valid: false}
		} else {
			t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", "expires_at must be RFC3339 or empty string", "")
				return
			}
			patch.ExpiresAt = &sql.NullTime{Time: t.UTC(), Valid: true}
		}
	}

	if err := d.Store.UpdateS3Credential(r.Context(), akid, patch); err != nil {
		if errors.Is(err, sqlite.ErrS3CredentialNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "credential not found", "")
			return
		}
		d.Logger.Warn("update s3 credential", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "could not update credential", "")
		return
	}
	d.fireReload(r.Context())

	audit.RecordRequest(d.Audit, r, audit.Event{
		Action: "s3_credential.update",
		Detail: map[string]any{"access_key": akid},
	})

	row, err := d.Store.GetS3Credential(r.Context(), akid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not reload credential", "")
		return
	}
	writeJSON(w, http.StatusOK, credToDTO(row))
}

func (d *S3CredentialDeps) handleDelete(w http.ResponseWriter, r *http.Request) {
	akid := chi.URLParam(r, "akid")
	if akid == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "access key required", "")
		return
	}
	if err := d.Store.DeleteS3Credential(r.Context(), akid); err != nil {
		if errors.Is(err, sqlite.ErrS3CredentialNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "credential not found", "")
			return
		}
		d.Logger.Warn("delete s3 credential", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "could not delete credential", "")
		return
	}
	d.fireReload(r.Context())

	audit.RecordRequest(d.Audit, r, audit.Event{
		Action: "s3_credential.delete",
		Detail: map[string]any{"access_key": akid},
	})
	w.WriteHeader(http.StatusNoContent)
}

func (d *S3CredentialDeps) fireReload(ctx context.Context) {
	if d.Reloader == nil {
		return
	}
	if err := d.Reloader.Reload(context.Background()); err != nil {
		d.Logger.Warn("s3 credential reload after CRUD failed", "err", err.Error())
	}
	_ = ctx // request context is discarded — reload runs to completion
}

// generateAccessKey returns a 20-char "AKIA" + 16 base32-ish chars value
// that resembles AWS's access-key shape. Bytewise random with collision
// probability ≈ 2^-80 over 1e6 keys — overwhelmingly safe; the database
// PRIMARY KEY constraint catches the (theoretical) duplicate anyway.
func generateAccessKey() (string, error) {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567" // base32, AWS-style alphabet
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	out := make([]byte, 20)
	copy(out, "AKIA")
	for i, b := range buf {
		out[4+i] = chars[int(b)%len(chars)]
	}
	return string(out), nil
}

// generateSecretKey returns 40 hex chars (160 bits of entropy). AWS uses
// base64 with a longer length, but hex is unambiguous in JSON / shell
// quoting and sufficient for HMAC-SHA256.
func generateSecretKey() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
