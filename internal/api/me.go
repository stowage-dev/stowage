// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/stowage-dev/stowage/internal/auth"
)

func (d *AuthDeps) handleMe(w http.ResponseWriter, r *http.Request) {
	id := auth.IdentityFrom(r.Context())
	out := map[string]any{
		"id":              id.UserID,
		"username":        id.Username,
		"role":            id.Role,
		"identity_source": id.Source,
		"must_change_pw":  id.MustChangePW,
	}
	if !id.SyntheticUser {
		u, err := d.Service.Store.GetUserByID(r.Context(), id.UserID)
		if err == nil && u.Email.Valid {
			out["email"] = u.Email.String
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (d *AuthDeps) handleChangeOwnPassword(w http.ResponseWriter, r *http.Request) {
	id := auth.IdentityFrom(r.Context())
	if id.SyntheticUser {
		writeError(w, http.StatusBadRequest, "static_user", "static users cannot change their password at runtime", "")
		return
	}
	if id.Source != auth.SourceLocal {
		writeError(w, http.StatusBadRequest, "not_local", "only local accounts can change passwords here", "")
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	err := d.Service.ChangeOwnPassword(r.Context(), id.UserID, id.SessionID, req.CurrentPassword, req.NewPassword)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case errors.Is(err, auth.ErrLoginFailed):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "current password is incorrect", "")
	default:
		var pe *auth.PolicyError
		if errors.As(err, &pe) {
			writeError(w, http.StatusBadRequest, "weak_password", pe.Error(), "")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not change password", "")
	}
}
