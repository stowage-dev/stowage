// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stowage-dev/stowage/internal/audit"
	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/sizes"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// ---- Per-bucket toggle --------------------------------------------------

type sizeTrackingDTO struct {
	Enabled   bool   `json:"enabled"`
	UpdatedAt string `json:"updated_at,omitempty"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

func (d *BackendDeps) handleGetBucketSizeTracking(w http.ResponseWriter, r *http.Request) {
	if d.Sizes == nil || d.Sizes.Store == nil {
		writeError(w, http.StatusNotImplemented, "not_supported", "size tracking is not enabled", "")
		return
	}
	bid := chi.URLParam(r, "bid")
	bucket := chi.URLParam(r, "bucket")
	if _, ok := d.Registry.Get(bid); !ok {
		writeError(w, http.StatusNotFound, "not_found", "backend not found", "")
		return
	}
	enabled, err := d.Sizes.Store.IsBucketSizeTracked(r.Context(), bid, bucket)
	if err != nil {
		d.Logger.Warn("size tracking lookup failed", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "could not load setting", "")
		return
	}
	writeJSON(w, http.StatusOK, sizeTrackingDTO{Enabled: enabled})
}

func (d *BackendDeps) handlePutBucketSizeTracking(w http.ResponseWriter, r *http.Request) {
	if d.Sizes == nil || d.Sizes.Store == nil {
		writeError(w, http.StatusNotImplemented, "not_supported", "size tracking is not enabled", "")
		return
	}
	id := auth.IdentityFrom(r.Context())
	if id == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required", "")
		return
	}
	bid := chi.URLParam(r, "bid")
	bucket := chi.URLParam(r, "bucket")
	if _, ok := d.Registry.Get(bid); !ok {
		writeError(w, http.StatusNotFound, "not_found", "backend not found", "")
		return
	}

	var req sizeTrackingDTO
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}

	row := &sqlite.BucketSizeTracking{
		BackendID: bid,
		Bucket:    bucket,
		Enabled:   req.Enabled,
		UpdatedAt: time.Now().UTC(),
		UpdatedBy: id.UserID,
	}
	if err := d.Sizes.Store.SetBucketSizeTracking(r.Context(), row); err != nil {
		d.Logger.Warn("size tracking upsert failed", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "could not save setting", "")
		return
	}
	// Drop cached entries when the admin disables tracking — the listing
	// should stop showing a stale figure right away.
	if !req.Enabled {
		d.Sizes.Forget(bid, bucket)
	}
	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "bucket.size_tracking.set",
		Backend: bid,
		Bucket:  bucket,
		Detail:  map[string]any{"enabled": req.Enabled},
	})
	writeJSON(w, http.StatusOK, sizeTrackingDTO{
		Enabled:   req.Enabled,
		UpdatedAt: row.UpdatedAt.Format(time.RFC3339),
		UpdatedBy: row.UpdatedBy,
	})
}

// ---- Prefix size --------------------------------------------------------

type prefixSizeDTO struct {
	Bytes      int64  `json:"bytes"`
	Count      int64  `json:"count"`
	Prefix     string `json:"prefix"`
	ComputedAt string `json:"computed_at"`
}

// handlePrefixSize returns the recursive byte total under prefix. The
// prefix may be empty (whole bucket). The result is served from the
// 60s TTL cache when fresh, otherwise computed live.
func (d *BackendDeps) handlePrefixSize(w http.ResponseWriter, r *http.Request) {
	if d.Sizes == nil {
		writeError(w, http.StatusNotImplemented, "not_supported", "size tracking is not enabled", "")
		return
	}
	bid := chi.URLParam(r, "bid")
	bucket := chi.URLParam(r, "bucket")
	if _, ok := d.Registry.Get(bid); !ok {
		writeError(w, http.StatusNotFound, "not_found", "backend not found", "")
		return
	}
	prefix := r.URL.Query().Get("prefix")
	// Reject paths that escape the bucket. validObjectKey is overkill for
	// a prefix (which may be empty or end with /), but the rune-level
	// rejections — control chars, "..", null — are exactly what we want.
	if prefix != "" && !validObjectKey(strings.TrimSuffix(prefix, "/")+"/x") {
		writeError(w, http.StatusBadRequest, "invalid_prefix", "prefix is invalid", "")
		return
	}

	bytes, count, err := d.Sizes.PrefixSize(r.Context(), bid, bucket, prefix)
	if err != nil {
		if errors.Is(err, sizes.ErrTrackingDisabled) {
			writeError(w, http.StatusConflict, "size_tracking_disabled",
				"size tracking is disabled for this bucket", "")
			return
		}
		d.backendError(w, r, err, "prefix size")
		return
	}
	writeJSON(w, http.StatusOK, prefixSizeDTO{
		Bytes:      bytes,
		Count:      count,
		Prefix:     prefix,
		ComputedAt: time.Now().UTC().Format(time.RFC3339),
	})
}
