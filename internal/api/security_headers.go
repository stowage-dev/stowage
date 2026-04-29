// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import "net/http"

// baseCSP is the Content-Security-Policy applied to every route by default.
// Handlers can override it by writing the header themselves — spaHandler
// does that for the SvelteKit shell so it can fold in sha256 hashes for the
// inline bootstrap scripts the static build emits.
const baseCSP = "default-src 'self'; " +
	"img-src 'self' data: blob:; " +
	"media-src 'self' blob:; " +
	"style-src 'self' 'unsafe-inline'; " +
	"script-src 'self'; " +
	"connect-src 'self'; " +
	"font-src 'self' data:; " +
	"frame-ancestors 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'"

// securityHeaders applies a baseline set of response headers to every route
// the router serves. Handlers are free to override the values (e.g.
// /s/<code>/raw replaces the CSP with `sandbox` and the share-info JSON
// keeps Cache-Control: no-store).
//
// We set the headers BEFORE invoking the inner handler so handler-level
// w.Header().Set() calls win — Go's http.Header semantics are last-write-
// wins for Set, which is what we want here.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", baseCSP)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=(), payment=(), usb=()")
		// HSTS only meaningful on TLS — emitting it on plain HTTP is at
		// best ignored, at worst a foot-gun if a dev hits the box on
		// localhost without TLS and then the browser caches the directive.
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
