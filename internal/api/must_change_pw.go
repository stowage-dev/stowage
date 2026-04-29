// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"net/http"

	"github.com/stowage-dev/stowage/internal/auth"
)

// requirePasswordRotated blocks every authenticated /api/* path except the
// minimum surface a user needs to actually rotate their password. Sits AFTER
// auth.Require so id != nil; sits BEFORE the role-based gates so a user who
// hasn't rotated can't slip into admin endpoints just because their session
// happens to carry role=admin.
//
// The /me/password endpoint is allow-listed because it's how the user gets
// out of the must-rotate state. /me read access is allow-listed so the SPA
// can show the user their identity on the rotation screen. /auth/logout is
// at the router root, not under /api, so it's already free.
func requirePasswordRotated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := auth.IdentityFrom(r.Context())
		if id == nil || !id.MustChangePW {
			next.ServeHTTP(w, r)
			return
		}
		if isPasswordRotationAllowed(r) {
			next.ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusForbidden, "password_change_required",
			"rotate your password before using the API", "")
	})
}

// isPasswordRotationAllowed lists the request shapes a must-rotate user is
// still allowed to make. Kept narrow on purpose: any new endpoint defaults
// to "blocked" until someone explicitly adds it here.
func isPasswordRotationAllowed(r *http.Request) bool {
	switch r.URL.Path {
	case "/api/me":
		return r.Method == http.MethodGet
	case "/api/me/password":
		return r.Method == http.MethodPost
	}
	return false
}
