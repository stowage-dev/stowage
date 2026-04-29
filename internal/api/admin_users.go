// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

type userDTO struct {
	ID             string  `json:"id"`
	Username       string  `json:"username"`
	Email          *string `json:"email,omitempty"`
	Role           string  `json:"role"`
	IdentitySource string  `json:"identity_source"`
	Enabled        bool    `json:"enabled"`
	MustChangePW   bool    `json:"must_change_pw"`
	FailedAttempts int     `json:"failed_attempts"`
	LockedUntil    *string `json:"locked_until,omitempty"`
	CreatedAt      string  `json:"created_at"`
	LastLoginAt    *string `json:"last_login_at,omitempty"`
}

func toUserDTO(u *sqlite.User) userDTO {
	d := userDTO{
		ID:             u.ID,
		Username:       u.Username,
		Role:           u.Role,
		IdentitySource: u.IdentitySource,
		Enabled:        u.Enabled,
		MustChangePW:   u.MustChangePW,
		FailedAttempts: u.FailedAttempts,
		CreatedAt:      u.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if u.Email.Valid {
		s := u.Email.String
		d.Email = &s
	}
	if u.LockedUntil.Valid {
		s := u.LockedUntil.Time.Format("2006-01-02T15:04:05Z07:00")
		d.LockedUntil = &s
	}
	if u.LastLoginAt.Valid {
		s := u.LastLoginAt.Time.Format("2006-01-02T15:04:05Z07:00")
		d.LastLoginAt = &s
	}
	return d
}

func (d *AuthDeps) handleListUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := sqlite.UserListFilter{
		Query:  q.Get("query"),
		Role:   q.Get("role"),
		Source: q.Get("source"),
	}
	if v := q.Get("enabled"); v != "" {
		b := v == "true"
		filter.Enabled = &b
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.Limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.Offset = n
		}
	}
	users, err := d.Service.Store.ListUsers(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list users", "")
		return
	}
	out := make([]userDTO, 0, len(users))
	for _, u := range users {
		out = append(out, toUserDTO(u))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

type createUserRequest struct {
	Username     string `json:"username"`
	Email        string `json:"email"`
	Password     string `json:"password"`
	Role         string `json:"role"`
	MustChangePW *bool  `json:"must_change_pw"`
}

func (d *AuthDeps) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	if req.Username == "" || req.Password == "" || req.Role == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "username, password, and role are required", "")
		return
	}
	mustChange := true
	if req.MustChangePW != nil {
		mustChange = *req.MustChangePW
	}
	actor := auth.IdentityFrom(r.Context())
	createdBy := ""
	if actor != nil && !actor.SyntheticUser {
		createdBy = actor.UserID
	}

	u, err := d.Service.CreateLocalUser(r.Context(), req.Username, req.Email, req.Password, req.Role, createdBy, mustChange)
	if err != nil {
		switch {
		case errors.Is(err, sqlite.ErrUsernameTaken):
			writeError(w, http.StatusConflict, "username_taken", "username already taken", "")
		case errors.Is(err, sqlite.ErrEmailTaken):
			writeError(w, http.StatusConflict, "email_taken", "email already taken", "")
		default:
			var pe *auth.PolicyError
			if errors.As(err, &pe) {
				writeError(w, http.StatusBadRequest, "weak_password", pe.Error(), "")
				return
			}
			writeError(w, http.StatusBadRequest, "bad_request", err.Error(), "")
		}
		return
	}
	writeJSON(w, http.StatusCreated, toUserDTO(u))
}

func (d *AuthDeps) handleGetUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u, err := d.Service.Store.GetUserByID(r.Context(), id)
	if errors.Is(err, sqlite.ErrUserNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "user not found", "")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load user", "")
		return
	}
	writeJSON(w, http.StatusOK, toUserDTO(u))
}

type patchUserRequest struct {
	Role    *string `json:"role"`
	Enabled *bool   `json:"enabled"`
	Email   *string `json:"email"`
}

func (d *AuthDeps) handlePatchUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req patchUserRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}

	// Guard against patches that would orphan the deployment (no enabled
	// admin left). Fires only when the patch actually demotes/disables an
	// admin row; reads the current row first so we don't pay the count for
	// a no-op patch.
	if req.Role != nil || req.Enabled != nil {
		current, err := d.Service.Store.GetUserByID(r.Context(), id)
		if errors.Is(err, sqlite.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "user not found", "")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not load user", "")
			return
		}
		demoting := req.Role != nil && *req.Role != "admin" && current.Role == "admin"
		disabling := req.Enabled != nil && !*req.Enabled && current.Enabled && current.Role == "admin"
		if demoting || disabling {
			n, err := d.Service.Store.CountEnabledAdmins(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, "internal", "could not check admin count", "")
				return
			}
			if n <= 1 {
				writeError(w, http.StatusConflict, "last_admin",
					"refusing to demote or disable the only enabled admin", "")
				return
			}
		}
		// Self-role-change is a foot-gun even when other admins exist —
		// the operator's session keeps the old role until next login, so
		// the UI silently desyncs. Block it; the operator should ask
		// another admin or use a separate account.
		actor := auth.IdentityFrom(r.Context())
		if actor != nil && actor.UserID == id && req.Role != nil && *req.Role != current.Role {
			writeError(w, http.StatusForbidden, "self_role_change",
				"cannot change your own role; ask another admin", "")
			return
		}
	}

	patch := sqlite.UserPatch{Role: req.Role, Enabled: req.Enabled}
	if req.Email != nil {
		ns := sql.NullString{String: *req.Email, Valid: *req.Email != ""}
		patch.Email = &ns
	}
	if err := d.Service.Store.UpdateUser(r.Context(), id, patch); err != nil {
		if errors.Is(err, sqlite.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "user not found", "")
			return
		}
		if errors.Is(err, sqlite.ErrEmailTaken) {
			writeError(w, http.StatusConflict, "email_taken", "email already taken", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not update user", "")
		return
	}
	// Role / enabled / email changes need to take effect immediately on
	// the target's next request, regardless of cache TTL.
	d.Service.Sessions.Cache.InvalidateUser(id)
	u, _ := d.Service.Store.GetUserByID(r.Context(), id)
	writeJSON(w, http.StatusOK, toUserDTO(u))
}

type resetPasswordRequest struct {
	NewPassword string `json:"new_password"`
}

func (d *AuthDeps) handleAdminResetPassword(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req resetPasswordRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	// Self-reset shouldn't flip must_change_pw or kill the caller's session:
	// the admin just chose this password themselves, so forcing another rotate
	// at next sign-in is nonsensical and would log them out immediately.
	keepSessionID := ""
	mustChange := true
	actor := auth.IdentityFrom(r.Context())
	if actor != nil && !actor.SyntheticUser && actor.UserID == id {
		keepSessionID = actor.SessionID
		mustChange = false
	}
	if err := d.Service.AdminResetPassword(r.Context(), id, keepSessionID, req.NewPassword, mustChange); err != nil {
		var pe *auth.PolicyError
		if errors.As(err, &pe) {
			writeError(w, http.StatusBadRequest, "weak_password", pe.Error(), "")
			return
		}
		if errors.Is(err, sqlite.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "user not found", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not reset password", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d *AuthDeps) handleAdminUnlock(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := d.Service.Store.UpdateUser(r.Context(), id, sqlite.UserPatch{Unlock: true}); err != nil {
		if errors.Is(err, sqlite.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "user not found", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not unlock user", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d *AuthDeps) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	actor := auth.IdentityFrom(r.Context())
	if actor != nil && actor.UserID == id {
		writeError(w, http.StatusBadRequest, "self_delete", "cannot delete your own account", "")
		return
	}
	// Refuse the delete if the target is the only remaining enabled admin.
	target, err := d.Service.Store.GetUserByID(r.Context(), id)
	if errors.Is(err, sqlite.ErrUserNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "user not found", "")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load user", "")
		return
	}
	if target.Role == "admin" && target.Enabled {
		n, err := d.Service.Store.CountEnabledAdmins(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not check admin count", "")
			return
		}
		if n <= 1 {
			writeError(w, http.StatusConflict, "last_admin",
				"refusing to delete the only enabled admin", "")
			return
		}
	}
	if err := d.Service.Store.DeleteUser(r.Context(), id); err != nil {
		if errors.Is(err, sqlite.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "user not found", "")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "could not delete user", "")
		return
	}
	_ = d.Service.Store.DeleteUserSessions(r.Context(), id, "")
	d.Service.Sessions.Cache.InvalidateUser(id)
	w.WriteHeader(http.StatusNoContent)
}
