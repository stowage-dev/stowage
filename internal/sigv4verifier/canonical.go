// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sigv4verifier

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// Algorithm is the only algorithm we accept.
const Algorithm = "AWS4-HMAC-SHA256"

// UnsignedPayload is the placeholder SigV4 uses when the payload hash is
// intentionally not bound to the signature.
const UnsignedPayload = "UNSIGNED-PAYLOAD"

// StreamingPayload is the placeholder used by aws-chunked signed streams.
const StreamingPayload = "STREAMING-AWS4-HMAC-SHA256-PAYLOAD"

// CanonicalRequest builds the canonical request string defined by the spec:
//
//	HTTPMethod\n
//	CanonicalURI\n
//	CanonicalQueryString\n
//	CanonicalHeaders\n
//	SignedHeaders\n
//	HexEncode(Hash(RequestPayload))
//
// signedHeaders must be lower-case and sorted. payloadHash is the value the
// caller has decided to bind the signature to (may be UNSIGNED-PAYLOAD).
func CanonicalRequest(method string, rawURL *url.URL, headers http.Header, signedHeaders []string, payloadHash string) string {
	var b strings.Builder
	b.Grow(256)
	b.WriteString(strings.ToUpper(method))
	b.WriteByte('\n')
	b.WriteString(canonicalURI(rawURL.EscapedPath()))
	b.WriteByte('\n')
	b.WriteString(canonicalQuery(rawURL.Query()))
	b.WriteByte('\n')
	b.WriteString(canonicalHeaders(headers, signedHeaders))
	b.WriteByte('\n')
	b.WriteString(strings.Join(signedHeaders, ";"))
	b.WriteByte('\n')
	b.WriteString(payloadHash)
	return b.String()
}

// HashCanonical returns hex(sha256(canonicalRequest)).
//
// Uses a sync.Pool for the sha256 hash state — alloc profiling under
// proxy bench load showed sha256.New() alone at ~6% of allocations
// before this was pooled. Reset() leaves the underlying buffer in
// place so consecutive callers can reuse it without zeroing.
func HashCanonical(canonical string) string {
	h := sha256Pool.Get().(hash.Hash)
	h.Reset()
	_, _ = h.Write([]byte(canonical))
	var sum [32]byte
	h.Sum(sum[:0])
	sha256Pool.Put(h)
	return hex.EncodeToString(sum[:])
}

var sha256Pool = sync.Pool{
	New: func() any { return sha256.New() },
}

// canonicalURI applies the S3 double-encoding rule: S3 does NOT double-encode
// the path (unlike most AWS services) — its CanonicalURI is the already-
// percent-encoded path as it appears on the wire. Empty paths become "/".
func canonicalURI(escapedPath string) string {
	if escapedPath == "" {
		return "/"
	}
	if !strings.HasPrefix(escapedPath, "/") {
		return "/" + escapedPath
	}
	return escapedPath
}

// canonicalQuery sorts query params by key (lexicographic bytewise), then by
// value for repeated keys, and percent-encodes both according to RFC 3986.
func canonicalQuery(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	first := true
	for _, k := range keys {
		vals := append([]string(nil), q[k]...)
		sort.Strings(vals)
		// SigV4 query components always encode '/' — this matters for values
		// like X-Amz-Credential that embed scope slashes.
		ek := awsURLEncode(k, true)
		for _, v := range vals {
			if !first {
				b.WriteByte('&')
			}
			first = false
			b.WriteString(ek)
			b.WriteByte('=')
			b.WriteString(awsURLEncode(v, true))
		}
	}
	return b.String()
}

// canonicalHeaders emits "name:trimmed-value\n" for each signed header, in the
// same order as signedHeaders.
func canonicalHeaders(headers http.Header, signedHeaders []string) string {
	var b strings.Builder
	for _, h := range signedHeaders {
		b.WriteString(h)
		b.WriteByte(':')
		b.WriteString(canonicalHeaderValue(headers, h))
		b.WriteByte('\n')
	}
	return b.String()
}

func canonicalHeaderValue(headers http.Header, name string) string {
	// http.Header preserves the canonicalized-MIME casing; SigV4 wants lower
	// keys, and http.Header.Get does case-insensitive lookup, which is what
	// we want. For multi-value headers, join with ","  with inner values
	// space-collapsed.
	vals := headers.Values(http.CanonicalHeaderKey(name))
	if len(vals) == 0 {
		// Host is not stored in http.Header by net/http servers.
		if strings.EqualFold(name, "host") {
			return ""
		}
		return ""
	}
	joined := strings.Join(vals, ",")
	return trimAndCollapse(joined)
}

func trimAndCollapse(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	inQuotes := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			inQuotes = !inQuotes
			b.WriteByte(c)
			prevSpace = false
			continue
		}
		if !inQuotes && (c == ' ' || c == '\t') {
			if prevSpace {
				continue
			}
			prevSpace = true
			b.WriteByte(' ')
			continue
		}
		prevSpace = false
		b.WriteByte(c)
	}
	return b.String()
}

// awsURLEncode percent-encodes per AWS's SigV4 rules:
//   - A-Z a-z 0-9 '-' '_' '.' '~' are unreserved
//   - '/' is escaped except when encodeSlash is false
func awsURLEncode(s string, encodeSlash bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'A' <= c && c <= 'Z',
			'a' <= c && c <= 'z',
			'0' <= c && c <= '9',
			c == '-' || c == '_' || c == '.' || c == '~':
			b.WriteByte(c)
		case c == '/' && !encodeSlash:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hexUpper(c >> 4))
			b.WriteByte(hexUpper(c & 0xF))
		}
	}
	return b.String()
}

func hexUpper(n byte) byte {
	if n < 10 {
		return '0' + n
	}
	return 'A' + n - 10
}

// ExtractSignedHeaders returns a sorted, lower-cased list.
func ExtractSignedHeaders(raw string) []string {
	parts := strings.Split(raw, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}
