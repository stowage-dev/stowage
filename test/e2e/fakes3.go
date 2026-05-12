// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// FakeS3 is a tiny S3-shaped HTTP server that satisfies the bucket-level
// calls the operator's reconcilers make: HeadBucket, CreateBucket,
// DeleteBucket, ListBuckets, plus the empty/list hooks called from the
// deletion path. It does not implement object storage and does not validate
// SigV4.
//
// Why not the official aws-sdk-go-v2 mock or MinIO?
//   - The mock packages aren't general-purpose HTTP fakes.
//   - MinIO would pull in a container dependency for what is, in this
//     codebase, four endpoints' worth of behaviour. We do install MinIO in
//     CI for the chart-install lane, but the in-process operator tests
//     don't need a real S3.
//
// FakeS3 lets the e2e suite exercise the real AWS SDK signing path, the
// real backend.Classify error mapping, and the real reconcile loop while
// keeping the in-process tests self-contained.
type FakeS3 struct {
	server *httptest.Server

	mu      sync.Mutex
	buckets map[string]struct{}
	// failHead, when non-empty, returns 500 for HeadBucket against the
	// matching name. Useful for the "endpoint unreachable" reconcile path.
	failHead string
}

// NewFakeS3 starts a fake S3 server. The server is registered for teardown
// on test cleanup; the URL is suitable for S3Backend.spec.endpoint.
//
// The server binds to 127.0.0.1 on a random port, which is reachable from
// the in-process operator manager running in the same `go test` binary —
// the apiserver itself never needs to call out to it.
func NewFakeS3(t *testing.T) *FakeS3 {
	t.Helper()
	f := &FakeS3{buckets: map[string]struct{}{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

// URL returns the http://host:port endpoint clients should target.
func (f *FakeS3) URL() string { return f.server.URL }

// SeedBucket pre-creates a bucket — useful for HeadBucket-returns-200 tests.
func (f *FakeS3) SeedBucket(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.buckets[name] = struct{}{}
}

// HasBucket reports whether the named bucket exists in the fake.
func (f *FakeS3) HasBucket(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.buckets[name]
	return ok
}

// FailHeadFor configures HeadBucket to return 500 for the named bucket.
// Used to drive the reconciler into the "creation inconsistent" branch.
func (f *FakeS3) FailHeadFor(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failHead = name
}

func (f *FakeS3) handle(w http.ResponseWriter, r *http.Request) {
	bucket := strings.Trim(r.URL.Path, "/")
	if i := strings.IndexByte(bucket, '/'); i >= 0 {
		bucket = bucket[:i]
	}

	switch {
	case bucket == "" && r.Method == http.MethodGet:
		f.listBuckets(w)
		return
	case r.Method == http.MethodHead:
		f.headBucket(w, bucket)
		return
	case r.Method == http.MethodPut && r.URL.RawQuery == "":
		f.putBucket(w, bucket)
		return
	case r.Method == http.MethodDelete && r.URL.RawQuery == "":
		f.deleteBucket(w, bucket)
		return
	case r.Method == http.MethodGet:
		// list-objects-v2 / list-multipart-uploads — both return empty.
		f.listEmpty(w, bucket, r.URL.RawQuery)
		return
	}

	w.WriteHeader(http.StatusNotImplemented)
}

func (f *FakeS3) listBuckets(w http.ResponseWriter) {
	f.mu.Lock()
	names := make([]string, 0, len(f.buckets))
	for n := range f.buckets {
		names = append(names, n)
	}
	f.mu.Unlock()

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	b.WriteString(`<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
	b.WriteString(`<Owner><ID>fake</ID><DisplayName>fake</DisplayName></Owner>`)
	b.WriteString(`<Buckets>`)
	for _, n := range names {
		fmt.Fprintf(&b, `<Bucket><Name>%s</Name><CreationDate>2026-01-01T00:00:00.000Z</CreationDate></Bucket>`, n)
	}
	b.WriteString(`</Buckets></ListAllMyBucketsResult>`)

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}

func (f *FakeS3) headBucket(w http.ResponseWriter, name string) {
	f.mu.Lock()
	_, ok := f.buckets[name]
	failHead := f.failHead
	f.mu.Unlock()
	if failHead != "" && failHead == name {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (f *FakeS3) putBucket(w http.ResponseWriter, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.buckets[name]; exists {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>BucketAlreadyOwnedByYou</Code></Error>`))
		return
	}
	f.buckets[name] = struct{}{}
	w.WriteHeader(http.StatusOK)
}

func (f *FakeS3) deleteBucket(w http.ResponseWriter, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.buckets[name]; !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	delete(f.buckets, name)
	w.WriteHeader(http.StatusNoContent)
}

func (f *FakeS3) listEmpty(w http.ResponseWriter, bucket, query string) {
	f.mu.Lock()
	_, ok := f.buckets[bucket]
	f.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	switch {
	case strings.Contains(query, "uploads"):
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ListMultipartUploadsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></ListMultipartUploadsResult>`))
	default:
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></ListBucketResult>`))
	}
}
