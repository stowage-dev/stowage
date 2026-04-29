// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stowage-dev/stowage/internal/audit"
	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// S3AnonymousDeps groups the collaborators the anonymous-binding handlers
// need. Reloader fires the same SQLiteSource.Reload as the credential
// handlers — the binding cache lives in the same source.
type S3AnonymousDeps struct {
	Store    *sqlite.Store
	Reloader SourceReloader
	Audit    audit.Recorder
	Logger   *slog.Logger
}

type s3AnonDTO struct {
	BackendID      string `json:"backend_id"`
	Bucket         string `json:"bucket"`
	Mode           string `json:"mode"`
	PerSourceIPRPS int    `json:"per_source_ip_rps"`
	CreatedAt      string `json:"created_at"`
	CreatedBy      string `json:"created_by,omitempty"`
}

func anonToDTO(b *sqlite.S3AnonymousBinding) s3AnonDTO {
	out := s3AnonDTO{
		BackendID:      b.BackendID,
		Bucket:         b.Bucket,
		Mode:           b.Mode,
		PerSourceIPRPS: b.PerSourceIPRPS,
		CreatedAt:      b.CreatedAt.UTC().Format(time.RFC3339),
	}
	if b.CreatedBy.Valid {
		out.CreatedBy = b.CreatedBy.String
	}
	return out
}

func (d *S3AnonymousDeps) handleList(w http.ResponseWriter, r *http.Request) {
	rows, err := d.Store.ListS3AnonymousBindings(r.Context())
	if err != nil {
		d.Logger.Warn("list s3 anonymous bindings", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "could not list bindings", "")
		return
	}
	out := make([]s3AnonDTO, 0, len(rows))
	for _, b := range rows {
		out = append(out, anonToDTO(b))
	}
	writeJSON(w, http.StatusOK, map[string]any{"bindings": out})
}

type upsertS3AnonRequest struct {
	BackendID      string `json:"backend_id"`
	Bucket         string `json:"bucket"`
	Mode           string `json:"mode,omitempty"`
	PerSourceIPRPS *int   `json:"per_source_ip_rps,omitempty"`
}

func (d *S3AnonymousDeps) handleUpsert(w http.ResponseWriter, r *http.Request) {
	id := auth.IdentityFrom(r.Context())
	if id == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required", "")
		return
	}
	var req upsertS3AnonRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	if req.BackendID == "" || req.Bucket == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "backend_id and bucket are required", "")
		return
	}
	mode := req.Mode
	if mode == "" {
		mode = "ReadOnly"
	}
	if mode != "ReadOnly" {
		writeError(w, http.StatusBadRequest, "invalid_request",
			"only ReadOnly is supported in v1", "")
		return
	}
	rps := 20
	if req.PerSourceIPRPS != nil {
		if *req.PerSourceIPRPS < 0 {
			writeError(w, http.StatusBadRequest, "invalid_request",
				"per_source_ip_rps must be non-negative", "")
			return
		}
		rps = *req.PerSourceIPRPS
	}

	binding := &sqlite.S3AnonymousBinding{
		BackendID:      req.BackendID,
		Bucket:         req.Bucket,
		Mode:           mode,
		PerSourceIPRPS: rps,
		CreatedAt:      time.Now().UTC(),
		CreatedBy:      sql.NullString{String: id.UserID, Valid: id.UserID != ""},
	}
	if err := d.Store.UpsertS3AnonymousBinding(r.Context(), binding); err != nil {
		d.Logger.Warn("upsert s3 anonymous binding", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "could not save binding", "")
		return
	}
	if d.Reloader != nil {
		if err := d.Reloader.Reload(r.Context()); err != nil {
			d.Logger.Warn("s3 source reload after anonymous upsert failed", "err", err.Error())
		}
	}

	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "s3_anonymous.upsert",
		Backend: req.BackendID,
		Bucket:  req.Bucket,
		Detail: map[string]any{
			"mode":              mode,
			"per_source_ip_rps": rps,
		},
	})
	writeJSON(w, http.StatusOK, anonToDTO(binding))
}

func (d *S3AnonymousDeps) handleDelete(w http.ResponseWriter, r *http.Request) {
	bid := chi.URLParam(r, "bid")
	bucket := chi.URLParam(r, "bucket")
	if bid == "" || bucket == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "backend and bucket required", "")
		return
	}
	if err := d.Store.DeleteS3AnonymousBinding(r.Context(), bid, bucket); err != nil {
		if errors.Is(err, sqlite.ErrS3AnonymousBindingNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "binding not found", "")
			return
		}
		d.Logger.Warn("delete s3 anonymous binding", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "could not delete binding", "")
		return
	}
	if d.Reloader != nil {
		if err := d.Reloader.Reload(r.Context()); err != nil {
			d.Logger.Warn("s3 source reload after anonymous delete failed", "err", err.Error())
		}
	}
	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "s3_anonymous.delete",
		Backend: bid,
		Bucket:  bucket,
	})
	w.WriteHeader(http.StatusNoContent)
}
