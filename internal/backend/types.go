// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package backend

import (
	"io"
	"time"
)

type Bucket struct {
	Name      string
	CreatedAt time.Time
}

type BucketInfo struct {
	Name   string
	Region string
	Exists bool
}

type CORSRule struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
	ExposeHeaders  []string
	MaxAgeSeconds  int
}

type LifecycleRule struct {
	ID                     string
	Prefix                 string
	Enabled                bool
	ExpirationDays         int
	NoncurrentExpireDays   int
	AbortIncompleteDays    int
	TransitionDays         int
	TransitionStorageClass string
}

type ListObjectsRequest struct {
	Bucket            string
	Prefix            string
	Delimiter         string
	ContinuationToken string
	MaxKeys           int
}

type ListObjectsResult struct {
	Objects               []ObjectInfo
	CommonPrefixes        []string
	NextContinuationToken string
	IsTruncated           bool
}

type ObjectInfo struct {
	Key          string
	Size         int64
	ETag         string
	ContentType  string
	StorageClass string
	VersionID    string
	LastModified time.Time
	Metadata     map[string]string
}

// ObjectReader is returned from GetObject. Callers must Close.
type ObjectReader interface {
	io.ReadCloser
	Info() ObjectInfo
}

type Range struct {
	Start, End int64 // inclusive. End<0 means "to the end".
}

type PutObjectRequest struct {
	Bucket      string
	Key         string
	Body        io.Reader
	Size        int64
	ContentType string
	Metadata    map[string]string
}

type ObjectIdentifier struct {
	Key       string `json:"key"`
	VersionID string `json:"version_id,omitempty"`
}

type DeleteObjectsResult struct {
	Deleted []ObjectIdentifier `json:"deleted"`
	Errors  []DeleteError      `json:"errors,omitempty"`
}

type DeleteError struct {
	Key       string `json:"key"`
	VersionID string `json:"version_id,omitempty"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
}

type ObjectRef struct {
	Bucket    string
	Key       string
	VersionID string
}

type CompletedPart struct {
	PartNumber int
	ETag       string
}

type MultipartUpload struct {
	Key       string
	UploadID  string
	Initiated time.Time
}

// ObjectVersion describes one row from ListObjectVersions. Covers both
// regular versions and delete markers (IsDeleteMarker=true; Size/ETag zero).
type ObjectVersion struct {
	Key            string
	VersionID      string
	IsLatest       bool
	IsDeleteMarker bool
	Size           int64
	ETag           string
	StorageClass   string
	LastModified   time.Time
}

// Admin-side types.

type User struct {
	Name      string
	Enabled   bool
	CreatedAt time.Time
}

type AccessKey struct {
	ID        string
	User      string
	Enabled   bool
	CreatedAt time.Time
	ExpiresAt *time.Time
}

type AccessKeyOpts struct {
	ExpiresAt *time.Time
}

type Policy struct {
	Name     string
	Document string // JSON
}
