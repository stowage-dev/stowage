// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyOperation_PostObject(t *testing.T) {
	cases := []struct {
		name        string
		method      string
		path        string
		contentType string
		route       RouteInfo
		want        string
	}{
		{
			name:        "browser form upload, path-style",
			method:      http.MethodPost,
			path:        "/my-bucket",
			contentType: "multipart/form-data; boundary=----abc",
			route:       RouteInfo{Bucket: "my-bucket", PathStyle: true},
			want:        "PostObject",
		},
		{
			name:        "browser form upload, virtual-hosted",
			method:      http.MethodPost,
			path:        "/",
			contentType: "multipart/form-data; boundary=----xyz",
			route:       RouteInfo{Bucket: "my-bucket"},
			want:        "PostObject",
		},
		{
			name:        "case-insensitive content type",
			method:      http.MethodPost,
			path:        "/",
			contentType: "Multipart/Form-Data; boundary=foo",
			route:       RouteInfo{Bucket: "b"},
			want:        "PostObject",
		},
		{
			name:        "POST with a key in the path is NOT PostObject",
			method:      http.MethodPost,
			path:        "/b/some-key",
			contentType: "multipart/form-data; boundary=foo",
			route:       RouteInfo{Bucket: "b", Key: "some-key"},
			want:        "Unknown",
		},
		{
			name:        "POST without multipart body falls through",
			method:      http.MethodPost,
			path:        "/b",
			contentType: "application/octet-stream",
			route:       RouteInfo{Bucket: "b"},
			want:        "Unknown",
		},
		{
			name:        "POST with no content type falls through",
			method:      http.MethodPost,
			path:        "/b",
			contentType: "",
			route:       RouteInfo{Bucket: "b"},
			want:        "Unknown",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "http://example.com"+tc.path, nil)
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}
			got := classifyOperation(r, tc.route)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestClassifyOperation_PostStillRecognizesMultipartSubresources(t *testing.T) {
	// CreateMultipartUpload is a POST + ?uploads. Even with a key="" route
	// (path-style without key) it must still classify as a multipart op,
	// not PostObject. PostObject requires multipart/form-data Content-Type
	// AND no other discriminating query parameter, so the query check wins.
	r := httptest.NewRequest(http.MethodPost, "http://example.com/b/k?uploads", nil)
	r.Header.Set("Content-Type", "multipart/form-data; boundary=foo")
	got := classifyOperation(r, RouteInfo{Bucket: "b", Key: "k"})
	require.Equal(t, "CreateMultipartUpload", got)
}

func TestIsPostObjectRequest(t *testing.T) {
	cases := []struct {
		ct   string
		want bool
	}{
		{"multipart/form-data; boundary=x", true},
		{"multipart/form-data", true},
		{"MULTIPART/FORM-DATA; boundary=x", true},
		{"application/json", false},
		{"", false},
		{"text/plain", false},
		{"multipart/related; boundary=x", false},
	}
	for _, tc := range cases {
		t.Run(tc.ct, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "http://example.com/", nil)
			if tc.ct != "" {
				r.Header.Set("Content-Type", tc.ct)
			}
			require.Equal(t, tc.want, isPostObjectRequest(r))
		})
	}
}
