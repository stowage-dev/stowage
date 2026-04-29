// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// Pin endpoints live on AuthDeps because they're scoped to /api/me/* and
// the auth Service already owns the SQLite store handle.

type pinDTO struct {
	BackendID string `json:"backend_id"`
	Bucket    string `json:"bucket"`
	CreatedAt string `json:"created_at"`
}

func (d *AuthDeps) handleListPins(w http.ResponseWriter, r *http.Request) {
	id := auth.IdentityFrom(r.Context())
	if id == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required", "")
		return
	}
	rows, err := d.Service.Store.ListPinsByUser(r.Context(), id.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list pins failed", "")
		return
	}
	out := make([]pinDTO, 0, len(rows))
	for _, p := range rows {
		out = append(out, pinDTO{
			BackendID: p.BackendID,
			Bucket:    p.Bucket,
			CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"pins": out})
}

type createPinRequest struct {
	BackendID string `json:"backend_id"`
	Bucket    string `json:"bucket"`
}

func (d *AuthDeps) handleCreatePin(w http.ResponseWriter, r *http.Request) {
	id := auth.IdentityFrom(r.Context())
	if id == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required", "")
		return
	}
	var req createPinRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	if req.BackendID == "" || req.Bucket == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "backend_id and bucket are required", "")
		return
	}
	pin := &sqlite.BucketPin{
		UserID:    id.UserID,
		BackendID: req.BackendID,
		Bucket:    req.Bucket,
		CreatedAt: time.Now().UTC(),
	}
	if err := d.Service.Store.InsertPin(r.Context(), pin); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "create pin failed", "")
		return
	}
	writeJSON(w, http.StatusCreated, pinDTO{
		BackendID: pin.BackendID,
		Bucket:    pin.Bucket,
		CreatedAt: pin.CreatedAt.Format(time.RFC3339),
	})
}

func (d *AuthDeps) handleDeletePin(w http.ResponseWriter, r *http.Request) {
	id := auth.IdentityFrom(r.Context())
	if id == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required", "")
		return
	}
	bid := chi.URLParam(r, "bid")
	bucket := chi.URLParam(r, "bucket")
	if bid == "" || bucket == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "backend and bucket are required", "")
		return
	}
	if err := d.Service.Store.DeletePin(r.Context(), id.UserID, bid, bucket); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete pin failed", "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
