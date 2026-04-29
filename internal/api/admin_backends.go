// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stowage-dev/stowage/internal/audit"
	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/backend"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// adminBackendDTO is what /api/admin/backends serves. It exposes more than
// the public backendDTO (endpoint, region, source, …) but never returns the
// stored secret — only a flag indicating whether one is set.
type adminBackendDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Endpoint    string `json:"endpoint"`
	Region      string `json:"region"`
	PathStyle   bool   `json:"path_style"`
	AccessKey   string `json:"access_key,omitempty"`
	SecretSet   bool   `json:"secret_set"`
	Enabled     bool   `json:"enabled"`
	Source      string `json:"source"`
	Healthy     bool   `json:"healthy"`
	LastError   string `json:"last_error,omitempty"`
	LastProbeAt string `json:"last_probe_at,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

// idRegexp constrains backend ids to a slug shape so they're URL-safe and
// stable across YAML / DB / log lines.
var idRegexp = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

func (d *BackendDeps) handleAdminListBackends(w http.ResponseWriter, r *http.Request) {
	// Index DB rows up front so we can adorn registry entries with their
	// stored metadata (endpoint, region, …) and detect the disabled rows
	// that aren't in the registry at all.
	rowsByID := map[string]*sqlite.Backend{}
	if d.Store != nil {
		rows, err := d.Store.ListBackends(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not list endpoints", "")
			return
		}
		for _, row := range rows {
			rowsByID[row.ID] = row
		}
	}

	out := make([]adminBackendDTO, 0)
	seen := map[string]struct{}{}
	for _, e := range d.Registry.List() {
		id := e.Backend.ID()
		out = append(out, fromRegistryEntry(e, rowsByID[id]))
		seen[id] = struct{}{}
	}
	for id, row := range rowsByID {
		if _, ok := seen[id]; ok {
			continue
		}
		out = append(out, fromStoredRow(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"backends": out})
}

func fromRegistryEntry(e backend.Entry, row *sqlite.Backend) adminBackendDTO {
	dto := adminBackendDTO{
		ID:        e.Backend.ID(),
		Name:      e.Backend.DisplayName(),
		Source:    string(e.Source),
		Healthy:   e.Status.Healthy,
		LastError: e.Status.LastError,
		Enabled:   true, // anything in the registry is enabled by definition
	}
	if !e.Status.LastProbeAt.IsZero() {
		dto.LastProbeAt = e.Status.LastProbeAt.Format(time.RFC3339)
	}
	if row != nil {
		dto.Type = row.Type
		dto.Endpoint = row.Endpoint
		dto.Region = row.Region
		dto.PathStyle = row.PathStyle
		dto.AccessKey = row.AccessKey
		dto.SecretSet = len(row.SecretKeyEnc) > 0
		dto.CreatedAt = row.CreatedAt.UTC().Format(time.RFC3339)
		dto.UpdatedAt = row.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return dto
}

func fromStoredRow(row *sqlite.Backend) adminBackendDTO {
	dto := adminBackendDTO{
		ID:        row.ID,
		Name:      row.Name,
		Type:      row.Type,
		Endpoint:  row.Endpoint,
		Region:    row.Region,
		PathStyle: row.PathStyle,
		AccessKey: row.AccessKey,
		SecretSet: len(row.SecretKeyEnc) > 0,
		Enabled:   row.Enabled,
		Source:    string(backend.SourceDB),
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: row.UpdatedAt.UTC().Format(time.RFC3339),
	}
	return dto
}

func (d *BackendDeps) handleAdminGetBackend(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "bid")
	var row *sqlite.Backend
	if d.Store != nil {
		got, err := d.Store.GetBackend(r.Context(), id)
		if err != nil && !errors.Is(err, sqlite.ErrBackendNotFound) {
			writeError(w, http.StatusInternalServerError, "internal", "could not load endpoint", "")
			return
		}
		row = got
	}
	if e, ok := d.lookupEntry(id); ok {
		writeJSON(w, http.StatusOK, fromRegistryEntry(e, row))
		return
	}
	if row == nil {
		writeError(w, http.StatusNotFound, "not_found", "backend not found", "")
		return
	}
	writeJSON(w, http.StatusOK, fromStoredRow(row))
}

// lookupEntry returns the registry entry for id. Linear over the registry
// because the public Get returns just the Backend, not the source/status.
func (d *BackendDeps) lookupEntry(id string) (backend.Entry, bool) {
	for _, e := range d.Registry.List() {
		if e.Backend.ID() == id {
			return e, true
		}
	}
	return backend.Entry{}, false
}

// ---- Create -------------------------------------------------------------

type createBackendRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	// Endpoint accepts an arbitrary scheme://host[:port] URL — nonstandard
	// ports are fine; see validateEndpoint.
	Endpoint string `json:"endpoint"`
	Region   string `json:"region"`
	// PathStyle is a *bool so that "omitted" can default to true. Path-style
	// is the safer default for the self-hosted S3-compatibles this proxy is
	// usually pointed at (MinIO, Garage, SeaweedFS); AWS S3 still accepts it.
	PathStyle *bool  `json:"path_style"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
	Enabled   *bool  `json:"enabled"`
}

func (d *BackendDeps) handleAdminCreateBackend(w http.ResponseWriter, r *http.Request) {
	if d.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "endpoint storage not configured", "")
		return
	}
	if d.Sealer == nil {
		writeError(w, http.StatusServiceUnavailable, "secret_key_unset",
			"set STOWAGE_SECRET_KEY to manage endpoints from the UI", "")
		return
	}
	var req createBackendRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	req.ID = strings.TrimSpace(strings.ToLower(req.ID))
	if !idRegexp.MatchString(req.ID) {
		writeError(w, http.StatusBadRequest, "bad_request",
			"id must be 1-64 chars of lowercase letters, digits, '-' or '_' starting alnum", "")
		return
	}
	if err := validateEndpoint(req.Endpoint); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), "")
		return
	}
	if req.AccessKey == "" || req.SecretKey == "" {
		writeError(w, http.StatusBadRequest, "bad_request",
			"access_key and secret_key are required", "")
		return
	}
	if req.Type == "" {
		req.Type = "s3v4"
	}

	// Refuse if a YAML-managed entry with that id is already serving.
	if src, ok := d.Registry.Source(req.ID); ok && src == backend.SourceConfig {
		writeError(w, http.StatusConflict, "yaml_managed",
			"an endpoint with this id is defined in config.yaml; choose a different id", "")
		return
	}

	now := time.Now().UTC()
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	pathStyle := true
	if req.PathStyle != nil {
		pathStyle = *req.PathStyle
	}
	createdBy := actorID(r)
	row := &sqlite.Backend{
		ID:        req.ID,
		Name:      valueOr(req.Name, req.ID),
		Type:      req.Type,
		Endpoint:  req.Endpoint,
		Region:    req.Region,
		PathStyle: pathStyle,
		AccessKey: req.AccessKey,
		Enabled:   enabled,
		CreatedAt: now,
		UpdatedAt: now,
		CreatedBy: nullStringOf(createdBy),
		UpdatedBy: nullStringOf(createdBy),
	}
	enc, err := d.Sealer.Seal([]byte(req.SecretKey))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not seal secret", "")
		return
	}
	row.SecretKeyEnc = enc

	// Build the runtime driver before persisting. Catches obviously broken
	// configs (bad endpoint, unknown type) up front so we don't write a row
	// that can never come up.
	driver, err := d.driverFor(r.Context(), row, req.SecretKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), "")
		return
	}

	if err := d.Store.CreateBackend(r.Context(), row); err != nil {
		if errors.Is(err, sqlite.ErrBackendIDTaken) {
			writeError(w, http.StatusConflict, "id_taken", "an endpoint with this id already exists", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not save backend", "")
		return
	}

	if enabled {
		if err := d.Registry.RegisterWithSource(driver, backend.SourceDB); err != nil {
			// Roll back the row so the next attempt can reuse the id.
			_ = d.Store.DeleteBackend(r.Context(), row.ID)
			writeError(w, http.StatusConflict, "register_failed", err.Error(), "")
			return
		}
	}

	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "backend.create",
		Backend: row.ID,
		Detail:  map[string]any{"endpoint": row.Endpoint, "region": row.Region, "type": row.Type},
	})
	writeJSON(w, http.StatusCreated, fromStoredRow(row))
}

// ---- Patch --------------------------------------------------------------

type patchBackendRequest struct {
	Name      *string `json:"name"`
	Endpoint  *string `json:"endpoint"`
	Region    *string `json:"region"`
	PathStyle *bool   `json:"path_style"`
	AccessKey *string `json:"access_key"`
	SecretKey *string `json:"secret_key"`
	Enabled   *bool   `json:"enabled"`
}

func (d *BackendDeps) handleAdminPatchBackend(w http.ResponseWriter, r *http.Request) {
	if d.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "endpoint storage not configured", "")
		return
	}
	if d.Sealer == nil {
		writeError(w, http.StatusServiceUnavailable, "secret_key_unset",
			"set STOWAGE_SECRET_KEY to manage endpoints from the UI", "")
		return
	}
	id := chi.URLParam(r, "bid")

	// YAML-managed entries are immutable through the UI by policy.
	if src, ok := d.Registry.Source(id); ok && src == backend.SourceConfig {
		writeError(w, http.StatusConflict, "yaml_managed",
			"this endpoint is defined in config.yaml; edit the file and restart", "")
		return
	}

	var req patchBackendRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}

	row, err := d.Store.GetBackend(r.Context(), id)
	if errors.Is(err, sqlite.ErrBackendNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "backend not found", "")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load backend", "")
		return
	}

	// Resolve effective row by overlaying the patch. This is the shape the
	// rebuilt driver will see and the shape we write back.
	if req.Name != nil {
		row.Name = *req.Name
	}
	if req.Endpoint != nil {
		if err := validateEndpoint(*req.Endpoint); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error(), "")
			return
		}
		row.Endpoint = *req.Endpoint
	}
	if req.Region != nil {
		row.Region = *req.Region
	}
	if req.PathStyle != nil {
		row.PathStyle = *req.PathStyle
	}
	if req.AccessKey != nil {
		row.AccessKey = *req.AccessKey
	}
	if req.Enabled != nil {
		row.Enabled = *req.Enabled
	}

	// Resolve the cleartext secret needed to (re)build the driver. New
	// secret in the request takes precedence; otherwise unseal the stored
	// envelope so the rebuilt driver still has working creds.
	var cleartext string
	if req.SecretKey != nil {
		cleartext = *req.SecretKey
	} else if len(row.SecretKeyEnc) > 0 {
		pt, err := d.Sealer.Open(row.SecretKeyEnc)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal",
				"stored secret could not be unsealed; rotate the secret_key", "")
			return
		}
		cleartext = string(pt)
	}

	patch := sqlite.BackendPatch{
		Name:      req.Name,
		Endpoint:  req.Endpoint,
		Region:    req.Region,
		PathStyle: req.PathStyle,
		AccessKey: req.AccessKey,
		Enabled:   req.Enabled,
		UpdatedAt: time.Now().UTC(),
		UpdatedBy: nullStringOf(actorID(r)),
	}
	if req.SecretKey != nil {
		enc, err := d.Sealer.Seal([]byte(*req.SecretKey))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not seal secret", "")
			return
		}
		patch.SetSecret(enc)
	}

	// Build the new driver before writing. If it fails we surface the
	// validation error and leave the DB untouched.
	driver, err := d.driverFor(r.Context(), row, cleartext)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), "")
		return
	}

	if err := d.Store.UpdateBackend(r.Context(), id, patch); err != nil {
		if errors.Is(err, sqlite.ErrBackendNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "backend not found", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not save backend", "")
		return
	}

	// Reflect the patch in the registry. Three states matter:
	//   already-registered & enabled  -> Replace
	//   already-registered & disabled -> Unregister
	//   not-registered & enabled      -> Register (e.g. first re-enable)
	//   not-registered & disabled     -> nothing to do
	_, registered := d.Registry.Get(id)
	switch {
	case registered && row.Enabled:
		_ = d.Registry.Replace(id, driver)
	case registered && !row.Enabled:
		_ = d.Registry.Unregister(id)
	case !registered && row.Enabled:
		_ = d.Registry.RegisterWithSource(driver, backend.SourceDB)
	}

	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "backend.update",
		Backend: id,
		Detail:  map[string]any{"fields": patchedFields(req)},
	})

	if updated, err := d.Store.GetBackend(r.Context(), id); err == nil {
		writeJSON(w, http.StatusOK, fromStoredRow(updated))
		return
	}
	writeJSON(w, http.StatusOK, fromStoredRow(row))
}

func patchedFields(p patchBackendRequest) []string {
	out := []string{}
	if p.Name != nil {
		out = append(out, "name")
	}
	if p.Endpoint != nil {
		out = append(out, "endpoint")
	}
	if p.Region != nil {
		out = append(out, "region")
	}
	if p.PathStyle != nil {
		out = append(out, "path_style")
	}
	if p.AccessKey != nil {
		out = append(out, "access_key")
	}
	if p.SecretKey != nil {
		out = append(out, "secret_key")
	}
	if p.Enabled != nil {
		out = append(out, "enabled")
	}
	return out
}

// ---- Delete -------------------------------------------------------------

func (d *BackendDeps) handleAdminDeleteBackend(w http.ResponseWriter, r *http.Request) {
	if d.Store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "endpoint storage not configured", "")
		return
	}
	id := chi.URLParam(r, "bid")
	if src, ok := d.Registry.Source(id); ok && src == backend.SourceConfig {
		writeError(w, http.StatusConflict, "yaml_managed",
			"this endpoint is defined in config.yaml; remove it from the file instead", "")
		return
	}

	err := d.Store.DeleteBackend(r.Context(), id)
	if errors.Is(err, sqlite.ErrBackendNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "backend not found", "")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not delete backend", "")
		return
	}
	_ = d.Registry.Unregister(id) // silently ok: may already be gone if disabled

	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "backend.delete",
		Backend: id,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ---- Test connection ----------------------------------------------------

type testBackendRequest struct {
	Type     string `json:"type"`
	Endpoint string `json:"endpoint"`
	Region   string `json:"region"`
	// PathStyle is a *bool so that omission can default to true; matches the
	// create handler so a "Test connection" before a save behaves identically.
	PathStyle *bool  `json:"path_style"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

func (d *BackendDeps) handleAdminTestBackend(w http.ResponseWriter, r *http.Request) {
	var req testBackendRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	if err := validateEndpoint(req.Endpoint); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), "")
		return
	}
	if req.AccessKey == "" || req.SecretKey == "" {
		writeError(w, http.StatusBadRequest, "bad_request",
			"access_key and secret_key are required", "")
		return
	}
	if req.Type == "" {
		req.Type = "s3v4"
	}

	pathStyle := true
	if req.PathStyle != nil {
		pathStyle = *req.PathStyle
	}
	// Build a throw-away driver under a synthetic id; it's never registered.
	throwaway := &sqlite.Backend{
		ID:        "test-connection",
		Name:      "Test connection",
		Type:      req.Type,
		Endpoint:  req.Endpoint,
		Region:    req.Region,
		PathStyle: pathStyle,
		AccessKey: req.AccessKey,
	}
	driver, err := d.driverFor(r.Context(), throwaway, req.SecretKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), "")
		return
	}

	st := backend.Probe(r.Context(), driver, 5*time.Second)
	resp := map[string]any{
		"healthy":    st.Healthy,
		"latency_ms": st.LastLatency.Milliseconds(),
	}
	if st.LastError != "" {
		resp["error"] = st.LastError
	}

	audit.RecordRequest(d.Audit, r, audit.Event{
		Action: "backend.test",
		Detail: map[string]any{"endpoint": req.Endpoint, "healthy": st.Healthy},
	})
	writeJSON(w, http.StatusOK, resp)
}

// ---- Helpers ------------------------------------------------------------

func validateEndpoint(s string) error {
	if s == "" {
		return errors.New("endpoint is required")
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return errors.New("endpoint must be a valid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("endpoint scheme must be http or https")
	}
	// A nonstandard port is fine, but it must be a number in the TCP range.
	// url.Parse leaves obvious garbage (":abc") in u.Host without erroring on
	// older Go versions, so re-validate explicitly here.
	if portStr := u.Port(); portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil || port < 1 || port > 65535 {
			return errors.New("endpoint port must be a number between 1 and 65535")
		}
	}
	// The s3v4 driver passes the endpoint to the AWS SDK as BaseEndpoint, which
	// expects scheme://host[:port] only. A path/query/fragment here would be
	// silently dropped or merged into request URLs in confusing ways — reject
	// up front so the user can fix it.
	if u.Path != "" && u.Path != "/" {
		return errors.New("endpoint must not include a path")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return errors.New("endpoint must not include a query string or fragment")
	}
	if u.User != nil {
		return errors.New("endpoint must not include credentials; use access_key/secret_key")
	}
	return nil
}

func actorID(r *http.Request) string {
	id := auth.IdentityFrom(r.Context())
	if id == nil {
		return ""
	}
	return id.UserID
}

func nullStringOf(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
