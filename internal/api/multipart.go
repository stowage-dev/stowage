// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stowage-dev/stowage/internal/backend"
	"github.com/stowage-dev/stowage/internal/backend/s3v4"
)

// Multipart endpoints.
//
// upload_id is passed as a query parameter rather than a path segment. S3
// upload IDs are opaque and commonly contain '/', '+' and '=' which are
// awkward in path positions even when URL-encoded (some intermediaries
// decode early). Query params sidestep that and keep the route table
// straightforward.
//
// Routes (relative to /api/backends/{bid}/buckets/{bucket}):
//   POST   /multipart?key=...                                  → create
//   PUT    /multipart/parts/{part}?key=...&upload_id=...       → upload part (streamed)
//   POST   /multipart/complete?key=...&upload_id=...           → complete
//   DELETE /multipart?key=...&upload_id=...                    → abort
//   GET    /multipart?prefix=...                               → list in-progress

// Per-part ceiling. S3 caps individual parts at 5 GiB, but Phase 3 the
// proxy buffers each part to satisfy SigV4 over plain HTTP (see
// internal/backend/s3v4/s3v4.go MaxBufferedPartBytes). The handler cap
// matches the driver cap so users get a clear 413 instead of an opaque
// backend_error mid-stream.
const maxPartBytes = 64 * 1024 * 1024

type createMultipartRequest struct {
	ContentType string            `json:"content_type"`
	Metadata    map[string]string `json:"metadata"`
}

type createMultipartResponse struct {
	Bucket   string `json:"bucket"`
	Key      string `json:"key"`
	UploadID string `json:"upload_id"`
}

func (d *BackendDeps) handleMultipartCreate(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	key := r.URL.Query().Get("key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "key query param is required", "")
		return
	}
	if !validObjectKey(key) {
		writeError(w, http.StatusBadRequest, "invalid_key", "object key is invalid", "")
		return
	}
	var req createMultipartRequest
	// Body is optional — empty body means default content type and no metadata.
	if r.ContentLength > 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
			return
		}
	}
	// Conditional write: same `If-None-Match: *` semantics as the single-PUT
	// path. Reject before initiating the multipart so the client doesn't
	// upload parts that would be discarded at completion time.
	if r.Header.Get("If-None-Match") == "*" {
		if _, err := b.HeadObject(r.Context(), bucket, key, ""); err == nil {
			writeError(w, http.StatusPreconditionFailed, "object_exists",
				"an object already exists at this key", "")
			return
		} else if !s3v4.IsNotFound(err) {
			d.backendError(w, r, err, "head object")
			return
		}
	}
	uploadID, err := b.CreateMultipart(r.Context(), bucket, key, req.ContentType, req.Metadata)
	if err != nil {
		d.backendError(w, r, err, "create multipart")
		return
	}
	writeJSON(w, http.StatusCreated, createMultipartResponse{
		Bucket: bucket, Key: key, UploadID: uploadID,
	})
}

type uploadPartResponse struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
	Size       int64  `json:"size"`
}

func (d *BackendDeps) handleMultipartUploadPart(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	key := r.URL.Query().Get("key")
	uploadID := r.URL.Query().Get("upload_id")
	if key == "" || uploadID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "key and upload_id query params are required", "")
		return
	}
	partStr := chi.URLParam(r, "part")
	partNum, err := strconv.Atoi(partStr)
	if err != nil || partNum < 1 || partNum > 10000 {
		writeError(w, http.StatusBadRequest, "bad_request", "part must be an integer between 1 and 10000", "")
		return
	}
	if r.ContentLength <= 0 {
		writeError(w, http.StatusLengthRequired, "length_required", "Content-Length is required for part uploads", "")
		return
	}
	if r.ContentLength > maxPartBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large",
			"part exceeds 5 GiB ceiling", "")
		return
	}

	// Cap the request body at the declared length. This keeps the proxy
	// honest (no buffering past the declared size) and the SDK happy
	// (Content-Length-driven streaming, no SHA pre-computation pass).
	body := http.MaxBytesReader(w, r.Body, r.ContentLength)
	defer body.Close()

	if err := d.checkQuota(r, bucket, r.ContentLength); err != nil {
		d.writeQuotaError(w, err)
		return
	}

	etag, err := b.UploadPart(r.Context(), bucket, key, uploadID, partNum, body, r.ContentLength)
	if err != nil {
		d.backendError(w, r, err, "upload part")
		return
	}
	// Bump the cache per-part. If the multipart aborts later we'll over-
	// count until the scheduler reconciles, which is the safer direction.
	d.recordQuotaWrite(r, bucket, r.ContentLength)
	writeJSON(w, http.StatusOK, uploadPartResponse{
		PartNumber: partNum, ETag: etag, Size: r.ContentLength,
	})
}

type completeMultipartRequest struct {
	Parts []backend.CompletedPart `json:"parts"`
}

func (d *BackendDeps) handleMultipartComplete(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	key := r.URL.Query().Get("key")
	uploadID := r.URL.Query().Get("upload_id")
	if key == "" || uploadID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "key and upload_id query params are required", "")
		return
	}
	var req completeMultipartRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	if len(req.Parts) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "parts is required and must be non-empty", "")
		return
	}
	info, err := b.CompleteMultipart(r.Context(), bucket, key, uploadID, req.Parts)
	if err != nil {
		d.backendError(w, r, err, "complete multipart")
		return
	}
	writeJSON(w, http.StatusOK, toObjectDTO(info))
}

func (d *BackendDeps) handleMultipartAbort(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	key := r.URL.Query().Get("key")
	uploadID := r.URL.Query().Get("upload_id")
	if key == "" || uploadID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "key and upload_id query params are required", "")
		return
	}
	if err := b.AbortMultipart(r.Context(), bucket, key, uploadID); err != nil {
		d.backendError(w, r, err, "abort multipart")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type multipartUploadDTO struct {
	Key       string `json:"key"`
	UploadID  string `json:"upload_id"`
	Initiated string `json:"initiated,omitempty"`
}

func (d *BackendDeps) handleMultipartList(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	prefix := r.URL.Query().Get("prefix")
	uploads, err := b.ListMultipartUploads(r.Context(), bucket, prefix)
	if err != nil {
		d.backendError(w, r, err, "list multipart uploads")
		return
	}
	out := make([]multipartUploadDTO, 0, len(uploads))
	for _, u := range uploads {
		dto := multipartUploadDTO{Key: u.Key, UploadID: u.UploadID}
		if !u.Initiated.IsZero() {
			dto.Initiated = u.Initiated.Format(time.RFC3339)
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, map[string]any{"uploads": out})
}
