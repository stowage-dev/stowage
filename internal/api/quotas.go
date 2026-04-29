// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stowage-dev/stowage/internal/audit"
	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/quotas"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// quotaDTO is the shape clients see — combines persisted config with
// (best-effort) current usage. Both halves may be missing.
type quotaDTO struct {
	Configured  bool   `json:"configured"`
	SoftBytes   int64  `json:"soft_bytes,omitempty"`
	HardBytes   int64  `json:"hard_bytes,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	UpdatedBy   string `json:"updated_by,omitempty"`
	UsageBytes  int64  `json:"usage_bytes,omitempty"`
	ObjectCount int64  `json:"object_count,omitempty"`
	ComputedAt  string `json:"computed_at,omitempty"`
	HasUsage    bool   `json:"has_usage"`
}

func (d *BackendDeps) handleGetQuota(w http.ResponseWriter, r *http.Request) {
	if d.Quotas == nil {
		writeError(w, http.StatusNotImplemented, "not_supported", "quotas are not enabled", "")
		return
	}
	bid := chi.URLParam(r, "bid")
	bucket := chi.URLParam(r, "bucket")
	if _, ok := d.Registry.Get(bid); !ok {
		writeError(w, http.StatusNotFound, "not_found", "backend not found", "")
		return
	}

	st, err := d.Quotas.Status(r.Context(), bid, bucket)
	if err != nil {
		d.Logger.Warn("quota status failed", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "could not load quota", "")
		return
	}
	writeJSON(w, http.StatusOK, quotaToDTO(st.Limit, st.Usage))
}

type putQuotaRequest struct {
	SoftBytes int64 `json:"soft_bytes"`
	HardBytes int64 `json:"hard_bytes"`
}

func (d *BackendDeps) handlePutQuota(w http.ResponseWriter, r *http.Request) {
	if d.Quotas == nil {
		writeError(w, http.StatusNotImplemented, "not_supported", "quotas are not enabled", "")
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

	var req putQuotaRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	if req.SoftBytes < 0 || req.HardBytes < 0 {
		writeError(w, http.StatusBadRequest, "invalid_quota", "byte values must be non-negative", "")
		return
	}
	if req.SoftBytes == 0 && req.HardBytes == 0 {
		writeError(w, http.StatusBadRequest, "invalid_quota",
			"set at least one of soft_bytes or hard_bytes; DELETE the resource to clear", "")
		return
	}
	// Soft must be ≤ hard when both set, otherwise the soft warning never
	// fires before the hard rejection.
	if req.SoftBytes > 0 && req.HardBytes > 0 && req.SoftBytes > req.HardBytes {
		writeError(w, http.StatusBadRequest, "invalid_quota",
			"soft_bytes must be less than or equal to hard_bytes", "")
		return
	}

	q := &sqlite.BucketQuota{
		BackendID: bid,
		Bucket:    bucket,
		SoftBytes: req.SoftBytes,
		HardBytes: req.HardBytes,
		UpdatedAt: time.Now().UTC(),
		UpdatedBy: id.UserID,
	}
	if err := d.Quotas.Store.UpsertQuota(r.Context(), q); err != nil {
		d.Logger.Warn("quota upsert failed", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "could not save quota", "")
		return
	}
	// Refresh the in-memory limit cache so the next CheckUpload sees the
	// new value without waiting for the scheduled ticker. Use a fresh
	// context: the request may already be on its way to completion.
	if err := d.Quotas.ReloadLimits(context.Background()); err != nil {
		d.Logger.Warn("quota reload after upsert failed", "err", err.Error())
	}
	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "quota.set",
		Backend: bid,
		Bucket:  bucket,
		Detail:  map[string]any{"soft_bytes": req.SoftBytes, "hard_bytes": req.HardBytes},
	})

	usage := d.Quotas.Get(bid, bucket)
	writeJSON(w, http.StatusOK, quotaToDTO(quotaFromRow(q), usage))
}

func (d *BackendDeps) handleDeleteQuota(w http.ResponseWriter, r *http.Request) {
	if d.Quotas == nil {
		writeError(w, http.StatusNotImplemented, "not_supported", "quotas are not enabled", "")
		return
	}
	bid := chi.URLParam(r, "bid")
	bucket := chi.URLParam(r, "bucket")
	if err := d.Quotas.Store.DeleteQuota(r.Context(), bid, bucket); err != nil {
		d.Logger.Warn("quota delete failed", "err", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "could not delete quota", "")
		return
	}
	if err := d.Quotas.ReloadLimits(context.Background()); err != nil {
		d.Logger.Warn("quota reload after delete failed", "err", err.Error())
	}
	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "quota.delete",
		Backend: bid,
		Bucket:  bucket,
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleRecomputeQuota forces a fresh scan ignoring the schedule. Useful
// after bulk deletes or when an admin wants an up-to-date number for a
// purchase / capacity decision.
func (d *BackendDeps) handleRecomputeQuota(w http.ResponseWriter, r *http.Request) {
	if d.Quotas == nil {
		writeError(w, http.StatusNotImplemented, "not_supported", "quotas are not enabled", "")
		return
	}
	bid := chi.URLParam(r, "bid")
	bucket := chi.URLParam(r, "bucket")
	if _, ok := d.Registry.Get(bid); !ok {
		writeError(w, http.StatusNotFound, "not_found", "backend not found", "")
		return
	}
	usage, err := d.Quotas.Scan(r.Context(), bid, bucket)
	if err != nil {
		d.Logger.Warn("quota scan failed", "err", err.Error())
		writeError(w, http.StatusBadGateway, "backend_error", "scan failed", "")
		return
	}
	// Tolerate "no quota row" — admin might be probing usage without a
	// formal quota set.
	limit, _ := d.Quotas.Limits.Get(bid, bucket)
	writeJSON(w, http.StatusOK, quotaToDTO(limit, usage))
}

func quotaToDTO(l *quotas.Limit, u *quotas.Usage) quotaDTO {
	out := quotaDTO{}
	if l != nil {
		out.Configured = true
		out.SoftBytes = l.SoftBytes
		out.HardBytes = l.HardBytes
		out.UpdatedAt = l.UpdatedAt.UTC().Format(time.RFC3339)
		out.UpdatedBy = l.UpdatedBy
	}
	if u != nil {
		out.HasUsage = true
		out.UsageBytes = u.Bytes
		out.ObjectCount = u.ObjectCount
		out.ComputedAt = u.ComputedAt.UTC().Format(time.RFC3339)
	}
	return out
}

// quotaFromRow converts a freshly-upserted row into a Limit so the upsert
// handler can return the stored shape without going through Limits.Get
// (which would race with ReloadLimits on slow systems).
func quotaFromRow(q *sqlite.BucketQuota) *quotas.Limit {
	if q == nil {
		return nil
	}
	return &quotas.Limit{
		SoftBytes: q.SoftBytes,
		HardBytes: q.HardBytes,
		UpdatedAt: q.UpdatedAt,
		UpdatedBy: q.UpdatedBy,
		Source:    "sqlite",
	}
}
