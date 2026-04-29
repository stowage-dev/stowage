// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stowage-dev/stowage/internal/audit"
	"github.com/stowage-dev/stowage/internal/backend"
	"github.com/stowage-dev/stowage/internal/backend/s3v4"
	"github.com/stowage-dev/stowage/internal/quotas"
	"github.com/stowage-dev/stowage/internal/secrets"
	"github.com/stowage-dev/stowage/internal/sizes"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// objectCopyBufPool keeps 32 KiB scratch buffers around for streaming object
// bodies through io.CopyBuffer. The default io.Copy fallback allocates a
// fresh 32 KiB buffer per call; pooling them removes one allocation per GET.
var objectCopyBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

const maxSingleUploadBytes = 5 * 1024 * 1024 // Phase 2 cap; multipart arrives in Phase 3.

// BackendDeps groups the registry handler needs. Separate from AuthDeps so
// the router wiring stays transparent.
type BackendDeps struct {
	Registry *backend.Registry
	Logger   *slog.Logger
	Quotas   *quotas.Service // optional; nil disables proxy quota enforcement
	Sizes    *sizes.Service  // optional; nil disables proxy-computed bucket/prefix sizes
	Audit    audit.Recorder  // nil-safe via audit.Noop

	// Store and Sealer power the admin endpoint-management API. Both are
	// required for /api/admin/backends; when nil those handlers report
	// 503 secret_key_unset / 503 store_unavailable rather than panic.
	Store  *sqlite.Store
	Sealer *secrets.Sealer

	// BuildBackend lets tests plug in a memory backend instead of the
	// production s3v4 driver. nil means "use s3v4".
	BuildBackend func(ctx context.Context, row *sqlite.Backend, secretKey string) (backend.Backend, error)

	// bucketsCache memoises ListBuckets responses for a few seconds; lazily
	// allocated so tests that don't pre-seed BackendDeps still work.
	bucketsCacheOnce sync.Once
	bucketsCacheRef  *bucketsCache
}

func (d *BackendDeps) buckets() *bucketsCache {
	d.bucketsCacheOnce.Do(func() { d.bucketsCacheRef = newBucketsCache() })
	return d.bucketsCacheRef
}

// driverFor builds a runtime backend from a stored row + cleartext secret.
// Honours BuildBackend when set; otherwise dispatches on Type to the
// production s3v4 driver. The cleartext secret is held only for the
// duration of this call.
func (d *BackendDeps) driverFor(ctx context.Context, row *sqlite.Backend, secretKey string) (backend.Backend, error) {
	if d.BuildBackend != nil {
		return d.BuildBackend(ctx, row, secretKey)
	}
	switch row.Type {
	case "s3v4", "":
		return s3v4.New(ctx, s3v4.Config{
			ID:        row.ID,
			Name:      valueOr(row.Name, row.ID),
			Endpoint:  row.Endpoint,
			Region:    row.Region,
			AccessKey: row.AccessKey,
			SecretKey: secretKey,
			PathStyle: row.PathStyle,
		})
	default:
		return nil, fmt.Errorf("unknown backend type %q", row.Type)
	}
}

func valueOr(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

// ---- /api/backends ------------------------------------------------------

type backendDTO struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Capabilities backend.Capabilities `json:"capabilities"`
	Healthy      bool                 `json:"healthy"`
	LastProbeAt  string               `json:"last_probe_at,omitempty"`
	LastError    string               `json:"last_error,omitempty"`
}

func (d *BackendDeps) handleListBackends(w http.ResponseWriter, _ *http.Request) {
	entries := d.Registry.List()
	out := make([]backendDTO, 0, len(entries))
	for _, e := range entries {
		dto := backendDTO{
			ID:           e.Backend.ID(),
			Name:         e.Backend.DisplayName(),
			Capabilities: e.Backend.Capabilities(),
			Healthy:      e.Status.Healthy,
			LastError:    e.Status.LastError,
		}
		if !e.Status.LastProbeAt.IsZero() {
			dto.LastProbeAt = e.Status.LastProbeAt.Format(time.RFC3339)
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, listBackendsResponse{Backends: out})
}

type listBackendsResponse struct {
	Backends []backendDTO `json:"backends"`
}

func (d *BackendDeps) handleGetBackend(w http.ResponseWriter, r *http.Request) {
	b, ok := d.Registry.Get(chi.URLParam(r, "bid"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "backend not found", "")
		return
	}
	st, _ := d.Registry.Status(b.ID())
	dto := backendDTO{
		ID:           b.ID(),
		Name:         b.DisplayName(),
		Capabilities: b.Capabilities(),
		Healthy:      st.Healthy,
		LastError:    st.LastError,
	}
	if !st.LastProbeAt.IsZero() {
		dto.LastProbeAt = st.LastProbeAt.Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, dto)
}

func (d *BackendDeps) handleProbeBackend(w http.ResponseWriter, r *http.Request) {
	b, ok := d.Registry.Get(chi.URLParam(r, "bid"))
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "backend not found", "")
		return
	}
	st := backend.Probe(r.Context(), b, 5*time.Second)
	d.Registry.SetStatus(b.ID(), st)
	writeJSON(w, http.StatusOK, probeResponse{
		Healthy:     st.Healthy,
		LastProbeAt: st.LastProbeAt.Format(time.RFC3339),
		LastError:   st.LastError,
	})
}

type probeResponse struct {
	Healthy     bool   `json:"healthy"`
	LastProbeAt string `json:"last_probe_at"`
	LastError   string `json:"last_error"`
}

// ---- Bucket ops ---------------------------------------------------------

type bucketDTO struct {
	Name        string `json:"name"`
	CreatedAt   string `json:"created_at,omitempty"`
	SizeTracked bool   `json:"size_tracked"`
	// Cached size figures, populated only when tracking is on for this
	// bucket and the scanner has run at least once. The client treats the
	// absence of these fields as "not yet known" rather than "zero".
	SizeBytes   *int64 `json:"size_bytes,omitempty"`
	ObjectCount *int64 `json:"object_count,omitempty"`
	ComputedAt  string `json:"computed_at,omitempty"`
}

func (d *BackendDeps) handleListBuckets(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	buckets, err := d.listBucketsCached(r, b)
	if err != nil {
		d.backendError(w, r, err, "list buckets")
		return
	}
	bid := chi.URLParam(r, "bid")
	disabled := map[string]struct{}{}
	if d.Sizes != nil && d.Sizes.Store != nil {
		// One round-trip for the whole listing instead of N per-bucket
		// queries. Errors here are non-fatal: tracking just appears on by
		// default for every bucket, which matches the schema default.
		if m, derr := d.Sizes.Store.ListDisabledSizeTracking(r.Context()); derr == nil {
			disabled = m
		} else {
			d.Logger.Warn("size tracking lookup failed",
				"backend", bid, "err", derr.Error())
		}
	}
	out := make([]bucketDTO, 0, len(buckets))
	for _, bk := range buckets {
		dto := bucketDTO{Name: bk.Name}
		if !bk.CreatedAt.IsZero() {
			dto.CreatedAt = bk.CreatedAt.Format(time.RFC3339)
		}
		_, off := disabled[bid+"/"+bk.Name]
		dto.SizeTracked = !off
		if dto.SizeTracked && d.Sizes != nil {
			if u := d.Sizes.Get(bid, bk.Name); u != nil {
				bytes := u.Bytes
				count := u.ObjectCount
				dto.SizeBytes = &bytes
				dto.ObjectCount = &count
				dto.ComputedAt = u.ComputedAt.Format(time.RFC3339)
			}
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, listBucketsResponse{Buckets: out})
}

// listBucketsCached serves from the in-process cache when an entry is fresh
// (< bucketsCacheTTL). Concurrent requests for the same backend coalesce
// behind the per-entry mutex so the inevitable simultaneous page-load
// fan-out becomes one upstream call.
func (d *BackendDeps) listBucketsCached(r *http.Request, b backend.Backend) ([]backend.Bucket, error) {
	cache := d.buckets()
	bid := chi.URLParam(r, "bid")
	e := cache.entry(bid)
	e.mu.Lock()
	defer e.mu.Unlock()
	if time.Now().Before(e.expiresAt) && e.buckets != nil {
		return e.buckets, nil
	}
	buckets, err := b.ListBuckets(r.Context())
	if err != nil {
		return nil, err
	}
	e.buckets = buckets
	e.expiresAt = time.Now().Add(bucketsCacheTTL)
	return buckets, nil
}

type listBucketsResponse struct {
	Buckets []bucketDTO `json:"buckets"`
}

type createBucketRequest struct {
	Name   string `json:"name"`
	Region string `json:"region"`
}

func (d *BackendDeps) handleCreateBucket(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	var req createBucketRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	if !validBucketName(req.Name) {
		writeError(w, http.StatusBadRequest, "invalid_bucket_name", "bucket name is invalid", "")
		return
	}
	if err := b.CreateBucket(r.Context(), req.Name, req.Region); err != nil {
		d.backendError(w, r, err, "create bucket")
		return
	}
	d.buckets().invalidate(chi.URLParam(r, "bid"))
	writeJSON(w, http.StatusCreated, createBucketResponse{Name: req.Name})
}

type createBucketResponse struct {
	Name string `json:"name"`
}

func (d *BackendDeps) handleDeleteBucket(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	if err := b.DeleteBucket(r.Context(), bucket); err != nil {
		d.backendError(w, r, err, "delete bucket")
		return
	}
	bid := chi.URLParam(r, "bid")
	d.buckets().invalidate(bid)
	if d.Sizes != nil {
		d.Sizes.Forget(bid, bucket)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- Object ops ---------------------------------------------------------

type objectDTO struct {
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	ETag         string            `json:"etag,omitempty"`
	ContentType  string            `json:"content_type,omitempty"`
	StorageClass string            `json:"storage_class,omitempty"`
	VersionID    string            `json:"version_id,omitempty"`
	LastModified string            `json:"last_modified,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

func toObjectDTO(o backend.ObjectInfo) objectDTO {
	d := objectDTO{
		Key:          o.Key,
		Size:         o.Size,
		ETag:         o.ETag,
		ContentType:  o.ContentType,
		StorageClass: o.StorageClass,
		VersionID:    o.VersionID,
		Metadata:     o.Metadata,
	}
	if !o.LastModified.IsZero() {
		d.LastModified = o.LastModified.Format(time.RFC3339)
	}
	return d
}

func (d *BackendDeps) handleListObjects(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	q := r.URL.Query()

	req := backend.ListObjectsRequest{
		Bucket:            bucket,
		Prefix:            q.Get("prefix"),
		ContinuationToken: q.Get("token"),
	}
	// Default delimiter=/ (hierarchical view) unless the client explicitly
	// sends delimiter= (flat listing).
	if _, present := q["delimiter"]; present {
		req.Delimiter = q.Get("delimiter")
	} else {
		req.Delimiter = "/"
	}
	if v := q.Get("max_keys"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			req.MaxKeys = n
		}
	}
	res, err := b.ListObjects(r.Context(), req)
	if err != nil {
		d.backendError(w, r, err, "list objects")
		return
	}
	objs := make([]objectDTO, 0, len(res.Objects))
	for _, o := range res.Objects {
		objs = append(objs, toObjectDTO(o))
	}
	if res.CommonPrefixes == nil {
		res.CommonPrefixes = []string{}
	}
	writeJSON(w, http.StatusOK, listObjectsResponse{
		Prefix:                req.Prefix,
		Delimiter:             req.Delimiter,
		CommonPrefixes:        res.CommonPrefixes,
		IsTruncated:           res.IsTruncated,
		NextContinuationToken: res.NextContinuationToken,
		Objects:               objs,
	})
}

type listObjectsResponse struct {
	Prefix                string      `json:"prefix"`
	Delimiter             string      `json:"delimiter"`
	CommonPrefixes        []string    `json:"common_prefixes"`
	IsTruncated           bool        `json:"is_truncated"`
	NextContinuationToken string      `json:"next_continuation_token"`
	Objects               []objectDTO `json:"objects"`
}

func (d *BackendDeps) handleHeadObject(w http.ResponseWriter, r *http.Request) {
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
	info, err := b.HeadObject(r.Context(), bucket, key, r.URL.Query().Get("version_id"))
	if err != nil {
		d.backendError(w, r, err, "head object")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if info.ETag != "" {
		w.Header().Set("ETag", info.ETag)
	}
	writeJSON(w, http.StatusOK, toObjectDTO(info))
}

func (d *BackendDeps) handleGetObject(w http.ResponseWriter, r *http.Request) {
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
	versionID := r.URL.Query().Get("version_id")
	rng := parseRangeHeader(r.Header.Get("Range"))

	// HEAD up front when a Range was requested — needed to populate the
	// Content-Range header on the 206 response. HTML5 <video>/<audio>
	// refuse to start playback without it.
	var totalSize int64
	if rng != nil {
		head, err := b.HeadObject(r.Context(), bucket, key, versionID)
		if err != nil {
			d.backendError(w, r, err, "head for range")
			return
		}
		totalSize = head.Size
		if rng.End < 0 || rng.End >= totalSize {
			rng.End = totalSize - 1
		}
		if rng.Start < 0 || rng.Start > rng.End {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", totalSize))
			writeError(w, http.StatusRequestedRangeNotSatisfiable, "bad_range", "range not satisfiable", "")
			return
		}
	}

	obj, err := b.GetObject(r.Context(), bucket, key, versionID, rng)
	if err != nil {
		d.backendError(w, r, err, "get object")
		return
	}
	defer obj.Close()

	info := obj.Info()
	if info.ContentType != "" {
		w.Header().Set("Content-Type", info.ContentType)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if info.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	}
	if info.ETag != "" {
		w.Header().Set("ETag", info.ETag)
	}
	w.Header().Set("Accept-Ranges", "bytes")
	disp := r.URL.Query().Get("disposition")
	if disp == "" {
		disp = "attachment"
	}
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`%s; filename="%s"`, disp, sanitizeFilename(path.Base(key))))
	if rng != nil {
		w.Header().Set("Content-Range",
			fmt.Sprintf("bytes %d-%d/%d", rng.Start, rng.End, totalSize))
		w.WriteHeader(http.StatusPartialContent)
	}
	bufp := objectCopyBufPool.Get().(*[]byte)
	_, _ = io.CopyBuffer(w, obj, *bufp)
	objectCopyBufPool.Put(bufp)
}

func (d *BackendDeps) handleUploadObject(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")

	// Cap at maxSingleUploadBytes + a little overhead for form headers.
	r.Body = http.MaxBytesReader(w, r.Body, maxSingleUploadBytes+64*1024)
	if err := r.ParseMultipartForm(maxSingleUploadBytes); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large",
			fmt.Sprintf("upload exceeds %d bytes (use multipart for larger uploads in Phase 3)", maxSingleUploadBytes), "")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "missing file field", "")
		return
	}
	defer f.Close()
	if hdr.Size > maxSingleUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large",
			fmt.Sprintf("file exceeds %d bytes", maxSingleUploadBytes), "")
		return
	}

	key := r.FormValue("key")
	if key == "" {
		key = hdr.Filename
	}
	if key == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "key field or filename is required", "")
		return
	}
	if !validObjectKey(key) {
		writeError(w, http.StatusBadRequest, "invalid_key", "object key is invalid", "")
		return
	}
	contentType := hdr.Header.Get("Content-Type")
	if v := r.FormValue("content_type"); v != "" {
		contentType = v
	}

	if err := d.checkQuota(r, bucket, hdr.Size); err != nil {
		d.writeQuotaError(w, err)
		return
	}

	// Conditional write: clients that want a confirmation prompt before
	// clobbering an existing object send `If-None-Match: *`. We do an explicit
	// HeadObject first because most S3-compatible backends don't honour the
	// header on PutObject itself (Garage and SeaweedFS notably).
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

	info, err := b.PutObject(r.Context(), backend.PutObjectRequest{
		Bucket:      bucket,
		Key:         key,
		Body:        f,
		Size:        hdr.Size,
		ContentType: contentType,
	})
	if err != nil {
		d.backendError(w, r, err, "put object")
		return
	}
	d.recordQuotaWrite(r, bucket, info.Size)
	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "object.upload",
		Backend: chi.URLParam(r, "bid"),
		Bucket:  bucket,
		Key:     key,
		Detail:  map[string]any{"size": info.Size, "content_type": contentType},
	})
	writeJSON(w, http.StatusCreated, toObjectDTO(info))
}

// checkQuota gates writes through the quota service when one is wired and
// the bucket has a hard quota configured. Returns nil when enforcement is
// disabled or off, ErrQuotaExceeded when the projected post-write total
// would exceed the limit, or another error if the lookup itself failed.
func (d *BackendDeps) checkQuota(r *http.Request, bucket string, addBytes int64) error {
	if d.Quotas == nil {
		return nil
	}
	bid := chi.URLParam(r, "bid")
	return d.Quotas.CheckUpload(r.Context(), bid, bucket, addBytes)
}

// recordQuotaWrite optimistically bumps the in-memory cache after a
// successful write, so a quota that was almost full doesn't briefly look
// available again until the next scheduled scan. Also nudges the
// size-tracking cache so the bucket listing reflects new uploads
// without waiting for the next scheduled scan.
func (d *BackendDeps) recordQuotaWrite(r *http.Request, bucket string, addBytes int64) {
	if addBytes <= 0 {
		return
	}
	bid := chi.URLParam(r, "bid")
	if d.Quotas != nil {
		d.Quotas.Recorded(bid, bucket, addBytes)
	}
	if d.Sizes != nil {
		d.Sizes.Recorded(bid, bucket, addBytes)
	}
}

// writeQuotaError maps quota-service errors to HTTP responses. Hard-quota
// failures get 507 Insufficient Storage so clients can branch on it
// (browsers don't surface the body, so the JSON body is for API consumers).
func (d *BackendDeps) writeQuotaError(w http.ResponseWriter, err error) {
	if errors.Is(err, quotas.ErrQuotaExceeded) {
		writeError(w, http.StatusInsufficientStorage, "quota_exceeded",
			"bucket hard quota exceeded — delete objects or raise the quota", "")
		return
	}
	d.Logger.Warn("quota check failed", "err", err.Error())
	writeError(w, http.StatusInternalServerError, "internal", "quota check failed", "")
}

func (d *BackendDeps) handleDeleteObject(w http.ResponseWriter, r *http.Request) {
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
	if err := b.DeleteObject(r.Context(), bucket, key, r.URL.Query().Get("version_id")); err != nil {
		d.backendError(w, r, err, "delete object")
		return
	}
	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "object.delete",
		Backend: chi.URLParam(r, "bid"),
		Bucket:  bucket,
		Key:     key,
	})
	w.WriteHeader(http.StatusNoContent)
}

type bulkDeleteRequest struct {
	Keys []backend.ObjectIdentifier `json:"keys"`
}

func (d *BackendDeps) handleBulkDelete(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	var req bulkDeleteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	if len(req.Keys) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "keys is required", "")
		return
	}
	res, err := b.DeleteObjects(r.Context(), bucket, req.Keys)
	if err != nil {
		d.backendError(w, r, err, "bulk delete")
		return
	}
	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "object.bulk_delete",
		Backend: chi.URLParam(r, "bid"),
		Bucket:  bucket,
		Detail: map[string]any{
			"requested": len(req.Keys),
			"deleted":   len(res.Deleted),
			"errors":    len(res.Errors),
		},
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"deleted": res.Deleted,
		"errors":  res.Errors,
	})
}

type copyObjectRequest struct {
	SrcKey    string `json:"src_key"`
	DstKey    string `json:"dst_key"`
	DstBucket string `json:"dst_bucket,omitempty"`
	// DstBackend, when set and different from the source backend, switches
	// the handler into stream-through-proxy mode (Phase 8). When empty or
	// equal to the source backend the SDK-level CopyObject is used, which
	// is faster because the bytes never leave the backend.
	DstBackend string `json:"dst_backend,omitempty"`
	VersionID  string `json:"version_id,omitempty"`
	// Metadata: nil preserves source metadata (COPY directive); a non-nil
	// map — even empty — replaces it (REPLACE directive).
	Metadata map[string]string `json:"metadata,omitempty"`
}

func (d *BackendDeps) handleCopyObject(w http.ResponseWriter, r *http.Request) {
	srcB, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	srcBackendID := chi.URLParam(r, "bid")
	srcBucket := chi.URLParam(r, "bucket")

	var req copyObjectRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	if req.SrcKey == "" || req.DstKey == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "src_key and dst_key are required", "")
		return
	}
	if !validObjectKey(req.SrcKey) || !validObjectKey(req.DstKey) {
		writeError(w, http.StatusBadRequest, "invalid_key", "object key is invalid", "")
		return
	}
	dstBucket := req.DstBucket
	if dstBucket == "" {
		dstBucket = srcBucket
	} else if !validBucketName(dstBucket) {
		writeError(w, http.StatusBadRequest, "invalid_bucket", "destination bucket name is invalid", "")
		return
	}
	dstBackendID := req.DstBackend
	if dstBackendID == "" {
		dstBackendID = srcBackendID
	}
	crossBackend := dstBackendID != srcBackendID
	if dstBackendID == srcBackendID && dstBucket == srcBucket && req.DstKey == req.SrcKey {
		writeError(w, http.StatusBadRequest, "bad_request", "source and destination are identical", "")
		return
	}

	if crossBackend {
		d.copyAcrossBackends(w, r, srcB, srcBackendID, srcBucket, dstBackendID, dstBucket, req)
		return
	}

	// Fast path — both endpoints on the same backend, the SDK's CopyObject
	// keeps bytes server-side.
	src := backend.ObjectRef{Bucket: srcBucket, Key: req.SrcKey, VersionID: req.VersionID}
	dst := backend.ObjectRef{Bucket: dstBucket, Key: req.DstKey}
	if err := srcB.CopyObject(r.Context(), src, dst, req.Metadata); err != nil {
		d.backendError(w, r, err, "copy object")
		return
	}
	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "object.copy",
		Backend: srcBackendID,
		Bucket:  srcBucket,
		Key:     req.SrcKey,
		Detail: map[string]any{
			"dst_bucket": dstBucket,
			"dst_key":    req.DstKey,
		},
	})
	d.respondCopyResult(w, r, srcB, dstBucket, req.DstKey)
}

// copyAcrossBackends handles src≠dst by streaming bytes through the proxy:
// HeadObject(src) for size+content-type → GetObject(src) → PutObject(dst).
// Quota-checked on the destination; recorded on success.
func (d *BackendDeps) copyAcrossBackends(
	w http.ResponseWriter,
	r *http.Request,
	srcB backend.Backend,
	srcBackendID, srcBucket, dstBackendID, dstBucket string,
	req copyObjectRequest,
) {
	dstB, ok := d.Registry.Get(dstBackendID)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "destination backend not found", "")
		return
	}

	info, err := srcB.HeadObject(r.Context(), srcBucket, req.SrcKey, req.VersionID)
	if err != nil {
		d.backendError(w, r, err, "head src for transfer")
		return
	}
	if d.Quotas != nil {
		if err := d.Quotas.CheckUpload(r.Context(), dstBackendID, dstBucket, info.Size); err != nil {
			d.writeQuotaError(w, err)
			return
		}
	}

	reader, err := srcB.GetObject(r.Context(), srcBucket, req.SrcKey, req.VersionID, nil)
	if err != nil {
		d.backendError(w, r, err, "get src for transfer")
		return
	}
	defer reader.Close()

	put, err := dstB.PutObject(r.Context(), backend.PutObjectRequest{
		Bucket:      dstBucket,
		Key:         req.DstKey,
		Body:        reader,
		Size:        info.Size,
		ContentType: info.ContentType,
		Metadata:    req.Metadata,
	})
	if err != nil {
		d.backendError(w, r, err, "put dst for transfer")
		return
	}
	if d.Quotas != nil {
		d.Quotas.Recorded(dstBackendID, dstBucket, put.Size)
	}
	if d.Sizes != nil {
		d.Sizes.Recorded(dstBackendID, dstBucket, put.Size)
	}
	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "object.transfer",
		Backend: srcBackendID,
		Bucket:  srcBucket,
		Key:     req.SrcKey,
		Detail: map[string]any{
			"dst_backend": dstBackendID,
			"dst_bucket":  dstBucket,
			"dst_key":     req.DstKey,
			"size":        info.Size,
		},
	})
	dto := toObjectDTO(put)
	writeJSON(w, http.StatusOK, map[string]any{
		"backend": dstBackendID,
		"bucket":  dstBucket,
		"object":  dto,
	})
}

// respondCopyResult HEAD's the destination so the UI can update its row
// without a follow-up listing call. Same-backend path only.
func (d *BackendDeps) respondCopyResult(
	w http.ResponseWriter,
	r *http.Request,
	dstB backend.Backend,
	dstBucket, dstKey string,
) {
	info, err := dstB.HeadObject(r.Context(), dstBucket, dstKey, "")
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"bucket": dstBucket,
			"key":    dstKey,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"bucket": dstBucket,
		"object": toObjectDTO(info),
	})
}

// ---- Prefix copy / delete ----------------------------------------------

type copyPrefixRequest struct {
	SrcPrefix  string            `json:"src_prefix"`
	DstPrefix  string            `json:"dst_prefix"`
	DstBucket  string            `json:"dst_bucket,omitempty"`
	DstBackend string            `json:"dst_backend,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// validPrefix accepts either the bucket root ("") or a non-empty key that
// ends in "/" and otherwise satisfies validObjectKey.
func validPrefix(p string) bool {
	if p == "" {
		return true
	}
	if !strings.HasSuffix(p, "/") {
		return false
	}
	return validObjectKey(p)
}

// handleCopyPrefix recursively copies every object under src_prefix to
// dst_prefix, streaming an NDJSON event log so the UI can render progress
// instead of waiting for a single response. Same-backend pages stay
// server-side via the SDK CopyObject; cross-backend pages stream through.
//
// Partial failure is tolerated: a failed object emits an "error" event and
// the walk continues. The terminal "done" event reports totals. Source
// objects are never deleted by this handler — moves are copy-prefix +
// delete-prefix, sequenced by the caller.
func (d *BackendDeps) handleCopyPrefix(w http.ResponseWriter, r *http.Request) {
	srcB, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	srcBackendID := chi.URLParam(r, "bid")
	srcBucket := chi.URLParam(r, "bucket")

	var req copyPrefixRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	if !validPrefix(req.SrcPrefix) || !validPrefix(req.DstPrefix) {
		writeError(w, http.StatusBadRequest, "invalid_prefix", "src_prefix and dst_prefix must be empty or end with '/'", "")
		return
	}
	dstBucket := req.DstBucket
	if dstBucket == "" {
		dstBucket = srcBucket
	} else if !validBucketName(dstBucket) {
		writeError(w, http.StatusBadRequest, "invalid_bucket", "destination bucket name is invalid", "")
		return
	}
	dstBackendID := req.DstBackend
	if dstBackendID == "" {
		dstBackendID = srcBackendID
	}

	// Reject self-copy and source-inside-destination loops on the same
	// backend+bucket. dst==src is a no-op; dst nested under src would
	// re-list newly-written keys forever.
	if dstBackendID == srcBackendID && dstBucket == srcBucket {
		if req.DstPrefix == req.SrcPrefix || strings.HasPrefix(req.DstPrefix, req.SrcPrefix) {
			writeError(w, http.StatusBadRequest, "bad_request",
				"destination prefix must be outside the source prefix on the same bucket", "")
			return
		}
	}

	dstB := srcB
	if dstBackendID != srcBackendID {
		got, gotOk := d.Registry.Get(dstBackendID)
		if !gotOk {
			writeError(w, http.StatusNotFound, "not_found", "destination backend not found", "")
			return
		}
		dstB = got
	}

	// Switch into NDJSON streaming mode. The body is a sequence of single-line
	// JSON events; the client reads each line and updates progress.
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	emit := func(ev map[string]any) {
		_ = enc.Encode(ev)
		if flusher != nil {
			flusher.Flush()
		}
	}

	emit(map[string]any{"event": "start", "src_prefix": req.SrcPrefix, "dst_prefix": req.DstPrefix})

	var (
		copied   int64
		failed   int64
		bytesSum int64
		token    string
	)

	crossBackend := dstBackendID != srcBackendID

	for {
		if r.Context().Err() != nil {
			emit(map[string]any{"event": "done", "copied": copied, "failed": failed,
				"bytes": bytesSum, "cancelled": true})
			return
		}
		res, err := srcB.ListObjects(r.Context(), backend.ListObjectsRequest{
			Bucket:            srcBucket,
			Prefix:            req.SrcPrefix,
			Delimiter:         "",
			ContinuationToken: token,
		})
		if err != nil {
			d.Logger.Warn("copy-prefix: list failed",
				"backend", srcBackendID, "bucket", srcBucket, "prefix", req.SrcPrefix, "err", err)
			emit(map[string]any{"event": "error", "code": "list_failed",
				"message": "listing source prefix failed"})
			emit(map[string]any{"event": "done", "copied": copied, "failed": failed,
				"bytes": bytesSum})
			return
		}
		for _, obj := range res.Objects {
			if r.Context().Err() != nil {
				emit(map[string]any{"event": "done", "copied": copied, "failed": failed,
					"bytes": bytesSum, "cancelled": true})
				return
			}
			rel := strings.TrimPrefix(obj.Key, req.SrcPrefix)
			dstKey := req.DstPrefix + rel
			// Skip the placeholder object when copying to the bucket root —
			// dst_prefix="" would yield an empty key, which is invalid.
			if dstKey == "" {
				continue
			}
			if !validObjectKey(dstKey) {
				failed++
				emit(map[string]any{"event": "error", "src": obj.Key, "dst": dstKey,
					"code": "invalid_key", "message": "rewritten destination key is invalid"})
				continue
			}
			if err := d.copyOnePrefix(r, srcB, dstB, srcBackendID, dstBackendID,
				srcBucket, dstBucket, obj, dstKey, req.Metadata, crossBackend); err != nil {
				failed++
				emit(map[string]any{"event": "error", "src": obj.Key, "dst": dstKey,
					"code": err.code, "message": err.msg})
				continue
			}
			copied++
			bytesSum += obj.Size
			emit(map[string]any{"event": "object", "src": obj.Key, "dst": dstKey, "size": obj.Size})
		}
		if !res.IsTruncated || res.NextContinuationToken == "" {
			break
		}
		token = res.NextContinuationToken
	}

	action := "object.copy_prefix"
	if crossBackend {
		action = "object.transfer_prefix"
	}
	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  action,
		Backend: srcBackendID,
		Bucket:  srcBucket,
		Detail: map[string]any{
			"src_prefix":  req.SrcPrefix,
			"dst_prefix":  req.DstPrefix,
			"dst_backend": dstBackendID,
			"dst_bucket":  dstBucket,
			"copied":      copied,
			"failed":      failed,
			"bytes":       bytesSum,
		},
	})
	emit(map[string]any{"event": "done", "copied": copied, "failed": failed, "bytes": bytesSum})
}

// prefixCopyErr is the typed error returned by copyOnePrefix so the streaming
// handler can attach a stable error code to the NDJSON event.
type prefixCopyErr struct {
	code string
	msg  string
}

func (e *prefixCopyErr) Error() string { return e.msg }

// copyOnePrefix dispatches a single object to either the same-backend
// CopyObject fast path or the cross-backend stream-through. Quota checks are
// applied (and recorded) on cross-backend copies only — same-backend doesn't
// change cumulative bytes on the destination.
func (d *BackendDeps) copyOnePrefix(
	r *http.Request,
	srcB, dstB backend.Backend,
	srcBackendID, dstBackendID, srcBucket, dstBucket string,
	obj backend.ObjectInfo, dstKey string,
	metadata map[string]string,
	crossBackend bool,
) *prefixCopyErr {
	if !crossBackend {
		err := srcB.CopyObject(r.Context(),
			backend.ObjectRef{Bucket: srcBucket, Key: obj.Key},
			backend.ObjectRef{Bucket: dstBucket, Key: dstKey},
			metadata)
		if err != nil {
			d.Logger.Warn("copy-prefix: copy failed",
				"backend", srcBackendID, "bucket", srcBucket, "src", obj.Key, "dst", dstKey, "err", err)
			return &prefixCopyErr{code: "copy_failed", msg: "copy failed"}
		}
		return nil
	}

	if d.Quotas != nil {
		if err := d.Quotas.CheckUpload(r.Context(), dstBackendID, dstBucket, obj.Size); err != nil {
			if errors.Is(err, quotas.ErrQuotaExceeded) {
				return &prefixCopyErr{code: "quota_exceeded", msg: "destination quota exceeded"}
			}
			return &prefixCopyErr{code: "quota_error", msg: "quota check failed"}
		}
	}
	reader, err := srcB.GetObject(r.Context(), srcBucket, obj.Key, "", nil)
	if err != nil {
		d.Logger.Warn("copy-prefix: get failed",
			"backend", srcBackendID, "src", obj.Key, "err", err)
		return &prefixCopyErr{code: "get_failed", msg: "reading source failed"}
	}
	defer reader.Close()
	put, err := dstB.PutObject(r.Context(), backend.PutObjectRequest{
		Bucket:      dstBucket,
		Key:         dstKey,
		Body:        reader,
		Size:        obj.Size,
		ContentType: obj.ContentType,
		Metadata:    metadata,
	})
	if err != nil {
		d.Logger.Warn("copy-prefix: put failed",
			"backend", dstBackendID, "dst", dstKey, "err", err)
		return &prefixCopyErr{code: "put_failed", msg: "writing destination failed"}
	}
	if d.Quotas != nil {
		d.Quotas.Recorded(dstBackendID, dstBucket, put.Size)
	}
	if d.Sizes != nil {
		d.Sizes.Recorded(dstBackendID, dstBucket, put.Size)
	}
	return nil
}

type deletePrefixRequest struct {
	Prefix string `json:"prefix"`
}

// deletePrefixBatchSize matches the S3 DeleteObjects cap. Drivers may permit
// less; the worst case is more roundtrips, never a failure.
const deletePrefixBatchSize = 1000

// handleDeletePrefix walks the source prefix and issues DeleteObjects in
// batches. Like handleCopyPrefix, partial failures are reported per-object
// in the NDJSON stream and the walk continues.
func (d *BackendDeps) handleDeletePrefix(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bid := chi.URLParam(r, "bid")
	bucket := chi.URLParam(r, "bucket")

	var req deletePrefixRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	if !validPrefix(req.Prefix) || req.Prefix == "" {
		// Refuse to drop a whole bucket via this endpoint — the caller can
		// DeleteBucket if that's what they want, with the explicit semantics
		// that come with it.
		writeError(w, http.StatusBadRequest, "invalid_prefix",
			"prefix is required and must end with '/'", "")
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	emit := func(ev map[string]any) {
		_ = enc.Encode(ev)
		if flusher != nil {
			flusher.Flush()
		}
	}

	emit(map[string]any{"event": "start", "prefix": req.Prefix})

	var (
		deleted int64
		failed  int64
		token   string
		batch   []backend.ObjectIdentifier
	)

	flushBatch := func() {
		if len(batch) == 0 {
			return
		}
		res, err := b.DeleteObjects(r.Context(), bucket, batch)
		if err != nil {
			d.Logger.Warn("delete-prefix: batch failed",
				"backend", bid, "bucket", bucket, "prefix", req.Prefix, "err", err)
			failed += int64(len(batch))
			emit(map[string]any{"event": "error", "code": "delete_failed",
				"message": "batch delete failed"})
			batch = batch[:0]
			return
		}
		for _, k := range res.Deleted {
			deleted++
			emit(map[string]any{"event": "object", "key": k.Key})
		}
		for _, e := range res.Errors {
			failed++
			emit(map[string]any{"event": "error", "key": e.Key,
				"code": e.Code, "message": e.Message})
		}
		batch = batch[:0]
	}

	for {
		if r.Context().Err() != nil {
			emit(map[string]any{"event": "done", "deleted": deleted, "failed": failed, "cancelled": true})
			return
		}
		res, err := b.ListObjects(r.Context(), backend.ListObjectsRequest{
			Bucket:            bucket,
			Prefix:            req.Prefix,
			Delimiter:         "",
			ContinuationToken: token,
		})
		if err != nil {
			d.Logger.Warn("delete-prefix: list failed",
				"backend", bid, "bucket", bucket, "prefix", req.Prefix, "err", err)
			emit(map[string]any{"event": "error", "code": "list_failed",
				"message": "listing prefix failed"})
			break
		}
		for _, obj := range res.Objects {
			batch = append(batch, backend.ObjectIdentifier{Key: obj.Key})
			if len(batch) >= deletePrefixBatchSize {
				flushBatch()
			}
		}
		if !res.IsTruncated || res.NextContinuationToken == "" {
			break
		}
		token = res.NextContinuationToken
	}
	flushBatch()

	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "object.delete_prefix",
		Backend: bid,
		Bucket:  bucket,
		Detail: map[string]any{
			"prefix":  req.Prefix,
			"deleted": deleted,
			"failed":  failed,
		},
	})
	emit(map[string]any{"event": "done", "deleted": deleted, "failed": failed})
}

// ---- Versions -----------------------------------------------------------

type versionDTO struct {
	Key            string `json:"key"`
	VersionID      string `json:"version_id"`
	IsLatest       bool   `json:"is_latest"`
	IsDeleteMarker bool   `json:"is_delete_marker,omitempty"`
	Size           int64  `json:"size"`
	ETag           string `json:"etag,omitempty"`
	StorageClass   string `json:"storage_class,omitempty"`
	LastModified   string `json:"last_modified,omitempty"`
}

func toVersionDTO(v backend.ObjectVersion) versionDTO {
	dto := versionDTO{
		Key:            v.Key,
		VersionID:      v.VersionID,
		IsLatest:       v.IsLatest,
		IsDeleteMarker: v.IsDeleteMarker,
		Size:           v.Size,
		ETag:           v.ETag,
		StorageClass:   v.StorageClass,
	}
	if !v.LastModified.IsZero() {
		dto.LastModified = v.LastModified.Format(time.RFC3339)
	}
	return dto
}

func (d *BackendDeps) handleListObjectVersions(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	key := r.URL.Query().Get("key")
	if key == "" || !validObjectKey(key) {
		writeError(w, http.StatusBadRequest, "invalid_key", "key query param is required and must be valid", "")
		return
	}
	if !b.Capabilities().Versioning {
		writeError(w, http.StatusNotImplemented, "not_supported", "backend does not support versioning", "")
		return
	}
	raw, err := b.ListObjectVersions(r.Context(), bucket, key)
	if err != nil {
		d.backendError(w, r, err, "list versions")
		return
	}
	// ListObjectVersions is prefix-scoped — filter to exact-key matches so
	// sibling keys like "foo.txt.bak" don't leak into a listing for "foo.txt".
	out := make([]versionDTO, 0, len(raw))
	for _, v := range raw {
		if v.Key != key {
			continue
		}
		out = append(out, toVersionDTO(v))
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": out})
}

// ---- Tags + metadata ----------------------------------------------------

// maxTagBytes caps the combined size of a PUT tags request. S3's own limits
// are 10 tags, 128-char keys, 256-char values — well under this envelope.
const maxTagBytes = 16 << 10
const maxMetadataBytes = 16 << 10

type tagsPayload struct {
	Tags map[string]string `json:"tags"`
}

func (d *BackendDeps) handleGetObjectTags(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	key := r.URL.Query().Get("key")
	if key == "" || !validObjectKey(key) {
		writeError(w, http.StatusBadRequest, "invalid_key", "key query param is required and must be valid", "")
		return
	}
	if !b.Capabilities().Tagging {
		writeError(w, http.StatusNotImplemented, "not_supported", "backend does not support object tagging", "")
		return
	}
	tags, err := b.GetObjectTagging(r.Context(), bucket, key, r.URL.Query().Get("version_id"))
	if err != nil {
		d.backendError(w, r, err, "get tags")
		return
	}
	if tags == nil {
		tags = map[string]string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

func (d *BackendDeps) handlePutObjectTags(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	key := r.URL.Query().Get("key")
	if key == "" || !validObjectKey(key) {
		writeError(w, http.StatusBadRequest, "invalid_key", "key query param is required and must be valid", "")
		return
	}
	if !b.Capabilities().Tagging {
		writeError(w, http.StatusNotImplemented, "not_supported", "backend does not support object tagging", "")
		return
	}
	var req tagsPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTagBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	// S3 caps: 10 tags, key ≤ 128 chars, value ≤ 256 chars. Enforce at the
	// proxy so users get a clean 400 rather than a backend 4xx they can't read.
	if len(req.Tags) > 10 {
		writeError(w, http.StatusBadRequest, "too_many_tags", "at most 10 tags per object", "")
		return
	}
	for k, v := range req.Tags {
		if k == "" || len(k) > 128 || len(v) > 256 {
			writeError(w, http.StatusBadRequest, "invalid_tag", "tag keys must be 1–128 chars and values ≤ 256 chars", "")
			return
		}
	}
	if err := b.SetObjectTagging(r.Context(), bucket, key, r.URL.Query().Get("version_id"), req.Tags); err != nil {
		d.backendError(w, r, err, "set tags")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type updateMetadataRequest struct {
	Metadata map[string]string `json:"metadata"`
}

func (d *BackendDeps) handleUpdateObjectMetadata(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	key := r.URL.Query().Get("key")
	if key == "" || !validObjectKey(key) {
		writeError(w, http.StatusBadRequest, "invalid_key", "key query param is required and must be valid", "")
		return
	}
	var req updateMetadataRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMetadataBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	// S3 user metadata: each key must be ASCII-lowercase-ish. Keep the
	// check minimal — reject control chars and ':'. Drivers further
	// validate at their layer.
	for k, v := range req.Metadata {
		if k == "" {
			writeError(w, http.StatusBadRequest, "invalid_metadata", "metadata keys must be non-empty", "")
			return
		}
		for _, r := range k + v {
			if r < 0x20 || r == 0x7f {
				writeError(w, http.StatusBadRequest, "invalid_metadata", "metadata contains control characters", "")
				return
			}
		}
	}
	if err := b.UpdateObjectMetadata(r.Context(), bucket, key, req.Metadata); err != nil {
		d.backendError(w, r, err, "update metadata")
		return
	}
	// Return the fresh HEAD so the UI can swap to the new state without a
	// second roundtrip. Only version-id "" here — the self-copy yields a
	// new VersionID on versioned buckets, and HeadObject without one
	// returns the latest.
	info, err := b.HeadObject(r.Context(), bucket, key, "")
	if err != nil {
		// Treat update as success; HEAD failure is informational.
		writeJSON(w, http.StatusOK, map[string]any{"key": key})
		return
	}
	writeJSON(w, http.StatusOK, toObjectDTO(info))
}

type createFolderRequest struct {
	Key string `json:"key"`
}

func (d *BackendDeps) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	var req createFolderRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	key := req.Key
	if key == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "key is required", "")
		return
	}
	if !strings.HasSuffix(key, "/") {
		key += "/"
	}
	if !validObjectKey(key) {
		writeError(w, http.StatusBadRequest, "invalid_key", "folder key is invalid", "")
		return
	}
	info, err := b.PutObject(r.Context(), backend.PutObjectRequest{
		Bucket: bucket,
		Key:    key,
		Body:   strings.NewReader(""),
		Size:   0,
	})
	if err != nil {
		d.backendError(w, r, err, "create folder")
		return
	}
	writeJSON(w, http.StatusCreated, toObjectDTO(info))
}

// maxZipKeys caps the fan-out per zip request. Prevents a single request from
// holding the backend for arbitrarily long via a huge selection. Individual
// prefixes can still expand to many files; this limit applies to the query
// params only.
const maxZipKeys = 1000

// handleZipDownload streams a zip archive of the requested keys. `key` may
// appear multiple times; a trailing-slash value is treated as a prefix and
// recursively expanded (delimiter="").
//
// Errors mid-stream are logged and the offending entry is skipped — we're
// already committed to writing a 200 response, so the alternative is a broken
// download with no explanation. Partial archives are well-formed (the central
// directory is written by zip.Writer.Close).
func (d *BackendDeps) handleZipDownload(w http.ResponseWriter, r *http.Request) {
	b, ok := d.resolveBackend(w, r)
	if !ok {
		return
	}
	bucket := chi.URLParam(r, "bucket")
	keys := r.URL.Query()["key"]
	if len(keys) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "at least one key query param is required", "")
		return
	}
	if len(keys) > maxZipKeys {
		writeError(w, http.StatusRequestEntityTooLarge, "too_many_keys",
			fmt.Sprintf("at most %d keys per zip request", maxZipKeys), "")
		return
	}
	for _, k := range keys {
		if !validObjectKey(k) {
			writeError(w, http.StatusBadRequest, "invalid_key", "one or more keys are invalid", "")
			return
		}
	}

	filename := fmt.Sprintf("%s-%s.zip", bucket, time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`attachment; filename="%s"`, sanitizeFilename(filename)))
	// Disable buffering by intermediate proxies.
	w.Header().Set("X-Accel-Buffering", "no")

	zw := zip.NewWriter(w)
	defer zw.Close()

	flusher, _ := w.(http.Flusher)

	for _, k := range keys {
		if r.Context().Err() != nil {
			return
		}
		if strings.HasSuffix(k, "/") {
			// Recursive prefix expansion.
			token := ""
			for {
				res, err := b.ListObjects(r.Context(), backend.ListObjectsRequest{
					Bucket:            bucket,
					Prefix:            k,
					Delimiter:         "",
					ContinuationToken: token,
				})
				if err != nil {
					d.Logger.Warn("zip: list failed", "prefix", k, "err", err)
					break
				}
				for _, obj := range res.Objects {
					if r.Context().Err() != nil {
						return
					}
					d.writeZipEntry(r, zw, b, bucket, obj.Key, obj)
				}
				if !res.IsTruncated || res.NextContinuationToken == "" {
					break
				}
				token = res.NextContinuationToken
			}
		} else {
			d.writeZipEntry(r, zw, b, bucket, k, backend.ObjectInfo{})
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// writeZipEntry adds a single object to the archive. `meta` may be zero if
// the caller doesn't have listing metadata; the modified time falls back to
// now in that case.
func (d *BackendDeps) writeZipEntry(
	r *http.Request,
	zw *zip.Writer,
	b backend.Backend,
	bucket, key string,
	meta backend.ObjectInfo,
) {
	reader, err := b.GetObject(r.Context(), bucket, key, "", nil)
	if err != nil {
		d.Logger.Warn("zip: get failed", "key", key, "err", err)
		return
	}
	defer reader.Close()

	hdr := &zip.FileHeader{
		Name:   key,
		Method: zip.Deflate,
	}
	if !meta.LastModified.IsZero() {
		hdr.Modified = meta.LastModified
	} else {
		hdr.Modified = time.Now()
	}
	fw, err := zw.CreateHeader(hdr)
	if err != nil {
		d.Logger.Warn("zip: create header failed", "key", key, "err", err)
		return
	}
	if _, err := io.Copy(fw, reader); err != nil {
		d.Logger.Warn("zip: copy failed", "key", key, "err", err)
		return
	}
}

// ---- helpers ------------------------------------------------------------

func (d *BackendDeps) resolveBackend(w http.ResponseWriter, r *http.Request) (backend.Backend, bool) {
	id := chi.URLParam(r, "bid")
	b, ok := d.Registry.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "backend not found", "")
		return nil, false
	}
	return b, true
}

func (d *BackendDeps) backendError(w http.ResponseWriter, r *http.Request, err error, op string) {
	// Keep error messages shown to users free of raw backend error codes.
	// Full detail goes to the log for operators.
	d.Logger.Warn("backend error", "op", op, "err", err.Error(),
		"backend", chi.URLParam(r, "bid"), "bucket", chi.URLParam(r, "bucket"))
	if s3v4.IsNotFound(err) {
		writeError(w, http.StatusNotFound, "not_found", "resource not found", "")
		return
	}
	writeError(w, http.StatusBadGateway, "backend_error", "backend operation failed", "")
}

// parseRangeHeader parses a subset of the HTTP Range header: bytes=start-end
// or bytes=start-. Anything more exotic is treated as absent.
func parseRangeHeader(h string) *backend.Range {
	if h == "" {
		return nil
	}
	if !strings.HasPrefix(h, "bytes=") {
		return nil
	}
	spec := strings.TrimPrefix(h, "bytes=")
	if strings.Contains(spec, ",") {
		return nil // multi-range not supported here
	}
	i := strings.IndexByte(spec, '-')
	if i < 0 {
		return nil
	}
	startStr, endStr := spec[:i], spec[i+1:]
	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil {
		return nil
	}
	rng := &backend.Range{Start: start, End: -1}
	if endStr != "" {
		if end, err := strconv.ParseInt(endStr, 10, 64); err == nil {
			rng.End = end
		}
	}
	return rng
}

// validBucketName is deliberately conservative — matches the S3 DNS-safe
// rules. Individual backends may permit more; we prefer portable names.
func validBucketName(n string) bool {
	if len(n) < 3 || len(n) > 63 {
		return false
	}
	for i, r := range n {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '.':
			if i == 0 || i == len(n)-1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// validObjectKey enforces §7.5 — no null bytes, no control chars except /,
// length ≤ 1024, no ".." components.
func validObjectKey(key string) bool {
	if key == "" || len(key) > 1024 {
		return false
	}
	for _, r := range key {
		if r == 0 {
			return false
		}
		// Reject C0 controls (except TAB) and DEL. Also reject the Unicode
		// bidirectional override codepoints — they let an attacker render a
		// filename in the UI as "report.pdf" while the bytes on disk are
		// something else, a pure confusion vector with no upside.
		if (r < 0x20 && r != '\t') || r == 0x7f {
			return false
		}
		if isBidiOverride(r) {
			return false
		}
	}
	for _, part := range strings.Split(key, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

// isBidiOverride reports whether r is one of the Unicode codepoints that flip
// rendering direction. Used by validObjectKey and sanitizeFilename.
func isBidiOverride(r rune) bool {
	switch r {
	case 0x202A, 0x202B, 0x202C, 0x202D, 0x202E,
		0x2066, 0x2067, 0x2068, 0x2069:
		return true
	}
	return false
}

func sanitizeFilename(name string) string {
	// Drop quotes and control chars for safe Content-Disposition. Mirrors
	// the validObjectKey rejection set so a pre-existing badly-named object
	// can still be downloaded with a defensible filename.
	var b strings.Builder
	for _, r := range name {
		if r == '"' || r == '\\' || r < 0x20 || r == 0x7f {
			continue
		}
		if isBidiOverride(r) {
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	if out == "" {
		return "download"
	}
	return out
}
