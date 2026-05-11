// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// CORSConfig governs how the proxy answers browser CORS preflight and
// decorates same-origin responses. Nil disables CORS entirely: the proxy
// passes OPTIONS through to the S3 dispatcher (which 4xx's it) and never
// adds Access-Control-* headers.
//
// CORS is configured cluster-wide rather than per-bucket because stowage
// doesn't (yet) carry per-bucket CORS configuration; an operator who
// needs different rules for different buckets should front the proxy
// with an ingress that does CORS.
type CORSConfig struct {
	// AllowedOrigins is the closed allowlist. A single "*" entry matches
	// every origin and echoes "*" back; otherwise origin must equal one
	// of the entries (case-sensitive, scheme+host+port) and that exact
	// origin is echoed in Access-Control-Allow-Origin.
	AllowedOrigins []string

	// AllowedMethods is echoed in preflight responses. Defaults to the
	// usual S3 set (GET, HEAD, PUT, POST, DELETE) when empty.
	AllowedMethods []string

	// AllowedHeaders is echoed in preflight responses. Defaults to a
	// permissive set covering SigV4 (Authorization, x-amz-*) and POST
	// Object (Content-Type, Content-Disposition, etc) when empty.
	AllowedHeaders []string

	// ExposedHeaders is sent on actual responses so JavaScript can read
	// ETag, x-amz-version-id, and x-amz-request-id from the Response.
	// Defaults to those three when empty.
	ExposedHeaders []string

	// MaxAge is the preflight cache duration the browser is told to
	// honor. Defaults to 10 minutes.
	MaxAge time.Duration

	// AllowCredentials controls whether Access-Control-Allow-Credentials
	// is set. Required to be false when AllowedOrigins is "*" — browsers
	// reject the combination.
	AllowCredentials bool
}

var (
	defaultCORSMethods = []string{
		http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPost, http.MethodDelete,
	}
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

// allowedOrigin returns the value to echo in Access-Control-Allow-Origin,
// or "" if origin is not allowed (or CORS is disabled, or no Origin
// header was sent).
func allowedOrigin(cfg *CORSConfig, origin string) string {
	if cfg == nil || origin == "" {
		return ""
	}
	for _, o := range cfg.AllowedOrigins {
		if o == "*" {
			return "*"
		}
		if o == origin {
			return origin
		}
	}
	return ""
}

// handlePreflight answers a CORS preflight request. Returns true when
// it consumed the response — in which case the caller must not invoke
// the regular handler chain. A preflight with a disallowed origin or
// with CORS disabled returns false; the request then falls through to
// the normal S3 dispatch path, which will 4xx it.
func handlePreflight(cfg *CORSConfig, w http.ResponseWriter, r *http.Request) bool {
	if cfg == nil {
		return false
	}
	if r.Method != http.MethodOptions {
		return false
	}
	origin := r.Header.Get("Origin")
	allow := allowedOrigin(cfg, origin)
	if allow == "" {
		return false
	}
	// A preflight must carry Access-Control-Request-Method. Treat the
	// header's absence as a non-preflight OPTIONS and let it fall
	// through — preserves any future S3-style OPTIONS handling.
	if r.Header.Get("Access-Control-Request-Method") == "" {
		return false
	}

	h := w.Header()
	h.Set("Access-Control-Allow-Origin", allow)
	if cfg.AllowCredentials && allow != "*" {
		h.Set("Access-Control-Allow-Credentials", "true")
	}
	methods := cfg.AllowedMethods
	if len(methods) == 0 {
		methods = defaultCORSMethods
	}
	h.Set("Access-Control-Allow-Methods", strings.Join(methods, ", "))

	headers := cfg.AllowedHeaders
	if len(headers) == 0 {
		headers = defaultCORSHeaders
	}
	h.Set("Access-Control-Allow-Headers", strings.Join(headers, ", "))

	maxAge := cfg.MaxAge
	if maxAge <= 0 {
		maxAge = defaultCORSMaxAge
	}
	h.Set("Access-Control-Max-Age", strconv.Itoa(int(maxAge.Seconds())))

	// Some caches key on Origin; tell them so they don't serve the
	// preflight response to a different origin's preflight.
	h.Add("Vary", "Origin")
	h.Add("Vary", "Access-Control-Request-Method")
	h.Add("Vary", "Access-Control-Request-Headers")

	w.WriteHeader(http.StatusNoContent)
	return true
}

// decorateCORS adds Access-Control-Allow-Origin and Expose-Headers to a
// non-preflight response when the origin is allowed. Safe to call before
// the response has been written; it only writes headers, not the body.
func decorateCORS(cfg *CORSConfig, w http.ResponseWriter, r *http.Request) {
	if cfg == nil {
		return
	}
	origin := r.Header.Get("Origin")
	allow := allowedOrigin(cfg, origin)
	if allow == "" {
		return
	}
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", allow)
	if cfg.AllowCredentials && allow != "*" {
		h.Set("Access-Control-Allow-Credentials", "true")
	}
	exposed := cfg.ExposedHeaders
	if len(exposed) == 0 {
		exposed = defaultCORSExposed
	}
	h.Set("Access-Control-Expose-Headers", strings.Join(exposed, ", "))
	// Caches must vary on Origin so a request from origin A doesn't get
	// served origin B's cached response.
	h.Add("Vary", "Origin")
}
