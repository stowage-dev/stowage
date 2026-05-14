// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// BucketCORSRule is one CORS rule attached to a bucket. The proxy
// evaluates inbound preflights against the union of rules a bucket has
// across every backend it's configured on (cache layer takes care of
// the union; this struct is just the wire shape).
//
// AllowedOrigins entries match the request's Origin header exactly, with
// "*" as the catch-all. AllowedMethods is the S3-permitted set
// (GET/HEAD/PUT/POST/DELETE). AllowedHeaders / ExposeHeaders default to
// a permissive SigV4 + POST-Object set when empty, mirroring what the
// cluster-wide config used to do. MaxAgeSeconds <= 0 falls back to 600s.
type BucketCORSRule struct {
	AllowedOrigins []string `json:"allowed_origins"`
	AllowedMethods []string `json:"allowed_methods"`
	AllowedHeaders []string `json:"allowed_headers,omitempty"`
	ExposeHeaders  []string `json:"expose_headers,omitempty"`
	MaxAgeSeconds  int      `json:"max_age_seconds,omitempty"`
}

// CORSSource is the read-only contract the proxy needs to answer a
// preflight without involving the upstream backend. Implementations
// (the SQLite source, tests) return the union of rules across every
// backend that hosts the bucket — preflights are anonymous, so the
// proxy can't tell which backend a future signed request will land on.
type CORSSource interface {
	LookupCORS(bucket string) ([]BucketCORSRule, bool)
}

var (
	defaultCORSHeaders = []string{
		"Authorization",
		"Content-Type",
		"Content-Disposition",
		"Content-Encoding",
		"Cache-Control",
		"Expires",
		"x-amz-date",
		"x-amz-content-sha256",
		"x-amz-security-token",
		"x-amz-user-agent",
		"x-amz-acl",
		"x-amz-storage-class",
		"x-amz-server-side-encryption",
		"x-amz-tagging",
		"x-amz-meta-*",
	}
	defaultCORSExposed = []string{
		"ETag",
		"x-amz-version-id",
		"x-amz-request-id",
	}
	defaultCORSMaxAge = 10 * time.Minute
)

// matchCORSRule walks the rule list and returns the first rule whose
// AllowedOrigins contains origin AND whose AllowedMethods contains
// method, plus the value to echo in Access-Control-Allow-Origin
// ("*" or the literal origin). Returns (nil, "") when nothing matches.
// method == "" skips the method check — used by non-preflight responses
// where the request's actual HTTP verb already authenticated the call.
func matchCORSRule(rules []BucketCORSRule, origin, method string) (*BucketCORSRule, string) {
	if origin == "" {
		return nil, ""
	}
	for i := range rules {
		ru := &rules[i]
		allow := matchOrigin(ru.AllowedOrigins, origin)
		if allow == "" {
			continue
		}
		if method != "" && !sliceContainsFold(ru.AllowedMethods, method) {
			continue
		}
		return ru, allow
	}
	return nil, ""
}

// matchOrigin returns "*" if the rule lists "*", the exact origin if it
// lists origin, or "" otherwise.
func matchOrigin(allowed []string, origin string) string {
	for _, o := range allowed {
		if o == "*" {
			return "*"
		}
		if o == origin {
			return origin
		}
	}
	return ""
}

func sliceContainsFold(haystack []string, needle string) bool {
	for _, v := range haystack {
		if strings.EqualFold(v, needle) {
			return true
		}
	}
	return false
}

// handlePreflight answers a CORS preflight request when one of the
// bucket's rules permits the requested origin + method. Returns true
// when it consumed the response. Non-preflight OPTIONS (no
// Access-Control-Request-Method) and unknown buckets fall through to
// the normal dispatcher, preserving pre-CORS behavior.
func handlePreflight(rules []BucketCORSRule, w http.ResponseWriter, r *http.Request) bool {
	if len(rules) == 0 {
		return false
	}
	if r.Method != http.MethodOptions {
		return false
	}
	reqMethod := r.Header.Get("Access-Control-Request-Method")
	if reqMethod == "" {
		// OPTIONS without ACRM isn't a preflight — fall through.
		return false
	}
	rule, allow := matchCORSRule(rules, r.Header.Get("Origin"), reqMethod)
	if rule == nil {
		return false
	}

	h := w.Header()
	h.Set("Access-Control-Allow-Origin", allow)

	methods := rule.AllowedMethods
	h.Set("Access-Control-Allow-Methods", strings.Join(methods, ", "))

	headers := rule.AllowedHeaders
	if len(headers) == 0 {
		headers = defaultCORSHeaders
	}
	h.Set("Access-Control-Allow-Headers", strings.Join(headers, ", "))

	maxAge := time.Duration(rule.MaxAgeSeconds) * time.Second
	if maxAge <= 0 {
		maxAge = defaultCORSMaxAge
	}
	h.Set("Access-Control-Max-Age", strconv.Itoa(int(maxAge.Seconds())))

	// Caches must key on Origin (and the preflight headers) so a
	// response cached for origin A doesn't leak to origin B.
	h.Add("Vary", "Origin")
	h.Add("Vary", "Access-Control-Request-Method")
	h.Add("Vary", "Access-Control-Request-Headers")

	w.WriteHeader(http.StatusNoContent)
	return true
}

// decorateCORS adds Access-Control-Allow-Origin + Expose-Headers to a
// non-preflight response when one of the bucket's rules covers the
// inbound origin. Method is not enforced here — the rule's
// AllowedMethods is a preflight contract, and an actual cross-origin
// request that survived auth has already been authorised.
func decorateCORS(rules []BucketCORSRule, w http.ResponseWriter, r *http.Request) {
	if len(rules) == 0 {
		return
	}
	rule, allow := matchCORSRule(rules, r.Header.Get("Origin"), "")
	if rule == nil {
		return
	}
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", allow)
	exposed := rule.ExposeHeaders
	if len(exposed) == 0 {
		exposed = defaultCORSExposed
	}
	h.Set("Access-Control-Expose-Headers", strings.Join(exposed, ", "))
	h.Add("Vary", "Origin")
}
