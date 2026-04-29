// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package memory

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stowage-dev/stowage/internal/backend"
)

func TestBucketCRUD(t *testing.T) {
	b := New("t", "test")
	ctx := context.Background()

	if err := b.CreateBucket(ctx, "a", ""); err != nil {
		t.Fatalf("create: %v", err)
	}
	buckets, err := b.ListBuckets(ctx)
	if err != nil || len(buckets) != 1 || buckets[0].Name != "a" {
		t.Fatalf("list: %v %#v", err, buckets)
	}
	if err := b.DeleteBucket(ctx, "a"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestObjectRoundTrip(t *testing.T) {
	b := New("t", "test")
	ctx := context.Background()
	_ = b.CreateBucket(ctx, "p", "")

	body := "hello, world"
	info, err := b.PutObject(ctx, backend.PutObjectRequest{
		Bucket:      "p",
		Key:         "greeting.txt",
		Body:        strings.NewReader(body),
		Size:        int64(len(body)),
		ContentType: "text/plain",
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if info.Size != int64(len(body)) {
		t.Fatalf("size=%d want %d", info.Size, len(body))
	}

	r, err := b.GetObject(ctx, "p", "greeting.txt", "", nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer r.Close()
	got, _ := io.ReadAll(r)
	if string(got) != body {
		t.Fatalf("got %q want %q", got, body)
	}
}

func TestListObjectsWithDelimiter(t *testing.T) {
	b := New("t", "test")
	ctx := context.Background()
	_ = b.CreateBucket(ctx, "p", "")
	for _, k := range []string{"a.txt", "dir/b.txt", "dir/sub/c.txt", "x.txt"} {
		_, _ = b.PutObject(ctx, backend.PutObjectRequest{
			Bucket: "p", Key: k, Body: strings.NewReader(""), Size: 0,
		})
	}
	res, err := b.ListObjects(ctx, backend.ListObjectsRequest{
		Bucket: "p", Prefix: "", Delimiter: "/",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(res.Objects) != 2 {
		t.Fatalf("objects=%d want 2: %+v", len(res.Objects), res.Objects)
	}
	if len(res.CommonPrefixes) != 1 || res.CommonPrefixes[0] != "dir/" {
		t.Fatalf("common prefixes=%v want [dir/]", res.CommonPrefixes)
	}
}

func TestDeleteBucketNonEmpty(t *testing.T) {
	b := New("t", "test")
	ctx := context.Background()
	_ = b.CreateBucket(ctx, "p", "")
	_, _ = b.PutObject(ctx, backend.PutObjectRequest{Bucket: "p", Key: "k", Body: strings.NewReader("x"), Size: 1})
	if err := b.DeleteBucket(ctx, "p"); err == nil {
		t.Fatal("expected error deleting non-empty bucket")
	}
}
