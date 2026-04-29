// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package backend

import (
	"context"
	"errors"
	"io"
	"net/url"
	"testing"
	"time"
)

// stubBackend is a minimal Backend that does *not* implement
// ProxyTargetProvider — used to verify the registry returns ok=false in that
// case rather than panicking.
type stubBackend struct{ id string }

func (s *stubBackend) ID() string                 { return s.id }
func (s *stubBackend) DisplayName() string        { return s.id }
func (s *stubBackend) Capabilities() Capabilities { return Capabilities{} }
func (s *stubBackend) ListBuckets(context.Context) ([]Bucket, error) {
	return nil, nil
}
func (s *stubBackend) CreateBucket(context.Context, string, string) error { return nil }
func (s *stubBackend) DeleteBucket(context.Context, string) error         { return nil }
func (s *stubBackend) HeadBucket(context.Context, string) (BucketInfo, error) {
	return BucketInfo{}, nil
}
func (s *stubBackend) GetBucketVersioning(context.Context, string) (bool, error) {
	return false, nil
}
func (s *stubBackend) SetBucketVersioning(context.Context, string, bool) error { return nil }
func (s *stubBackend) GetBucketCORS(context.Context, string) ([]CORSRule, error) {
	return nil, nil
}
func (s *stubBackend) SetBucketCORS(context.Context, string, []CORSRule) error { return nil }
func (s *stubBackend) GetBucketPolicy(context.Context, string) (string, error) { return "", nil }
func (s *stubBackend) SetBucketPolicy(context.Context, string, string) error   { return nil }
func (s *stubBackend) DeleteBucketPolicy(context.Context, string) error        { return nil }
func (s *stubBackend) GetBucketLifecycle(context.Context, string) ([]LifecycleRule, error) {
	return nil, nil
}
func (s *stubBackend) SetBucketLifecycle(context.Context, string, []LifecycleRule) error {
	return nil
}
func (s *stubBackend) ListObjects(context.Context, ListObjectsRequest) (ListObjectsResult, error) {
	return ListObjectsResult{}, nil
}
func (s *stubBackend) GetObject(context.Context, string, string, string, *Range) (ObjectReader, error) {
	return nil, nil
}
func (s *stubBackend) HeadObject(context.Context, string, string, string) (ObjectInfo, error) {
	return ObjectInfo{}, nil
}
func (s *stubBackend) ListObjectVersions(context.Context, string, string) ([]ObjectVersion, error) {
	return nil, nil
}
func (s *stubBackend) PutObject(context.Context, PutObjectRequest) (ObjectInfo, error) {
	return ObjectInfo{}, nil
}
func (s *stubBackend) DeleteObject(context.Context, string, string, string) error { return nil }
func (s *stubBackend) DeleteObjects(context.Context, string, []ObjectIdentifier) (DeleteObjectsResult, error) {
	return DeleteObjectsResult{}, nil
}
func (s *stubBackend) CopyObject(context.Context, ObjectRef, ObjectRef, map[string]string) error {
	return nil
}
func (s *stubBackend) GetObjectTagging(context.Context, string, string, string) (map[string]string, error) {
	return nil, nil
}
func (s *stubBackend) SetObjectTagging(context.Context, string, string, string, map[string]string) error {
	return nil
}
func (s *stubBackend) UpdateObjectMetadata(context.Context, string, string, map[string]string) error {
	return nil
}
func (s *stubBackend) CreateMultipart(context.Context, string, string, string, map[string]string) (string, error) {
	return "", nil
}
func (s *stubBackend) UploadPart(context.Context, string, string, string, int, io.Reader, int64) (string, error) {
	return "", nil
}
func (s *stubBackend) CompleteMultipart(context.Context, string, string, string, []CompletedPart) (ObjectInfo, error) {
	return ObjectInfo{}, nil
}
func (s *stubBackend) AbortMultipart(context.Context, string, string, string) error {
	return nil
}
func (s *stubBackend) ListMultipartUploads(context.Context, string, string) ([]MultipartUpload, error) {
	return nil, nil
}
func (s *stubBackend) PresignGet(context.Context, string, string, time.Duration) (string, error) {
	return "", nil
}
func (s *stubBackend) PresignPut(context.Context, string, string, time.Duration, string) (string, error) {
	return "", nil
}
func (s *stubBackend) Admin() AdminBackend { return nil }

// providerBackend embeds stubBackend and implements ProxyTargetProvider.
type providerBackend struct {
	stubBackend
	target ProxyTarget
	err    error
}

func (p *providerBackend) ProxyTarget() (ProxyTarget, error) {
	return p.target, p.err
}

func TestRegistry_ProxyTarget_UnknownID(t *testing.T) {
	r := NewRegistry()
	got, found, err := r.ProxyTarget("nope")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if found {
		t.Fatalf("expected found=false for unknown id")
	}
	if got != (ProxyTarget{}) {
		t.Fatalf("expected zero ProxyTarget, got %+v", got)
	}
}

func TestRegistry_ProxyTarget_BackendWithoutProvider(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&stubBackend{id: "plain"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	_, found, err := r.ProxyTarget("plain")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if found {
		t.Fatalf("expected found=false when driver does not implement ProxyTargetProvider")
	}
}

func TestRegistry_ProxyTarget_Happy(t *testing.T) {
	r := NewRegistry()
	u, _ := url.Parse("http://upstream.example.com:9000")
	want := ProxyTarget{
		Endpoint: u, Region: "us-east-1", PathStyle: true,
		AccessKey: "AKIAEXAMPLE", SecretKey: "secret",
	}
	if err := r.Register(&providerBackend{stubBackend: stubBackend{id: "minio"}, target: want}); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, found, err := r.ProxyTarget("minio")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !found {
		t.Fatalf("expected found=true")
	}
	if got.Endpoint.String() != want.Endpoint.String() ||
		got.AccessKey != want.AccessKey ||
		got.SecretKey != want.SecretKey {
		t.Fatalf("ProxyTarget mismatch: got %+v want %+v", got, want)
	}
}

func TestRegistry_ProxyTarget_ProviderError(t *testing.T) {
	r := NewRegistry()
	bErr := errors.New("boom")
	if err := r.Register(&providerBackend{stubBackend: stubBackend{id: "broken"}, err: bErr}); err != nil {
		t.Fatalf("register: %v", err)
	}
	_, found, err := r.ProxyTarget("broken")
	if !found {
		t.Fatalf("expected found=true (driver implements provider)")
	}
	if !errors.Is(err, bErr) {
		t.Fatalf("expected boom error, got %v", err)
	}
}
