// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package memory is an in-process Backend implementation for tests. It
// stores objects in maps and is not safe across process restarts.
package memory

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/stowage-dev/stowage/internal/backend"
)

type Backend struct {
	id   string
	name string

	mu      sync.Mutex
	buckets map[string]*bucketData
}

type bucketData struct {
	CreatedAt time.Time
	Versioned bool
	Policy    string
	CORS      []backend.CORSRule
	Lifecycle []backend.LifecycleRule
	Objects   map[string]*objectData
}

type objectData struct {
	Data        []byte
	ContentType string
	Metadata    map[string]string
	Tags        map[string]string
	ETag        string
	Modified    time.Time
	VersionID   string
}

var _ backend.Backend = (*Backend)(nil)

func New(id, name string) *Backend {
	return &Backend{
		id:      id,
		name:    name,
		buckets: make(map[string]*bucketData),
	}
}

func (b *Backend) ID() string          { return b.id }
func (b *Backend) DisplayName() string { return b.name }

func (b *Backend) Capabilities() backend.Capabilities {
	return backend.Capabilities{
		Versioning: true, Lifecycle: true, BucketPolicy: true, CORS: true,
		Tagging: true, MaxMultipartParts: 10000, MaxPartSizeBytes: 1 << 30,
	}
}

var (
	errNoBucket = errors.New("memory: no such bucket")
	errNoObject = errors.New("memory: no such object")
)

func (b *Backend) ListBuckets(_ context.Context) ([]backend.Bucket, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]backend.Bucket, 0, len(b.buckets))
	for name, data := range b.buckets {
		out = append(out, backend.Bucket{Name: name, CreatedAt: data.CreatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (b *Backend) CreateBucket(_ context.Context, name, _ string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.buckets[name]; exists {
		return errors.New("memory: bucket exists")
	}
	b.buckets[name] = &bucketData{CreatedAt: time.Now().UTC(), Objects: map[string]*objectData{}}
	return nil
}

func (b *Backend) DeleteBucket(_ context.Context, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	bk, ok := b.buckets[name]
	if !ok {
		return errNoBucket
	}
	if len(bk.Objects) > 0 {
		return errors.New("memory: bucket not empty")
	}
	delete(b.buckets, name)
	return nil
}

func (b *Backend) HeadBucket(_ context.Context, name string) (backend.BucketInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.buckets[name]; !ok {
		return backend.BucketInfo{Name: name}, errNoBucket
	}
	return backend.BucketInfo{Name: name, Exists: true}, nil
}

// ---- Bucket config ------------------------------------------------------

func (b *Backend) GetBucketVersioning(_ context.Context, bucket string) (bool, error) {
	bk, err := b.requireBucket(bucket)
	if err != nil {
		return false, err
	}
	return bk.Versioned, nil
}

func (b *Backend) SetBucketVersioning(_ context.Context, bucket string, enabled bool) error {
	bk, err := b.requireBucket(bucket)
	if err != nil {
		return err
	}
	bk.Versioned = enabled
	return nil
}

func (b *Backend) GetBucketCORS(_ context.Context, bucket string) ([]backend.CORSRule, error) {
	bk, err := b.requireBucket(bucket)
	if err != nil {
		return nil, err
	}
	return bk.CORS, nil
}

func (b *Backend) SetBucketCORS(_ context.Context, bucket string, rules []backend.CORSRule) error {
	bk, err := b.requireBucket(bucket)
	if err != nil {
		return err
	}
	bk.CORS = rules
	return nil
}

func (b *Backend) GetBucketPolicy(_ context.Context, bucket string) (string, error) {
	bk, err := b.requireBucket(bucket)
	if err != nil {
		return "", err
	}
	return bk.Policy, nil
}

func (b *Backend) SetBucketPolicy(_ context.Context, bucket, policy string) error {
	bk, err := b.requireBucket(bucket)
	if err != nil {
		return err
	}
	bk.Policy = policy
	return nil
}

func (b *Backend) DeleteBucketPolicy(_ context.Context, bucket string) error {
	bk, err := b.requireBucket(bucket)
	if err != nil {
		return err
	}
	bk.Policy = ""
	return nil
}

func (b *Backend) GetBucketLifecycle(_ context.Context, bucket string) ([]backend.LifecycleRule, error) {
	bk, err := b.requireBucket(bucket)
	if err != nil {
		return nil, err
	}
	return bk.Lifecycle, nil
}

func (b *Backend) SetBucketLifecycle(_ context.Context, bucket string, rules []backend.LifecycleRule) error {
	bk, err := b.requireBucket(bucket)
	if err != nil {
		return err
	}
	bk.Lifecycle = rules
	return nil
}

// ---- Object ops ---------------------------------------------------------

func (b *Backend) ListObjects(_ context.Context, req backend.ListObjectsRequest) (backend.ListObjectsResult, error) {
	bk, err := b.requireBucket(req.Bucket)
	if err != nil {
		return backend.ListObjectsResult{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	keys := make([]string, 0, len(bk.Objects))
	for k := range bk.Objects {
		if strings.HasPrefix(k, req.Prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	var res backend.ListObjectsResult
	prefixSet := map[string]struct{}{}
	for _, k := range keys {
		if req.Delimiter != "" {
			rest := strings.TrimPrefix(k, req.Prefix)
			if i := strings.Index(rest, req.Delimiter); i >= 0 {
				cp := req.Prefix + rest[:i+len(req.Delimiter)]
				if _, seen := prefixSet[cp]; !seen {
					prefixSet[cp] = struct{}{}
					res.CommonPrefixes = append(res.CommonPrefixes, cp)
				}
				continue
			}
		}
		o := bk.Objects[k]
		res.Objects = append(res.Objects, backend.ObjectInfo{
			Key:          k,
			Size:         int64(len(o.Data)),
			ETag:         o.ETag,
			ContentType:  o.ContentType,
			LastModified: o.Modified,
			Metadata:     o.Metadata,
		})
	}
	return res, nil
}

func (b *Backend) GetObject(_ context.Context, bucket, key, _ string, rng *backend.Range) (backend.ObjectReader, error) {
	bk, err := b.requireBucket(bucket)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	o, ok := bk.Objects[key]
	b.mu.Unlock()
	if !ok {
		return nil, errNoObject
	}
	data := o.Data
	if rng != nil {
		end := int64(len(data)) - 1
		if rng.End >= 0 && rng.End < end {
			end = rng.End
		}
		if rng.Start < 0 || rng.Start > end {
			return nil, errors.New("memory: range not satisfiable")
		}
		data = data[rng.Start : end+1]
	}
	return &reader{
		ReadCloser: io.NopCloser(bytes.NewReader(data)),
		info: backend.ObjectInfo{
			Key:          key,
			Size:         int64(len(data)),
			ETag:         o.ETag,
			ContentType:  o.ContentType,
			LastModified: o.Modified,
			Metadata:     o.Metadata,
		},
	}, nil
}

func (b *Backend) HeadObject(_ context.Context, bucket, key, _ string) (backend.ObjectInfo, error) {
	bk, err := b.requireBucket(bucket)
	if err != nil {
		return backend.ObjectInfo{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	o, ok := bk.Objects[key]
	if !ok {
		return backend.ObjectInfo{}, errNoObject
	}
	return backend.ObjectInfo{
		Key:          key,
		Size:         int64(len(o.Data)),
		ETag:         o.ETag,
		ContentType:  o.ContentType,
		LastModified: o.Modified,
		Metadata:     o.Metadata,
	}, nil
}

func (b *Backend) PutObject(_ context.Context, req backend.PutObjectRequest) (backend.ObjectInfo, error) {
	bk, err := b.requireBucket(req.Bucket)
	if err != nil {
		return backend.ObjectInfo{}, err
	}
	data, err := io.ReadAll(req.Body)
	if err != nil {
		return backend.ObjectInfo{}, err
	}
	now := time.Now().UTC()
	o := &objectData{
		Data:        data,
		ContentType: req.ContentType,
		Metadata:    req.Metadata,
		ETag:        fakeETag(data),
		Modified:    now,
	}
	b.mu.Lock()
	bk.Objects[req.Key] = o
	b.mu.Unlock()
	return backend.ObjectInfo{
		Key: req.Key, Size: int64(len(data)), ETag: o.ETag, ContentType: o.ContentType,
		LastModified: now, Metadata: req.Metadata,
	}, nil
}

func (b *Backend) DeleteObject(_ context.Context, bucket, key, _ string) error {
	bk, err := b.requireBucket(bucket)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(bk.Objects, key)
	return nil
}

func (b *Backend) DeleteObjects(ctx context.Context, bucket string, keys []backend.ObjectIdentifier) (backend.DeleteObjectsResult, error) {
	var res backend.DeleteObjectsResult
	for _, k := range keys {
		if err := b.DeleteObject(ctx, bucket, k.Key, k.VersionID); err != nil {
			res.Errors = append(res.Errors, backend.DeleteError{Key: k.Key, Message: err.Error()})
			continue
		}
		res.Deleted = append(res.Deleted, k)
	}
	return res, nil
}

func (b *Backend) CopyObject(_ context.Context, src, dst backend.ObjectRef, metadata map[string]string) error {
	sb, err := b.requireBucket(src.Bucket)
	if err != nil {
		return err
	}
	db, err := b.requireBucket(dst.Bucket)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	so, ok := sb.Objects[src.Key]
	if !ok {
		return errNoObject
	}
	cp := *so
	if metadata != nil {
		cp.Metadata = metadata
	}
	cp.Modified = time.Now().UTC()
	db.Objects[dst.Key] = &cp
	return nil
}

func (b *Backend) GetObjectTagging(_ context.Context, bucket, key, _ string) (map[string]string, error) {
	bk, err := b.requireBucket(bucket)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	o, ok := bk.Objects[key]
	if !ok {
		return nil, errNoObject
	}
	out := make(map[string]string, len(o.Tags))
	for k, v := range o.Tags {
		out[k] = v
	}
	return out, nil
}

func (b *Backend) SetObjectTagging(_ context.Context, bucket, key, _ string, tags map[string]string) error {
	bk, err := b.requireBucket(bucket)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	o, ok := bk.Objects[key]
	if !ok {
		return errNoObject
	}
	if len(tags) == 0 {
		o.Tags = nil
		return nil
	}
	cp := make(map[string]string, len(tags))
	for k, v := range tags {
		cp[k] = v
	}
	o.Tags = cp
	return nil
}

// ListObjectVersions mimics the single-version-per-key reality of the
// in-memory store: each matching object is returned as a single "latest"
// row. That's enough for tests that care about shape; full history tracking
// would require teaching the store to keep prior bytes around.
func (b *Backend) ListObjectVersions(_ context.Context, bucket, keyPrefix string) ([]backend.ObjectVersion, error) {
	bk, err := b.requireBucket(bucket)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []backend.ObjectVersion
	for k, o := range bk.Objects {
		if !strings.HasPrefix(k, keyPrefix) {
			continue
		}
		out = append(out, backend.ObjectVersion{
			Key:          k,
			VersionID:    o.VersionID,
			IsLatest:     true,
			Size:         int64(len(o.Data)),
			ETag:         o.ETag,
			LastModified: o.Modified,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (b *Backend) UpdateObjectMetadata(_ context.Context, bucket, key string, metadata map[string]string) error {
	bk, err := b.requireBucket(bucket)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	o, ok := bk.Objects[key]
	if !ok {
		return errNoObject
	}
	if metadata == nil {
		o.Metadata = nil
	} else {
		cp := make(map[string]string, len(metadata))
		for k, v := range metadata {
			cp[k] = v
		}
		o.Metadata = cp
	}
	o.Modified = time.Now().UTC()
	return nil
}

// ---- Multipart — skeleton (enough for tests) ----------------------------

type mpUpload struct {
	bucket, key string
	contentType string
	parts       map[int][]byte
	metadata    map[string]string
}

var mpUploads = struct {
	sync.Mutex
	m map[string]*mpUpload
}{m: map[string]*mpUpload{}}

func (b *Backend) CreateMultipart(_ context.Context, bucket, key, contentType string, metadata map[string]string) (string, error) {
	if _, err := b.requireBucket(bucket); err != nil {
		return "", err
	}
	mpUploads.Lock()
	defer mpUploads.Unlock()
	uid := bucket + ":" + key + ":" + time.Now().Format(time.RFC3339Nano)
	mpUploads.m[uid] = &mpUpload{bucket: bucket, key: key, contentType: contentType, parts: map[int][]byte{}, metadata: metadata}
	return uid, nil
}

func (b *Backend) UploadPart(_ context.Context, _, _ string, uploadID string, part int, r io.Reader, _ int64) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	mpUploads.Lock()
	defer mpUploads.Unlock()
	u, ok := mpUploads.m[uploadID]
	if !ok {
		return "", errors.New("memory: no such upload")
	}
	u.parts[part] = data
	return fakeETag(data), nil
}

func (b *Backend) CompleteMultipart(ctx context.Context, _, _, uploadID string, parts []backend.CompletedPart) (backend.ObjectInfo, error) {
	mpUploads.Lock()
	u, ok := mpUploads.m[uploadID]
	if !ok {
		mpUploads.Unlock()
		return backend.ObjectInfo{}, errors.New("memory: no such upload")
	}
	delete(mpUploads.m, uploadID)
	mpUploads.Unlock()

	sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
	var all bytes.Buffer
	for _, p := range parts {
		all.Write(u.parts[p.PartNumber])
	}
	return b.PutObject(ctx, backend.PutObjectRequest{
		Bucket:      u.bucket,
		Key:         u.key,
		Body:        &all,
		Size:        int64(all.Len()),
		ContentType: u.contentType,
		Metadata:    u.metadata,
	})
}

func (b *Backend) AbortMultipart(_ context.Context, _, _, uploadID string) error {
	mpUploads.Lock()
	defer mpUploads.Unlock()
	delete(mpUploads.m, uploadID)
	return nil
}

func (b *Backend) ListMultipartUploads(_ context.Context, bucket, prefix string) ([]backend.MultipartUpload, error) {
	mpUploads.Lock()
	defer mpUploads.Unlock()
	var out []backend.MultipartUpload
	for uid, u := range mpUploads.m {
		if u.bucket != bucket {
			continue
		}
		if prefix != "" && !strings.HasPrefix(u.key, prefix) {
			continue
		}
		out = append(out, backend.MultipartUpload{Key: u.key, UploadID: uid})
	}
	return out, nil
}

// ---- Presign / Admin — not meaningful for memory backend ----------------

func (b *Backend) PresignGet(_ context.Context, _, _ string, _ time.Duration) (string, error) {
	return "", errors.New("memory: presign not supported")
}
func (b *Backend) PresignPut(_ context.Context, _, _ string, _ time.Duration, _ string) (string, error) {
	return "", errors.New("memory: presign not supported")
}
func (b *Backend) Admin() backend.AdminBackend { return nil }

// ---- helpers ------------------------------------------------------------

func (b *Backend) requireBucket(name string) (*bucketData, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	bk, ok := b.buckets[name]
	if !ok {
		return nil, errNoBucket
	}
	return bk, nil
}

type reader struct {
	io.ReadCloser
	info backend.ObjectInfo
}

func (r *reader) Info() backend.ObjectInfo { return r.info }

func fakeETag(data []byte) string {
	// 16-char hex of sum of bytes mod 2^64 — stable enough for tests, not
	// meant to match real MD5 semantics.
	var sum uint64
	for _, b := range data {
		sum = sum*131 + uint64(b)
	}
	const hex = "0123456789abcdef"
	out := make([]byte, 16)
	for i := 15; i >= 0; i-- {
		out[i] = hex[sum&0xf]
		sum >>= 4
	}
	return `"` + string(out) + `"`
}
