// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stowage-dev/stowage/internal/audit"
	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// handleMyList returns every SQLite-managed credential whose user_id matches
// the calling identity. Operator-managed (Kubernetes) credentials are not
// included — those are tied to BucketClaims, not stowage user accounts.
func (d *S3CredentialDeps) handleMyList(w http.ResponseWriter, r *http.Request) {
	if d.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "s3_proxy_disabled", "s3 proxy is not enabled", "")
		return
	}
	id := auth.IdentityFrom(r.Context())
	if id == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required", "")
		return
	}
	rows, err := d.Store.ListS3Credentials(r.Context())
	if err != nil {
		d.Logger.Warn("list s3 credentials (me)", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "could not list credentials", "")
		return
	}
	out := make([]s3CredDTO, 0, len(rows))
	for _, c := range rows {
		if !c.UserID.Valid || c.UserID.String != id.UserID {
			continue
		}
		out = append(out, credToDTO(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"credentials": out})
}

// myCreateRequest mirrors createS3CredentialRequest but drops the UserID
// field — self-service callers can only mint credentials for themselves.
type myCreateRequest struct {
	BackendID   string   `json:"backend_id"`
	Buckets     []string `json:"buckets"`
	Description string   `json:"description,omitempty"`
	ExpiresAt   string   `json:"expires_at,omitempty"`
}

func (d *S3CredentialDeps) handleMyCreate(w http.ResponseWriter, r *http.Request) {
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

	var req myCreateRequest
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
		UserID:       sql.NullString{String: id.UserID, Valid: id.UserID != ""},
		CreatedBy:    sql.NullString{String: id.UserID, Valid: id.UserID != ""},
		UpdatedBy:    sql.NullString{String: id.UserID, Valid: id.UserID != ""},
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
		d.Logger.Warn("create s3 credential (me)", "err", err.Error())
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
			"self":        true,
		},
	})

	dto := credToDTO(c)
	dto.SecretKey = secret
	writeJSON(w, http.StatusCreated, dto)
}

// myPatchRequest is a subset of patchS3CredentialRequest — buckets/description
// /enabled/expires only. Users cannot reassign ownership.
type myPatchRequest struct {
	Buckets     *[]string `json:"buckets,omitempty"`
	Description *string   `json:"description,omitempty"`
	Enabled     *bool     `json:"enabled,omitempty"`
	ExpiresAt   *string   `json:"expires_at,omitempty"`
}

func (d *S3CredentialDeps) handleMyPatch(w http.ResponseWriter, r *http.Request) {
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
	if !d.ownsCredential(r.Context(), w, akid, id.UserID) {
		return
	}

	var req myPatchRequest
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
		d.Logger.Warn("update s3 credential (me)", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "could not update credential", "")
		return
	}
	d.fireReload(r.Context())

	audit.RecordRequest(d.Audit, r, audit.Event{
		Action: "s3_credential.update",
		Detail: map[string]any{"access_key": akid, "self": true},
	})

	row, err := d.Store.GetS3Credential(r.Context(), akid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not reload credential", "")
		return
	}
	writeJSON(w, http.StatusOK, credToDTO(row))
}

func (d *S3CredentialDeps) handleMyDelete(w http.ResponseWriter, r *http.Request) {
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
	if !d.ownsCredential(r.Context(), w, akid, id.UserID) {
		return
	}
	if err := d.Store.DeleteS3Credential(r.Context(), akid); err != nil {
		if errors.Is(err, sqlite.ErrS3CredentialNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "credential not found", "")
			return
		}
		d.Logger.Warn("delete s3 credential (me)", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "could not delete credential", "")
		return
	}
	d.fireReload(r.Context())

	audit.RecordRequest(d.Audit, r, audit.Event{
		Action: "s3_credential.delete",
		Detail: map[string]any{"access_key": akid, "self": true},
	})
	w.WriteHeader(http.StatusNoContent)
}

// ownsCredential gates self-service mutations. Returns true when the
// credential exists AND the calling user owns it; otherwise it has already
// written the appropriate error response and the caller should bail.
func (d *S3CredentialDeps) ownsCredential(ctx context.Context, w http.ResponseWriter, akid, userID string) bool {
	row, err := d.Store.GetS3Credential(ctx, akid)
	if err != nil {
		if errors.Is(err, sqlite.ErrS3CredentialNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "credential not found", "")
			return false
		}
		d.Logger.Warn("get s3 credential (me)", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "could not load credential", "")
		return false
	}
	if !row.UserID.Valid || row.UserID.String != userID {
		// 404 rather than 403 so the existence of credentials owned by
		// other users isn't disclosed to the caller.
		writeError(w, http.StatusNotFound, "not_found", "credential not found", "")
		return false
	}
	return true
}
