// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"crypto/subtle"
	"net/http"
)

// CSRF returns middleware that enforces the double-submit cookie pattern on
// mutating requests. Safe methods (GET, HEAD, OPTIONS) are not checked.
//
// The token comes from the Identity already attached to the request context
// by Attach, so this middleware no longer hits SQLite. Requests without a
// session (e.g. public share resolver, unauthenticated login) skip the
// check — Auth middleware downstream will reject if a session is required.
func (m *SessionManager) CSRF(onFailure http.HandlerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
				next.ServeHTTP(w, r)
				return
			}

			id := IdentityFrom(r.Context())
			if id == nil {
				// No identity → either there's no session cookie at all
				// (then no CSRF needed), or Attach failed to resolve the
				// session (then the Require gate will 401 downstream).
				next.ServeHTTP(w, r)
				return
			}

			hdr := r.Header.Get(CSRFHeaderName)
			if hdr == "" || subtle.ConstantTimeCompare([]byte(hdr), []byte(id.CSRFToken)) != 1 {
				onFailure(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
