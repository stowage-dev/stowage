// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/stowage-dev/stowage/internal/audit"
	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/auth/oidc"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// AuthDeps plugs the auth stack into api.NewRouter. Kept separate from the
// generic Deps struct so the router stays ignorant of auth internals.
type AuthDeps struct {
	Service *auth.Service
	OIDC    *oidc.Provider // optional
	// RateLim guards login and other unauthenticated endpoints; keyed by IP.
	RateLim *auth.RateLimiter
	// SessionRateLim guards /api/* per session; nil disables enforcement.
	SessionRateLim *auth.RateLimiter
	Logger         *slog.Logger
	Audit          audit.Recorder
}

func (d *AuthDeps) handleAuthConfig(w http.ResponseWriter, _ *http.Request) {
	modes := make([]string, 0, len(d.Service.Cfg.Modes))
	for _, m := range d.Service.Cfg.Modes {
		// Expose only modes that are actually wired up — e.g. don't say "oidc"
		// if the provider isn't initialised.
		if m == "oidc" && d.OIDC == nil {
			continue
		}
		modes = append(modes, m)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"modes":                   modes,
		"allow_self_registration": d.Service.Cfg.Local.AllowSelfRegistration,
	})
}

type loginLocalRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (d *AuthDeps) handleLoginLocal(w http.ResponseWriter, r *http.Request) {
	if !d.Service.ModeEnabled("local") && !d.Service.ModeEnabled("static") {
		writeError(w, http.StatusBadRequest, "mode_disabled", "local auth is not enabled", "")
		return
	}

	var req loginLocalRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials", "")
		return
	}

	// Static mode wins if the username matches the static user — that way
	// bootstrap works even before a local user is created.
	if d.Service.ModeEnabled("static") && d.Service.Static != nil && req.Username == d.Service.Static.Username {
		if err := d.Service.LoginStatic(r.Context(), req.Username, req.Password); err != nil {
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials", "")
			return
		}
		if _, err := d.Service.Sessions.Issue(r.Context(), w, r, auth.StaticUserID, auth.SourceStatic, 0); err != nil {
			writeError(w, http.StatusInternalServerError, "session_error", "could not create session", "")
			return
		}
		audit.RecordRequest(d.Audit, r, audit.Event{
			Action: "auth.login",
			UserID: auth.StaticUserID,
			Detail: map[string]any{"source": auth.SourceStatic},
		})
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "source": auth.SourceStatic})
		return
	}

	if !d.Service.ModeEnabled("local") {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials", "")
		return
	}

	res, err := d.Service.LoginLocal(r.Context(), req.Username, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrAccountLocked):
			writeError(w, http.StatusTooManyRequests, "account_locked", "account temporarily locked", "")
		case errors.Is(err, auth.ErrAccountDisabled):
			// Same shape as invalid credentials — don't leak that the user exists.
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials", "")
		default:
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials", "")
		}
		return
	}

	var flags sqlite.SessionFlags
	if res.MustChangePW {
		flags |= sqlite.FlagMustChangePW
	}
	if _, err := d.Service.Sessions.Issue(r.Context(), w, r, res.UserID, auth.SourceLocal, flags); err != nil {
		writeError(w, http.StatusInternalServerError, "session_error", "could not create session", "")
		return
	}
	audit.RecordRequest(d.Audit, r, audit.Event{
		Action: "auth.login",
		UserID: res.UserID,
		Detail: map[string]any{"source": auth.SourceLocal},
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"source":         auth.SourceLocal,
		"must_change_pw": res.MustChangePW,
	})
}

func (d *AuthDeps) handleLoginOIDCStart(w http.ResponseWriter, r *http.Request) {
	if d.OIDC == nil {
		writeError(w, http.StatusBadRequest, "mode_disabled", "OIDC is not configured", "")
		return
	}
	d.OIDC.StartLogin(w, r)
}

func (d *AuthDeps) handleCallback(w http.ResponseWriter, r *http.Request) {
	if d.OIDC == nil {
		writeError(w, http.StatusBadRequest, "mode_disabled", "OIDC is not configured", "")
		return
	}
	res, err := d.OIDC.Callback(r.Context(), r, w, d.Service.Store)
	if err != nil {
		d.Logger.Warn("oidc callback failed", "err", err.Error())
		writeError(w, http.StatusUnauthorized, "oidc_failed", "sign-in failed", "")
		return
	}
	if _, err := d.Service.Sessions.Issue(r.Context(), w, r, res.User.ID, res.User.IdentitySource, 0); err != nil {
		writeError(w, http.StatusInternalServerError, "session_error", "could not create session", "")
		return
	}
	// Redirect back to the SPA. In a richer implementation we'd persist the
	// original return-to URL in the start step.
	http.Redirect(w, r, "/", http.StatusFound)
}

func (d *AuthDeps) handleLogout(w http.ResponseWriter, r *http.Request) {
	id := auth.IdentityFrom(r.Context())
	if id != nil {
		_ = d.Service.Sessions.Revoke(r.Context(), w, r, id.SessionID)
		audit.RecordRequest(d.Audit, r, audit.Event{Action: "auth.logout", UserID: id.UserID})
	} else {
		_ = d.Service.Sessions.Revoke(r.Context(), w, r, "")
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
