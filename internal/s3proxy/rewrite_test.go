// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"net/url"
	"strings"
	"testing"
)

// TestStripPresignedQuery asserts that the SigV4 presigned-URL query params
// are removed while unrelated query params survive. If these params leaked
// through to the outbound URL, the backend would treat the request as
// query-string signed against the client's AKID — which it doesn't know —
// and reject with 403.
func TestStripPresignedQuery(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		// keep is the set of param keys we expect to remain after strip.
		// ordering is not asserted because url.Values.Encode sorts by key.
		keep []string
	}{
		{
			name: "empty",
			raw:  "",
			keep: nil,
		},
		{
			name: "all presigned params",
			raw:  "X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKID/20260424/us-east-1/s3/aws4_request&X-Amz-Date=20260424T000000Z&X-Amz-Expires=60&X-Amz-SignedHeaders=host&X-Amz-Signature=deadbeef&X-Amz-Security-Token=TOKEN",
			keep: nil,
		},
		{
			name: "presigned mixed with application params",
			raw:  "x-id=GetObject&X-Amz-Signature=deadbeef&X-Amz-Credential=AKID&versionId=v1&response-content-type=text%2Fplain",
			keep: []string{"x-id", "versionId", "response-content-type"},
		},
		{
			name: "no presigned params — pass through verbatim",
			raw:  "x-id=PutObject&partNumber=1&uploadId=abc",
			keep: []string{"x-id", "partNumber", "uploadId"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := StripPresignedQuery(tc.raw)
			parsed, err := url.ParseQuery(got)
			if err != nil {
				t.Fatalf("result %q not parseable: %v", got, err)
			}
			for _, p := range presignedQueryParams {
				if parsed.Has(p) {
					t.Errorf("presigned param %q still present: %q", p, got)
				}
			}
			for _, k := range tc.keep {
				if !parsed.Has(k) {
					t.Errorf("expected %q preserved, got %q", k, got)
				}
			}
			if len(tc.keep) == 0 && tc.raw == "" && got != "" {
				t.Errorf("empty input must stay empty, got %q", got)
			}
		})
	}
}

// TestStripPresignedQuery_MalformedIsPassthrough: if the input can't be
// parsed as a query string, we return it verbatim rather than lose data.
// The backend will reject it either way, but we must not mangle it here.
func TestStripPresignedQuery_MalformedIsPassthrough(t *testing.T) {
	raw := "broken=%ZZ"
	got := StripPresignedQuery(raw)
	if !strings.Contains(got, "broken=%ZZ") {
		t.Errorf("malformed input should pass through, got %q", got)
	}
}

// TestBuildOutboundRawPath verifies the AWS canonical-URI encoding of the
// outbound path. Only unreserved characters (alnum + `-._~`) survive as-is;
// everything else — including `+`, `=`, space, and multi-byte UTF-8 — must
// be pct-encoded so the wire form and the SigV4 canonical URI agree.
func TestBuildOutboundRawPath(t *testing.T) {
	tests := []struct {
		name   string
		bucket string
		key    string
		want   string
	}{
		{"no key", "mybucket", "", "/mybucket"},
		{"plain key", "mybucket", "hello.txt", "/mybucket/hello.txt"},
		{"plus and equals", "mybucket", "a+b=c.txt", "/mybucket/a%2Bb%3Dc.txt"},
		{"space", "mybucket", "hello world.txt", "/mybucket/hello%20world.txt"},
		{"utf-8", "mybucket", "café.txt", "/mybucket/caf%C3%A9.txt"},
		{"slash inside key is preserved", "mybucket", "prefix/sub.txt", "/mybucket/prefix/sub.txt"},
		{"slash plus reserved char", "mybucket", "a/b+c=d.txt", "/mybucket/a/b%2Bc%3Dd.txt"},
		{"unreserved punct", "mybucket", "a-b.c_d~e.txt", "/mybucket/a-b.c_d~e.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildOutboundRawPath(tc.bucket, tc.key)
			if got != tc.want {
				t.Errorf("BuildOutboundRawPath(%q, %q) = %q, want %q", tc.bucket, tc.key, got, tc.want)
			}
		})
	}
}

func TestAWSPathEscape_FastPath(t *testing.T) {
	in := "abcXYZ123-._~"
	if got := awsPathEscape(in); got != in {
		t.Errorf("fast path expected %q, got %q", in, got)
	}
}
