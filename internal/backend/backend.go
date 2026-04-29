// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package backend defines the single abstraction through which all S3 access
// flows — this is the most important file in the project. Concrete
// implementations live in sibling sub-packages (s3v4, minio, garage,
// seaweedfs, memory).
package backend

import (
	"context"
	"io"
	"time"
)

// Backend is the per-target S3-compatible driver.
type Backend interface {
	// Identity
	ID() string
	DisplayName() string
	Capabilities() Capabilities

	// Bucket ops
	ListBuckets(ctx context.Context) ([]Bucket, error)
	CreateBucket(ctx context.Context, name, region string) error
	DeleteBucket(ctx context.Context, name string) error
	HeadBucket(ctx context.Context, name string) (BucketInfo, error)

	// Bucket config
	GetBucketVersioning(ctx context.Context, bucket string) (bool, error)
	SetBucketVersioning(ctx context.Context, bucket string, enabled bool) error
	GetBucketCORS(ctx context.Context, bucket string) ([]CORSRule, error)
	SetBucketCORS(ctx context.Context, bucket string, rules []CORSRule) error
	GetBucketPolicy(ctx context.Context, bucket string) (string, error)
	SetBucketPolicy(ctx context.Context, bucket string, policy string) error
	DeleteBucketPolicy(ctx context.Context, bucket string) error
	GetBucketLifecycle(ctx context.Context, bucket string) ([]LifecycleRule, error)
	SetBucketLifecycle(ctx context.Context, bucket string, rules []LifecycleRule) error

	// Object ops
	ListObjects(ctx context.Context, req ListObjectsRequest) (ListObjectsResult, error)
	GetObject(ctx context.Context, bucket, key, versionID string, rng *Range) (ObjectReader, error)
	HeadObject(ctx context.Context, bucket, key, versionID string) (ObjectInfo, error)
	// ListObjectVersions returns versions and delete markers under keyPrefix.
	// Pass the exact object key as keyPrefix to list a single object's
	// history — callers must filter to exact key matches if they want to
	// avoid sibling-prefix contamination.
	ListObjectVersions(ctx context.Context, bucket, keyPrefix string) ([]ObjectVersion, error)
	PutObject(ctx context.Context, req PutObjectRequest) (ObjectInfo, error)
	DeleteObject(ctx context.Context, bucket, key, versionID string) error
	DeleteObjects(ctx context.Context, bucket string, keys []ObjectIdentifier) (DeleteObjectsResult, error)
	CopyObject(ctx context.Context, src, dst ObjectRef, metadata map[string]string) error

	// Object tags and user metadata. Drivers MAY return a "not supported"
	// error if the backend advertises Capabilities.Tagging == false.
	//
	// SetObjectTagging with an empty map removes all tags (on S3: mapped to
	// DeleteObjectTagging). UpdateObjectMetadata is an in-place rewrite of
	// the user metadata — on S3 it's a self-copy with MetadataDirective =
	// REPLACE, which creates a new version on versioned buckets.
	GetObjectTagging(ctx context.Context, bucket, key, versionID string) (map[string]string, error)
	SetObjectTagging(ctx context.Context, bucket, key, versionID string, tags map[string]string) error
	UpdateObjectMetadata(ctx context.Context, bucket, key string, metadata map[string]string) error

	// Multipart
	CreateMultipart(ctx context.Context, bucket, key, contentType string, metadata map[string]string) (uploadID string, err error)
	UploadPart(ctx context.Context, bucket, key, uploadID string, partNum int, r io.Reader, size int64) (etag string, err error)
	CompleteMultipart(ctx context.Context, bucket, key, uploadID string, parts []CompletedPart) (ObjectInfo, error)
	AbortMultipart(ctx context.Context, bucket, key, uploadID string) error
	ListMultipartUploads(ctx context.Context, bucket, prefix string) ([]MultipartUpload, error)

	// Presigned URLs
	PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (string, error)
	PresignPut(ctx context.Context, bucket, key string, ttl time.Duration, contentType string) (string, error)

	// Optional admin interface. May return nil.
	Admin() AdminBackend
}

// Capabilities advertises what a backend can do. The UI uses this to hide
// features the backend does not support.
type Capabilities struct {
	Versioning        bool `json:"versioning"`
	ObjectLock        bool `json:"object_lock"`
	Lifecycle         bool `json:"lifecycle"`
	BucketPolicy      bool `json:"bucket_policy"`
	CORS              bool `json:"cors"`
	Tagging           bool `json:"tagging"`
	ServerSideEncrypt bool `json:"server_side_encrypt"`
	// AdminAPI is "" when no backend-native admin API is available. Known
	// values: "minio", "garage", "seaweedfs".
	AdminAPI          string `json:"admin_api"`
	MaxMultipartParts int    `json:"max_multipart_parts"`
	MaxPartSizeBytes  int64  `json:"max_part_size_bytes"`
}

// AdminBackend is optional and provides backend-native admin features.
// Proxy-layer features (sharing, audit, quotas) are handled separately and
// work regardless of whether Admin() returns nil.
type AdminBackend interface {
	ListUsers(ctx context.Context) ([]User, error)
	CreateUser(ctx context.Context, name string) (accessKey, secretKey string, err error)
	DeleteUser(ctx context.Context, name string) error
	ListAccessKeys(ctx context.Context, user string) ([]AccessKey, error)
	CreateAccessKey(ctx context.Context, user string, opts AccessKeyOpts) (AccessKey, error)
	RevokeAccessKey(ctx context.Context, user, accessKeyID string) error
	ListPolicies(ctx context.Context) ([]Policy, error)
	GetPolicy(ctx context.Context, name string) (Policy, error)
	PutPolicy(ctx context.Context, name, document string) error
	AttachPolicy(ctx context.Context, user, policy string) error
}
