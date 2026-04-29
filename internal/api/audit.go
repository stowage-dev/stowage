// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"time"

	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// AuditDeps wires the audit viewer endpoint to the SQLite store.
type AuditDeps struct {
	Store *sqlite.Store
}

type auditEventDTO struct {
	ID        int64  `json:"id"`
	Timestamp string `json:"timestamp"`
	UserID    string `json:"user_id,omitempty"`
	Action    string `json:"action"`
	Backend   string `json:"backend,omitempty"`
	Bucket    string `json:"bucket,omitempty"`
	Key       string `json:"key,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	IP        string `json:"ip,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
	Status    string `json:"status"`
	Detail    string `json:"detail,omitempty"`
}

func toAuditDTO(e *sqlite.AuditEvent) auditEventDTO {
	return auditEventDTO{
		ID:        e.ID,
		Timestamp: e.Timestamp.UTC().Format(time.RFC3339),
		UserID:    e.UserID.String,
		Action:    e.Action,
		Backend:   e.BackendID.String,
		Bucket:    e.Bucket.String,
		Key:       e.ObjectKey.String,
		RequestID: e.RequestID.String,
		IP:        e.IP.String,
		UserAgent: e.UserAgent.String,
		Status:    e.Status,
		Detail:    e.DetailJSON.String,
	}
}

// parseAuditFilter reads filter values from the request's query string.
// Empty fields are ignored. Unknown values fall back to defaults rather
// than error so the URL stays forgiving.
func parseAuditFilter(r *http.Request) sqlite.AuditFilter {
	q := r.URL.Query()
	f := sqlite.AuditFilter{
		UserID:    q.Get("user"),
		Action:    q.Get("action"),
		BackendID: q.Get("backend"),
		Bucket:    q.Get("bucket"),
		Status:    q.Get("status"),
	}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Since = t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.Until = t
		}
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Offset = n
		}
	}
	return f
}

func (d *AuditDeps) handleListAudit(w http.ResponseWriter, r *http.Request) {
	if d.Store == nil {
		writeError(w, http.StatusNotImplemented, "not_supported", "audit log is not enabled", "")
		return
	}
	f := parseAuditFilter(r)
	events, err := d.Store.ListAuditEvents(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list failed", "")
		return
	}
	total, err := d.Store.CountAuditEvents(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "count failed", "")
		return
	}
	out := make([]auditEventDTO, 0, len(events))
	for _, e := range events {
		out = append(out, toAuditDTO(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"events": out,
		"total":  total,
	})
}

// handleAuditCSV streams the matching events as CSV. Same filters as the
// JSON endpoint; respects limit/offset so admins can chunk large exports.
func (d *AuditDeps) handleAuditCSV(w http.ResponseWriter, r *http.Request) {
	if d.Store == nil {
		writeError(w, http.StatusNotImplemented, "not_supported", "audit log is not enabled", "")
		return
	}
	f := parseAuditFilter(r)
	if f.Limit == 0 {
		f.Limit = 1000 // CSV default — bigger than the JSON page size.
	}
	events, err := d.Store.ListAuditEvents(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list failed", "")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition",
		`attachment; filename="audit-`+time.Now().UTC().Format("20060102-150405")+`.csv"`)
	w.Header().Set("Cache-Control", "no-store")

	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{
		"id", "timestamp", "user_id", "action", "backend", "bucket", "key",
		"request_id", "ip", "user_agent", "status", "detail",
	})
	for _, e := range events {
		_ = cw.Write([]string{
			strconv.FormatInt(e.ID, 10),
			e.Timestamp.UTC().Format(time.RFC3339),
			csvSafe(e.UserID.String),
			csvSafe(e.Action),
			csvSafe(e.BackendID.String),
			csvSafe(e.Bucket.String),
			csvSafe(e.ObjectKey.String),
			csvSafe(e.RequestID.String),
			csvSafe(e.IP.String),
			csvSafe(e.UserAgent.String),
			csvSafe(e.Status),
			csvSafe(e.DetailJSON.String),
		})
	}
}

// csvSafe defangs Excel/LibreOffice formula injection by prefixing any cell
// whose first byte would trigger formula evaluation with an apostrophe.
// Object keys, bucket names, and user-agents are attacker-influenced fields
// that flow into the audit export an admin is likely to open in a
// spreadsheet.
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}
