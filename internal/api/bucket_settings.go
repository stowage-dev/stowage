// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stowage-dev/stowage/internal/audit"
	"github.com/stowage-dev/stowage/internal/backend"
	"github.com/stowage-dev/stowage/internal/backend/s3v4"
)

// Bucket settings — Phase 6 Slice A. Each handler is admin-only at the
// router layer; here we just speak to the backend driver.

// settingsReadTimeout caps GET-side bucket-settings calls. The settings page
// fans out to every capability in parallel, so one backend that hangs (e.g.
// older Garage swallowing GetBucketPolicy until the SDK exhausts retries)
// would otherwise block the whole page. Mutations are user-triggered and
// keep the request's full context.
const settingsReadTimeout = 5 * time.Second

// ---- Versioning ---------------------------------------------------------

type versioningDTO struct {
	Enabled bool `json:"enabled"`
}

func (d *BackendDeps) handleGetBucketVersioning(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	if !b.Capabilities().Versioning {
		writeError(w, http.StatusNotImplemented, "not_supported", "backend does not support versioning", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), settingsReadTimeout)
	defer cancel()
	enabled, err := b.GetBucketVersioning(ctx, bucket)
	if err != nil {
		d.backendError(w, r, err, "get versioning")
		return
	}
	writeJSON(w, http.StatusOK, versioningDTO{Enabled: enabled})
}

func (d *BackendDeps) handlePutBucketVersioning(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	if !b.Capabilities().Versioning {
		writeError(w, http.StatusNotImplemented, "not_supported", "backend does not support versioning", "")
		return
	}
	var req versioningDTO
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	if err := b.SetBucketVersioning(r.Context(), bucket, req.Enabled); err != nil {
		d.backendError(w, r, err, "set versioning")
		return
	}
	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "bucket.versioning.set",
		Backend: chi.URLParam(r, "bid"),
		Bucket:  bucket,
		Detail:  map[string]any{"enabled": req.Enabled},
	})
	writeJSON(w, http.StatusOK, req)
}

// ---- CORS ---------------------------------------------------------------

type corsRuleDTO struct {
	AllowedOrigins []string `json:"allowed_origins"`
	AllowedMethods []string `json:"allowed_methods"`
	AllowedHeaders []string `json:"allowed_headers,omitempty"`
	ExposeHeaders  []string `json:"expose_headers,omitempty"`
	MaxAgeSeconds  int      `json:"max_age_seconds,omitempty"`
}

type corsPayload struct {
	Rules []corsRuleDTO `json:"rules"`
}

// allowedCORSMethods is the S3-permitted set. AWS rejects anything else
// with 400; we surface that as a clean validation error instead.
var allowedCORSMethods = map[string]struct{}{
	"GET": {}, "PUT": {}, "POST": {}, "DELETE": {}, "HEAD": {},
}

func (d *BackendDeps) handleGetBucketCORS(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	if !b.Capabilities().CORS {
		writeError(w, http.StatusNotImplemented, "not_supported", "backend does not support CORS", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), settingsReadTimeout)
	defer cancel()
	rules, err := b.GetBucketCORS(ctx, bucket)
	if err != nil {
		// Backends return 404 when no CORS configuration exists. Treat that
		// as "no rules" rather than a hard error so the UI can render an
		// empty editor.
		if s3v4.IsNotFound(err) {
			writeJSON(w, http.StatusOK, corsPayload{Rules: []corsRuleDTO{}})
			return
		}
		d.backendError(w, r, err, "get cors")
		return
	}
	out := make([]corsRuleDTO, 0, len(rules))
	for _, ru := range rules {
		out = append(out, corsRuleDTO{
			AllowedOrigins: ru.AllowedOrigins,
			AllowedMethods: ru.AllowedMethods,
			AllowedHeaders: ru.AllowedHeaders,
			ExposeHeaders:  ru.ExposeHeaders,
			MaxAgeSeconds:  ru.MaxAgeSeconds,
		})
	}
	writeJSON(w, http.StatusOK, corsPayload{Rules: out})
}

func (d *BackendDeps) handlePutBucketCORS(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	if !b.Capabilities().CORS {
		writeError(w, http.StatusNotImplemented, "not_supported", "backend does not support CORS", "")
		return
	}
	var req corsPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	rules := make([]backend.CORSRule, 0, len(req.Rules))
	for i, ru := range req.Rules {
		if len(ru.AllowedOrigins) == 0 {
			writeError(w, http.StatusBadRequest, "invalid_cors",
				"rule "+strconv.Itoa(i)+": allowed_origins is required", "")
			return
		}
		if len(ru.AllowedMethods) == 0 {
			writeError(w, http.StatusBadRequest, "invalid_cors",
				"rule "+strconv.Itoa(i)+": allowed_methods is required", "")
			return
		}
		for _, m := range ru.AllowedMethods {
			if _, ok := allowedCORSMethods[m]; !ok {
				writeError(w, http.StatusBadRequest, "invalid_cors",
					"rule "+strconv.Itoa(i)+": method "+m+" is not allowed by S3", "")
				return
			}
		}
		if ru.MaxAgeSeconds < 0 {
			writeError(w, http.StatusBadRequest, "invalid_cors",
				"rule "+strconv.Itoa(i)+": max_age_seconds must be non-negative", "")
			return
		}
		rules = append(rules, backend.CORSRule{
			AllowedOrigins: ru.AllowedOrigins,
			AllowedMethods: ru.AllowedMethods,
			AllowedHeaders: ru.AllowedHeaders,
			ExposeHeaders:  ru.ExposeHeaders,
			MaxAgeSeconds:  ru.MaxAgeSeconds,
		})
	}
	if err := b.SetBucketCORS(r.Context(), bucket, rules); err != nil {
		d.backendError(w, r, err, "set cors")
		return
	}
	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "bucket.cors.set",
		Backend: chi.URLParam(r, "bid"),
		Bucket:  bucket,
		Detail:  map[string]any{"rules": len(rules)},
	})
	w.WriteHeader(http.StatusNoContent)
}

// ---- Policy -------------------------------------------------------------

type policyDTO struct {
	// Policy is an opaque JSON document. We round-trip it as a string so
	// the client can store/edit it verbatim and send it back without
	// canonicalisation drift.
	Policy string `json:"policy"`
}

func (d *BackendDeps) handleGetBucketPolicy(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	if !b.Capabilities().BucketPolicy {
		writeError(w, http.StatusNotImplemented, "not_supported", "backend does not support bucket policies", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), settingsReadTimeout)
	defer cancel()
	policy, err := b.GetBucketPolicy(ctx, bucket)
	if err != nil {
		if s3v4.IsNotFound(err) {
			writeJSON(w, http.StatusOK, policyDTO{Policy: ""})
			return
		}
		d.backendError(w, r, err, "get policy")
		return
	}
	writeJSON(w, http.StatusOK, policyDTO{Policy: policy})
}

func (d *BackendDeps) handlePutBucketPolicy(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	if !b.Capabilities().BucketPolicy {
		writeError(w, http.StatusNotImplemented, "not_supported", "backend does not support bucket policies", "")
		return
	}
	var req policyDTO
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	if req.Policy == "" {
		writeError(w, http.StatusBadRequest, "bad_request",
			"policy is empty — DELETE the resource to remove the policy", "")
		return
	}
	// Validate the policy is at least syntactically JSON. AWS will reject
	// semantically-invalid policies; we leave that to the backend so we
	// don't duplicate AWS's policy grammar.
	var probe any
	if err := json.Unmarshal([]byte(req.Policy), &probe); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_policy",
			"policy is not valid JSON: "+err.Error(), "")
		return
	}
	if err := b.SetBucketPolicy(r.Context(), bucket, req.Policy); err != nil {
		d.backendError(w, r, err, "set policy")
		return
	}
	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "bucket.policy.set",
		Backend: chi.URLParam(r, "bid"),
		Bucket:  bucket,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (d *BackendDeps) handleDeleteBucketPolicy(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	if !b.Capabilities().BucketPolicy {
		writeError(w, http.StatusNotImplemented, "not_supported", "backend does not support bucket policies", "")
		return
	}
	if err := b.DeleteBucketPolicy(r.Context(), bucket); err != nil {
		// 404 from the backend just means there was nothing to delete.
		if !s3v4.IsNotFound(err) {
			d.backendError(w, r, err, "delete policy")
			return
		}
	}
	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "bucket.policy.delete",
		Backend: chi.URLParam(r, "bid"),
		Bucket:  bucket,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ---- Lifecycle ----------------------------------------------------------

type lifecycleRuleDTO struct {
	ID                     string `json:"id,omitempty"`
	Prefix                 string `json:"prefix,omitempty"`
	Enabled                bool   `json:"enabled"`
	ExpirationDays         int    `json:"expiration_days,omitempty"`
	NoncurrentExpireDays   int    `json:"noncurrent_expire_days,omitempty"`
	AbortIncompleteDays    int    `json:"abort_incomplete_days,omitempty"`
	TransitionDays         int    `json:"transition_days,omitempty"`
	TransitionStorageClass string `json:"transition_storage_class,omitempty"`
}

type lifecyclePayload struct {
	Rules []lifecycleRuleDTO `json:"rules"`
}

func (d *BackendDeps) handleGetBucketLifecycle(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	if !b.Capabilities().Lifecycle {
		writeError(w, http.StatusNotImplemented, "not_supported", "backend does not support lifecycle rules", "")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), settingsReadTimeout)
	defer cancel()
	rules, err := b.GetBucketLifecycle(ctx, bucket)
	if err != nil {
		if s3v4.IsNotFound(err) {
			writeJSON(w, http.StatusOK, lifecyclePayload{Rules: []lifecycleRuleDTO{}})
			return
		}
		d.backendError(w, r, err, "get lifecycle")
		return
	}
	out := make([]lifecycleRuleDTO, 0, len(rules))
	for _, ru := range rules {
		out = append(out, lifecycleRuleDTO{
			ID:                     ru.ID,
			Prefix:                 ru.Prefix,
			Enabled:                ru.Enabled,
			ExpirationDays:         ru.ExpirationDays,
			NoncurrentExpireDays:   ru.NoncurrentExpireDays,
			AbortIncompleteDays:    ru.AbortIncompleteDays,
			TransitionDays:         ru.TransitionDays,
			TransitionStorageClass: ru.TransitionStorageClass,
		})
	}
	writeJSON(w, http.StatusOK, lifecyclePayload{Rules: out})
}

func (d *BackendDeps) handlePutBucketLifecycle(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	if !b.Capabilities().Lifecycle {
		writeError(w, http.StatusNotImplemented, "not_supported", "backend does not support lifecycle rules", "")
		return
	}
	var req lifecyclePayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	rules := make([]backend.LifecycleRule, 0, len(req.Rules))
	for i, ru := range req.Rules {
		// A rule with no actions is a no-op that AWS rejects with a cryptic
		// error. Catch it up front.
		if !ruleHasAction(ru) {
			writeError(w, http.StatusBadRequest, "invalid_lifecycle",
				"rule "+strconv.Itoa(i)+": at least one of expiration_days, noncurrent_expire_days, abort_incomplete_days, or transition_days is required", "")
			return
		}
		if ru.TransitionDays > 0 && ru.TransitionStorageClass == "" {
			writeError(w, http.StatusBadRequest, "invalid_lifecycle",
				"rule "+strconv.Itoa(i)+": transition_storage_class is required when transition_days is set", "")
			return
		}
		rules = append(rules, backend.LifecycleRule{
			ID:                     ru.ID,
			Prefix:                 ru.Prefix,
			Enabled:                ru.Enabled,
			ExpirationDays:         ru.ExpirationDays,
			NoncurrentExpireDays:   ru.NoncurrentExpireDays,
			AbortIncompleteDays:    ru.AbortIncompleteDays,
			TransitionDays:         ru.TransitionDays,
			TransitionStorageClass: ru.TransitionStorageClass,
		})
	}
	if err := b.SetBucketLifecycle(r.Context(), bucket, rules); err != nil {
		d.backendError(w, r, err, "set lifecycle")
		return
	}
	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "bucket.lifecycle.set",
		Backend: chi.URLParam(r, "bid"),
		Bucket:  bucket,
		Detail:  map[string]any{"rules": len(rules)},
	})
	w.WriteHeader(http.StatusNoContent)
}

func ruleHasAction(r lifecycleRuleDTO) bool {
	return r.ExpirationDays > 0 || r.NoncurrentExpireDays > 0 ||
		r.AbortIncompleteDays > 0 || r.TransitionDays > 0
}
