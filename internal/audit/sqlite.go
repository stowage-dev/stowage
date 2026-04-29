// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// SQLiteRecorder is the production audit recorder. Writes synchronously —
// SQLite WAL mode handles the per-event load fine and synchronous writes
// keep the audit trail consistent with the response. Should profiling ever
// surface this as a hot path, swap in a buffered async wrapper.
type SQLiteRecorder struct {
	Store   *sqlite.Store
	Logger  *slog.Logger
	Proxies *auth.ProxyTrust // optional; nil = trust no proxy headers
}

func NewSQLiteRecorder(store *sqlite.Store, logger *slog.Logger, proxies *auth.ProxyTrust) *SQLiteRecorder {
	if logger == nil {
		logger = slog.Default()
	}
	return &SQLiteRecorder{Store: store, Logger: logger, Proxies: proxies}
}

// Record persists the event. Failures are logged but do not propagate —
// the audit trail must never block real user-visible behaviour.
func (r *SQLiteRecorder) Record(ctx context.Context, e Event) error {
	row := eventToRow(e)
	if err := r.Store.InsertAuditEvent(ctx, row); err != nil {
		r.Logger.Warn("audit insert failed",
			"action", e.Action, "err", err.Error())
		return err
	}
	return nil
}

// RecordBatch persists several events in one transaction. Implements
// audit.BatchRecorder so AsyncRecorder can flush an opportunistic burst
// in a single SQLite round-trip — the per-event Record path was ~12% of
// proxy CPU at 700 rps under bench load before this was wired in.
func (r *SQLiteRecorder) RecordBatch(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	rows := make([]*sqlite.AuditEvent, len(events))
	for i, e := range events {
		rows[i] = eventToRow(e)
	}
	if err := r.Store.InsertAuditEvents(ctx, rows); err != nil {
		r.Logger.Warn("audit batch insert failed",
			"n", len(rows), "err", err.Error())
		return err
	}
	return nil
}

// eventToRow turns the wire-shape Event into the persistence-shape row.
// Centralised so Record + RecordBatch can't drift on null-coercion or
// timestamp/status defaulting.
func eventToRow(e Event) *sqlite.AuditEvent {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	if e.Status == "" {
		e.Status = "ok"
	}
	row := &sqlite.AuditEvent{
		Timestamp: e.Timestamp.UTC(),
		Action:    e.Action,
		Status:    e.Status,
	}
	row.UserID = nullIf(e.UserID)
	row.BackendID = nullIf(e.Backend)
	row.Bucket = nullIf(e.Bucket)
	row.ObjectKey = nullIf(e.Key)
	row.RequestID = nullIf(e.RequestID)
	row.IP = nullIf(e.IP)
	row.UserAgent = nullIf(e.UserAgent)
	if len(e.Detail) > 0 {
		if b, err := json.Marshal(e.Detail); err == nil {
			row.DetailJSON = sql.NullString{String: string(b), Valid: true}
		}
	}
	return row
}

func nullIf(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// FromRequest pre-fills an Event with the bits derivable from the HTTP
// request: identity, request-id, IP, user-agent. Call sites supply the
// Action / Backend / Bucket / Key / Detail / Status they actually know.
//
// Untrusted-peer note: the package-level helper does NOT honour proxy
// headers. RecordRequest uses the recorder's ProxyTrust when one is wired,
// which is what handlers should use.
func FromRequest(r *http.Request) Event {
	e := Event{
		RequestID: chimw.GetReqID(r.Context()),
		UserAgent: r.UserAgent(),
		IP:        clientIP(r),
	}
	if id := auth.IdentityFrom(r.Context()); id != nil {
		e.UserID = id.UserID
	}
	return e
}

// RecordRequest is the convenient call-site shape for handlers. Nil-safe:
// when rec is nil (tests, scaffolds without a wired recorder) it does
// nothing. Fills in user / request-id / IP / user-agent from r when the
// caller didn't pre-populate them.
func RecordRequest(rec Recorder, r *http.Request, e Event) {
	if rec == nil {
		return
	}
	if e.RequestID == "" {
		e.RequestID = chimw.GetReqID(r.Context())
	}
	if e.UserAgent == "" {
		e.UserAgent = r.UserAgent()
	}
	if e.IP == "" {
		// Use the recorder's trust list when available so audit IPs
		// reflect proxy-aware attribution. Unwrap an AsyncRecorder if
		// present so the proxy trust on the underlying SQLiteRecorder
		// is still reachable.
		if sr := unwrapSQLite(rec); sr != nil && sr.Proxies != nil {
			e.IP = sr.Proxies.ClientIP(r)
		} else {
			e.IP = clientIP(r)
		}
	}
	if e.UserID == "" {
		if id := auth.IdentityFrom(r.Context()); id != nil {
			e.UserID = id.UserID
		}
	}
	_ = rec.Record(r.Context(), e)
}

// clientIP extracts the request's source address without honouring any
// proxy headers — that path runs through ProxyTrust.ClientIP in the
// recorder. Kept as the trust-free fallback for legacy callers that
// haven't been wired up yet.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// unwrapSQLite returns the underlying SQLiteRecorder when rec is one
// directly or is an AsyncRecorder around one. nil otherwise — call sites
// must guard before using the result.
func unwrapSQLite(rec Recorder) *SQLiteRecorder {
	switch r := rec.(type) {
	case *SQLiteRecorder:
		return r
	case *AsyncRecorder:
		if inner, ok := r.Inner().(*SQLiteRecorder); ok {
			return inner
		}
	}
	return nil
}
