// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"net/http"
	"net/url"
	"strings"
)

// Inbound headers that must never pass through to the backend. We rebuild
// signing-related headers during outbound re-signing.
var stripInbound = map[string]struct{}{
	"authorization":                {},
	"x-amz-date":                   {},
	"x-amz-content-sha256":         {},
	"x-amz-security-token":         {},
	"x-amz-decoded-content-length": {},
	"connection":                   {},
	"keep-alive":                   {},
	"proxy-authenticate":           {},
	"proxy-authorization":          {},
	"te":                           {},
	"trailer":                      {},
	"transfer-encoding":            {},
	"upgrade":                      {},
}

// Allow-listed X-Amz-* headers that the backend cares about. Anything else
// starting with x-amz- is dropped defensively to avoid smuggling signed
// headers into the outbound signature.
var allowedAmzPrefixes = map[string]struct{}{
	"x-amz-acl":                       {},
	"x-amz-copy-source":               {},
	"x-amz-copy-source-range":         {},
	"x-amz-metadata-directive":        {},
	"x-amz-storage-class":             {},
	"x-amz-tagging":                   {},
	"x-amz-server-side-encryption":    {},
	"x-amz-mfa":                       {},
	"x-amz-grant-full-control":        {},
	"x-amz-grant-read":                {},
	"x-amz-grant-write":               {},
	"x-amz-grant-read-acp":            {},
	"x-amz-grant-write-acp":           {},
	"x-amz-website-redirect-location": {},
	"x-amz-request-payer":             {},
}

// User metadata prefix is preserved as a whole.
const userMetaPrefix = "x-amz-meta-"

// PrepareOutboundHeaders returns a new http.Header suitable for an outbound
// admin-signed request. Destination headers like Host are the caller's job.
func PrepareOutboundHeaders(in http.Header) http.Header {
	out := make(http.Header, len(in))
	for k, vs := range in {
		lk := strings.ToLower(k)
		if _, strip := stripInbound[lk]; strip {
			continue
		}
		if strings.HasPrefix(lk, "x-amz-") {
			if strings.HasPrefix(lk, userMetaPrefix) {
				out[k] = append(out[k], vs...)
				continue
			}
			if _, ok := allowedAmzPrefixes[lk]; !ok {
				continue
			}
		}
		out[k] = append(out[k], vs...)
	}
	return out
}

// BuildOutboundPath returns the outbound path-style URL path for a virtual
// request. If the inbound request was virtual-hosted, we flatten to path-style.
func BuildOutboundPath(realBucket, key string) string {
	if key == "" {
		return "/" + realBucket
	}
	return "/" + realBucket + "/" + key
}

// BuildOutboundRawPath returns the percent-encoded form of BuildOutboundPath
// using AWS SigV4 canonical-URI encoding: every byte outside the unreserved
// set (RFC 3986: A-Z / a-z / 0-9 / - . _ ~) is pct-encoded. Slashes inside
// the key are preserved literal — S3 keys are "blobs with /" on the wire, not
// URI segments, and SDKs/backends canonicalise them that way. Used to set
// url.URL.RawPath so the wire form and the SigV4 canonical URI agree for
// keys that contain reserved characters like `+` or `=`, which strict S3
// implementations (e.g. SeaweedFS) canonicalise to %2B/%3D and reject the
// mismatched signature otherwise.
func BuildOutboundRawPath(realBucket, key string) string {
	if key == "" {
		return "/" + awsPathEscape(realBucket)
	}
	return "/" + awsPathEscape(realBucket) + "/" + awsKeyEscape(key)
}

// awsKeyEscape is awsPathEscape for a full S3 key: each `/`-separated segment
// is escaped individually, slashes are preserved verbatim.
func awsKeyEscape(key string) string {
	if !strings.Contains(key, "/") {
		return awsPathEscape(key)
	}
	segs := strings.Split(key, "/")
	for i, s := range segs {
		segs[i] = awsPathEscape(s)
	}
	return strings.Join(segs, "/")
}

// awsPathEscape percent-encodes a single URI path segment using the S3 SigV4
// canonical-URI rules: unreserved set (A-Z / a-z / 0-9 / - . _ ~) is passed
// through; everything else is pct-encoded. Segment separators `/` are NOT
// treated specially here — the caller is expected to only pass single
// segments.
func awsPathEscape(s string) string {
	// Fast path: nothing to encode.
	needs := false
	for i := 0; i < len(s); i++ {
		if !isAWSUnreserved(s[i]) {
			needs = true
			break
		}
	}
	if !needs {
		return s
	}
	const hex = "0123456789ABCDEF"
	out := make([]byte, 0, len(s)+8)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isAWSUnreserved(c) {
			out = append(out, c)
			continue
		}
		out = append(out, '%', hex[c>>4], hex[c&0x0f])
	}
	return string(out)
}

func isAWSUnreserved(c byte) bool {
	switch {
	case 'A' <= c && c <= 'Z':
		return true
	case 'a' <= c && c <= 'z':
		return true
	case '0' <= c && c <= '9':
		return true
	}
	switch c {
	case '-', '.', '_', '~':
		return true
	}
	return false
}

// presignedQueryParams are the SigV4 query-string auth parameters. They carry
// the client's virtual-credential signature, which is meaningless to the
// backend once we re-sign via Authorization header — and their presence makes
// spec-compliant backends (e.g. SeaweedFS) treat the outbound as a presigned
// request, look up the VC's AKID, and reject with 403.
var presignedQueryParams = []string{
	"X-Amz-Algorithm",
	"X-Amz-Credential",
	"X-Amz-Date",
	"X-Amz-Expires",
	"X-Amz-SignedHeaders",
	"X-Amz-Signature",
	"X-Amz-Security-Token",
}

// StripPresignedQuery returns raw the way r.URL.RawQuery would, but with the
// SigV4 presigned-URL query parameters removed. Non-signing params — including
// application params like x-id, versionId, response-* overrides — are kept
// untouched.
func StripPresignedQuery(raw string) string {
	if raw == "" {
		return raw
	}
	q, err := url.ParseQuery(raw)
	if err != nil {
		return raw
	}
	stripped := false
	for _, k := range presignedQueryParams {
		if _, ok := q[k]; ok {
			q.Del(k)
			stripped = true
		}
	}
	if !stripped {
		return raw
	}
	return q.Encode()
}
