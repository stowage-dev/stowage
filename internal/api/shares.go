// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/stowage-dev/stowage/internal/audit"
	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/shares"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// ShareDeps plugs the shares package into the router. Kept separate so the
// router doesn't grow direct dependencies on every feature.
type ShareDeps struct {
	Service *shares.Service
	RateLim *auth.RateLimiter // applied to /s/:code
	Logger  *slog.Logger
	// Unlock is the signer used to mint the cookie that proves a recipient
	// already entered the right password. Stateless: cookie carries an
	// expiry timestamp and an HMAC signature; restart invalidates outstanding
	// cookies, which is acceptable for a 30-minute TTL.
	Unlock  *unlockSigner
	Audit   audit.Recorder
	Proxies *auth.ProxyTrust // optional; nil = trust no proxy headers
}

// unlockTTL caps how long a successful password unlock stays valid. Long
// enough that recipients can browse a shared file's preview, short enough
// that a stolen cookie is short-lived.
const unlockTTL = 30 * time.Minute

// unlockSigner produces and verifies short-lived HMAC tokens used as cookie
// values. The signature covers the expiry timestamp AND the share code, so a
// cookie minted for share A cannot be replayed under cookie name
// stowage_unlock_B even though the cookie name carries the code — the name
// is a client-side hint, not a security boundary.
type unlockSigner struct {
	secret []byte
}

func newUnlockSigner() (*unlockSigner, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return &unlockSigner{secret: b}, nil
}

// MustNewUnlockSigner panics on rng failure — used at startup where there's
// nothing to fall back to.
func MustNewUnlockSigner() *unlockSigner {
	u, err := newUnlockSigner()
	if err != nil {
		panic(err)
	}
	return u
}

func (u *unlockSigner) issue(now time.Time, code string) string {
	exp := now.Add(unlockTTL).Unix()
	msg := strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, u.secret)
	mac.Write([]byte(code))
	mac.Write([]byte{0})
	mac.Write([]byte(msg))
	return msg + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (u *unlockSigner) verify(cookie string, now time.Time, code string) bool {
	i := strings.LastIndexByte(cookie, '.')
	if i <= 0 {
		return false
	}
	msg, sigB64 := cookie[:i], cookie[i+1:]
	exp, err := strconv.ParseInt(msg, 10, 64)
	if err != nil || exp < now.Unix() {
		return false
	}
	got, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, u.secret)
	mac.Write([]byte(code))
	mac.Write([]byte{0})
	mac.Write([]byte(msg))
	return hmac.Equal(got, mac.Sum(nil))
}

func unlockCookieName(code string) string { return "stowage_unlock_" + code }

// isHTTPS reports whether the request rode over TLS, honouring
// X-Forwarded-Proto only when the immediate peer is in the trust list.
func (d *ShareDeps) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if d.Proxies != nil {
		return d.Proxies.IsHTTPS(r)
	}
	return false
}

// ---- DTOs ---------------------------------------------------------------

type shareDTO struct {
	ID             string `json:"id"`
	Code           string `json:"code"`
	URL            string `json:"url"` // path-only: "/s/<code>"
	BackendID      string `json:"backend_id"`
	Bucket         string `json:"bucket"`
	Key            string `json:"key"`
	CreatedBy      string `json:"created_by"`
	CreatedAt      string `json:"created_at"`
	ExpiresAt      string `json:"expires_at,omitempty"`
	HasPassword    bool   `json:"has_password"`
	MaxDownloads   int64  `json:"max_downloads,omitempty"`
	DownloadCount  int64  `json:"download_count"`
	Revoked        bool   `json:"revoked"`
	RevokedAt      string `json:"revoked_at,omitempty"`
	LastAccessedAt string `json:"last_accessed_at,omitempty"`
	Disposition    string `json:"disposition"`
}

func toShareDTO(s *sqlite.Share) shareDTO {
	d := shareDTO{
		ID:            s.ID,
		Code:          s.Code,
		URL:           "/s/" + s.Code,
		BackendID:     s.BackendID,
		Bucket:        s.Bucket,
		Key:           s.Key,
		CreatedBy:     s.CreatedBy,
		CreatedAt:     s.CreatedAt.UTC().Format(time.RFC3339),
		HasPassword:   s.PasswordHash != "",
		DownloadCount: s.DownloadCount,
		Revoked:       s.Revoked,
		Disposition:   s.Disposition,
	}
	if s.ExpiresAt.Valid {
		d.ExpiresAt = s.ExpiresAt.Time.UTC().Format(time.RFC3339)
	}
	if s.MaxDownloads.Valid {
		d.MaxDownloads = s.MaxDownloads.Int64
	}
	if s.RevokedAt.Valid {
		d.RevokedAt = s.RevokedAt.Time.UTC().Format(time.RFC3339)
	}
	if s.LastAccessedAt.Valid {
		d.LastAccessedAt = s.LastAccessedAt.Time.UTC().Format(time.RFC3339)
	}
	return d
}

// ---- Authenticated endpoints -------------------------------------------

type createShareRequest struct {
	BackendID    string  `json:"backend_id"`
	Bucket       string  `json:"bucket"`
	Key          string  `json:"key"`
	ExpiresAt    *string `json:"expires_at,omitempty"` // RFC3339
	Password     string  `json:"password,omitempty"`
	MaxDownloads int64   `json:"max_downloads,omitempty"`
	Disposition  string  `json:"disposition,omitempty"`
}

func (d *ShareDeps) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	id := auth.IdentityFrom(r.Context())
	if id == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required", "")
		return
	}

	var req createShareRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}
	if !validObjectKey(req.Key) {
		writeError(w, http.StatusBadRequest, "invalid_key", "object key is invalid", "")
		return
	}

	p := shares.CreateParams{
		BackendID:    req.BackendID,
		Bucket:       req.Bucket,
		Key:          req.Key,
		Password:     req.Password,
		MaxDownloads: req.MaxDownloads,
		Disposition:  req.Disposition,
	}
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "expires_at must be RFC3339", "")
			return
		}
		p.ExpiresAt = &t
	}

	sh, err := d.Service.Create(r.Context(), id.UserID, p)
	if err != nil {
		d.writeServiceError(w, err, "create share")
		return
	}
	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "share.create",
		Backend: sh.BackendID,
		Bucket:  sh.Bucket,
		Key:     sh.Key,
		Detail: map[string]any{
			"share_id":      sh.ID,
			"code":          sh.Code,
			"has_password":  sh.PasswordHash != "",
			"max_downloads": sh.MaxDownloads.Int64,
		},
	})
	writeJSON(w, http.StatusCreated, toShareDTO(sh))
}

func (d *ShareDeps) handleListShares(w http.ResponseWriter, r *http.Request) {
	id := auth.IdentityFrom(r.Context())
	if id == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required", "")
		return
	}

	var rows []*sqlite.Share
	var err error
	scope := r.URL.Query().Get("scope")
	if scope == "all" {
		if !id.IsAdmin() {
			writeError(w, http.StatusForbidden, "forbidden", "admin only", "")
			return
		}
		rows, err = d.Service.ListAll(r.Context())
	} else {
		rows, err = d.Service.ListMine(r.Context(), id.UserID)
	}
	if err != nil {
		d.writeServiceError(w, err, "list shares")
		return
	}
	out := make([]shareDTO, 0, len(rows))
	for _, sh := range rows {
		out = append(out, toShareDTO(sh))
	}
	writeJSON(w, http.StatusOK, map[string]any{"shares": out})
}

func (d *ShareDeps) handleRevokeShare(w http.ResponseWriter, r *http.Request) {
	id := auth.IdentityFrom(r.Context())
	if id == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required", "")
		return
	}
	shareID := chi.URLParam(r, "id")
	if shareID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "share id is required", "")
		return
	}
	if err := d.Service.Revoke(r.Context(), id.UserID, id.Role, shareID); err != nil {
		d.writeServiceError(w, err, "revoke share")
		return
	}
	audit.RecordRequest(d.Audit, r, audit.Event{
		Action: "share.revoke",
		Detail: map[string]any{"share_id": shareID},
	})
	w.WriteHeader(http.StatusNoContent)
}

// ---- Public resolver (JSON API + raw bytes) -----------------------------
//
// The recipient-facing UX lives in SvelteKit at /s/[code]. These handlers
// expose just the bare data and side-effects:
//
//   GET  /s/{code}/info    JSON metadata; password-required reported as 401
//   POST /s/{code}/unlock  body {password}; on match sets unlock cookie
//   GET  /s/{code}/raw     streams bytes; gated by the cookie if protected
//
// The SvelteKit page polls /info, posts to /unlock, then redirects the
// recipient to /raw via window.location for the actual download.

type shareInfoDTO struct {
	Code          string `json:"code"`
	Name          string `json:"name"`
	Size          int64  `json:"size"`
	ContentType   string `json:"content_type,omitempty"`
	ETag          string `json:"etag,omitempty"`
	LastModified  string `json:"last_modified,omitempty"`
	ExpiresAt     string `json:"expires_at,omitempty"`
	HasPassword   bool   `json:"has_password"`
	MaxDownloads  int64  `json:"max_downloads,omitempty"`
	DownloadCount int64  `json:"download_count"`
	DownloadsLeft int64  `json:"downloads_left,omitempty"`
	Disposition   string `json:"disposition"`
	PreviewKind   string `json:"preview_kind"` // "image"|"video"|"audio"|"pdf"|"text"|"none"
	RawURL        string `json:"raw_url"`
}

// isActiveContentType reports whether a Content-Type / extension pair could
// execute script when navigated top-level. Used by handleShareRaw to refuse
// inline disposition for SVG, HTML, XML, and xhtml — even on shares where
// the owner asked for inline.
func isActiveContentType(contentType, key string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	switch ct {
	case "image/svg+xml",
		"text/html",
		"application/xhtml+xml",
		"application/xml",
		"text/xml":
		return true
	}
	switch strings.ToLower(path.Ext(key)) {
	case ".svg", ".html", ".htm", ".xhtml", ".xml":
		return true
	}
	return false
}

// previewKind picks the inline preview style based on content type.
// Falls back to extension when the backend didn't surface a content type.
func previewKind(contentType, key string) string {
	ct := strings.ToLower(contentType)
	switch {
	case strings.HasPrefix(ct, "image/"):
		return "image"
	case strings.HasPrefix(ct, "video/"):
		return "video"
	case strings.HasPrefix(ct, "audio/"):
		return "audio"
	case ct == "application/pdf":
		return "pdf"
	case strings.HasPrefix(ct, "text/"),
		ct == "application/json", ct == "application/x-yaml", ct == "application/yaml":
		return "text"
	}
	switch strings.ToLower(path.Ext(key)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".bmp", ".svg":
		return "image"
	case ".mp4", ".webm", ".mov", ".m4v", ".mkv":
		return "video"
	case ".mp3", ".wav", ".ogg", ".flac", ".m4a":
		return "audio"
	case ".pdf":
		return "pdf"
	case ".txt", ".md", ".markdown", ".csv", ".tsv", ".json", ".yaml", ".yml",
		".log", ".ini", ".toml", ".sh", ".js", ".ts", ".css", ".html", ".xml",
		".go", ".py", ".rb", ".rs", ".java", ".c", ".h", ".cpp", ".sql":
		return "text"
	}
	return "none"
}

// shareInfoFor builds the info DTO for a resolved share. The HEAD call is
// cheap; we always do it so the SvelteKit page doesn't have to make a
// follow-up call to populate the preview.
func (d *ShareDeps) shareInfoFor(r *http.Request, sh *sqlite.Share) (shareInfoDTO, error) {
	info, err := d.Service.HeadTarget(r.Context(), sh)
	if err != nil {
		return shareInfoDTO{}, err
	}
	dto := shareInfoDTO{
		Code:          sh.Code,
		Name:          path.Base(sh.Key),
		Size:          info.Size,
		ContentType:   info.ContentType,
		ETag:          info.ETag,
		HasPassword:   sh.PasswordHash != "",
		DownloadCount: sh.DownloadCount,
		Disposition:   sh.Disposition,
		PreviewKind:   previewKind(info.ContentType, sh.Key),
		RawURL:        "/s/" + sh.Code + "/raw",
	}
	if !info.LastModified.IsZero() {
		dto.LastModified = info.LastModified.UTC().Format(time.RFC3339)
	}
	if sh.ExpiresAt.Valid {
		dto.ExpiresAt = sh.ExpiresAt.Time.UTC().Format(time.RFC3339)
	}
	if sh.MaxDownloads.Valid {
		dto.MaxDownloads = sh.MaxDownloads.Int64
		left := sh.MaxDownloads.Int64 - sh.DownloadCount
		if left < 0 {
			left = 0
		}
		dto.DownloadsLeft = left
	}
	return dto, nil
}

// resolveWithCookie tries Lookup first when the share is password-protected
// and the unlock cookie is valid; falls back to Resolve("") otherwise.
// Returns the share, whether the unlock path succeeded, and any error.
func (d *ShareDeps) resolveWithCookie(r *http.Request, code string) (*sqlite.Share, error) {
	if c, err := r.Cookie(unlockCookieName(code)); err == nil && d.Unlock.verify(c.Value, time.Now(), code) {
		// Cookie present and signature valid — bypass the password check
		// but still enforce the rest of the gates.
		sh, err := d.Service.Lookup(r.Context(), code)
		if err != nil {
			return nil, err
		}
		return sh, nil
	}
	return d.Service.Resolve(r.Context(), code, "")
}

func (d *ShareDeps) handleShareInfo(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if code == "" {
		writeError(w, http.StatusNotFound, "not_found", "share not found", "")
		return
	}

	sh, err := d.resolveWithCookie(r, code)
	if err != nil {
		d.writeResolverError(w, err)
		return
	}
	dto, err := d.shareInfoFor(r, sh)
	if err != nil {
		d.Logger.Warn("share head target failed", "code", code, "err", err.Error())
		writeError(w, http.StatusBadGateway, "backend_error", "could not read shared object", "")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, dto)
}

type unlockRequest struct {
	Password string `json:"password"`
}

func (d *ShareDeps) handleShareUnlock(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if code == "" {
		writeError(w, http.StatusNotFound, "not_found", "share not found", "")
		return
	}

	var req unlockRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", "")
		return
	}

	sh, err := d.Service.Resolve(r.Context(), code, req.Password)
	if err != nil {
		// ErrPasswordRequired with empty password collapses to ErrPasswordMismatch
		// from the recipient's perspective — same outcome.
		if errors.Is(err, shares.ErrPasswordRequired) || errors.Is(err, shares.ErrPasswordMismatch) {
			writeError(w, http.StatusUnauthorized, "password_mismatch", "wrong password", "")
			return
		}
		d.writeResolverError(w, err)
		return
	}

	// Mint the unlock cookie. Path scoped to /s/<code> so it's only sent
	// when relevant.
	http.SetCookie(w, &http.Cookie{
		Name:     unlockCookieName(code),
		Value:    d.Unlock.issue(time.Now(), code),
		Path:     "/s/" + code,
		HttpOnly: true,
		Secure:   d.isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(unlockTTL / time.Second),
	})

	dto, err := d.shareInfoFor(r, sh)
	if err != nil {
		d.Logger.Warn("share head target failed", "code", code, "err", err.Error())
		writeError(w, http.StatusBadGateway, "backend_error", "could not read shared object", "")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, dto)
}

func (d *ShareDeps) handleShareRaw(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if code == "" {
		http.NotFound(w, r)
		return
	}

	sh, err := d.resolveWithCookie(r, code)
	if err != nil {
		d.writeResolverError(w, err)
		return
	}

	// Range support — required by HTML5 <video>/<audio> for playback to
	// start cleanly. We always advertise Accept-Ranges so browsers know
	// they can seek; on quota-limited shares the SvelteKit page hides the
	// preview anyway, so a recipient won't fan out into many Range
	// requests by accident.
	//
	// HEAD up front to get the total object size — required for the
	// Content-Range header on a 206 response. Without Content-Range,
	// strict browsers (Safari especially) refuse to start playback.
	rng := parseRangeHeader(r.Header.Get("Range"))
	var totalSize int64
	if rng != nil {
		head, err := d.Service.HeadTarget(r.Context(), sh)
		if err != nil {
			d.Logger.Warn("share head failed", "code", code, "err", err.Error())
			writeError(w, http.StatusBadGateway, "backend_error", "could not fetch the shared file", "")
			return
		}
		totalSize = head.Size
		// Clamp the range against the actual size so a request like
		// "bytes=0-" turns into a concrete end byte.
		if rng.End < 0 || rng.End >= totalSize {
			rng.End = totalSize - 1
		}
		if rng.Start < 0 || rng.Start > rng.End {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", totalSize))
			writeError(w, http.StatusRequestedRangeNotSatisfiable, "bad_range", "range not satisfiable", "")
			return
		}
	}

	reader, err := d.Service.OpenTarget(r.Context(), sh, rng)
	if err != nil {
		d.Logger.Warn("share open target failed", "code", code, "err", err.Error())
		writeError(w, http.StatusBadGateway, "backend_error", "could not fetch the shared file", "")
		return
	}
	defer reader.Close()

	info := reader.Info()
	if info.ContentType != "" {
		w.Header().Set("Content-Type", info.ContentType)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	if info.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	}
	w.Header().Set("Accept-Ranges", "bytes")
	// Allow the SvelteKit page to override disposition via ?inline=1 — useful
	// for inline previews (<img src=…/raw>) on shares that default to
	// attachment. Only inline-vs-attachment is honoured; filename stays.
	disp := sh.Disposition
	if r.URL.Query().Get("inline") == "1" {
		disp = "inline"
	}
	// Active-content types (SVG / HTML / XML / xhtml) execute script when
	// rendered inline on this origin — even with HttpOnly session cookies
	// the script would run as the recipient on stowage's origin, so it
	// could read /api/me, list shares, etc. Force attachment regardless of
	// the share's setting or ?inline=1.
	if isActiveContentType(info.ContentType, sh.Key) {
		disp = "attachment"
	}
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`%s; filename="%s"`, disp, sanitizeFilename(path.Base(sh.Key))))
	w.Header().Set("Cache-Control", "no-store")
	// Defense-in-depth: stop the browser from sniffing a benign Content-Type
	// up to text/html, and sandbox any HTML/SVG that does end up rendered so
	// scripts, plugins, and same-origin XHR are all neutralised.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'; img-src data:")

	// Record the access BEFORE streaming finishes so the counter reflects
	// started downloads even if the client disconnects mid-stream. The SQL
	// UPDATE enforces the cap atomically so concurrent resolvers can't race
	// past max_downloads.
	if err := d.Service.RecordAccess(r.Context(), sh.ID); err != nil {
		writeError(w, http.StatusGone, "exhausted", "this share link is no longer available", "")
		return
	}
	if rng != nil {
		w.Header().Set("Content-Range",
			fmt.Sprintf("bytes %d-%d/%d", rng.Start, rng.End, totalSize))
		w.WriteHeader(http.StatusPartialContent)
	}
	// Audit the public access — recipients of share links are not signed
	// in, so UserID stays empty; the IP + user-agent are the operator's
	// only forensic handle.
	audit.RecordRequest(d.Audit, r, audit.Event{
		Action:  "share.access",
		Backend: sh.BackendID,
		Bucket:  sh.Bucket,
		Key:     sh.Key,
		Detail:  map[string]any{"share_id": sh.ID, "code": sh.Code},
	})
	_, _ = io.Copy(w, reader)
}

// writeResolverError maps shares package errors to HTTP responses for the
// JSON API. The body uses the standard {error:{code,message}} shape so the
// SvelteKit page can branch on `code`.
func (d *ShareDeps) writeResolverError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, shares.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "share not found", "")
	case errors.Is(err, shares.ErrRevoked):
		writeError(w, http.StatusGone, "revoked", "share revoked", "")
	case errors.Is(err, shares.ErrExpired):
		writeError(w, http.StatusGone, "expired", "share expired", "")
	case errors.Is(err, shares.ErrExhausted):
		writeError(w, http.StatusGone, "exhausted", "download limit reached", "")
	case errors.Is(err, shares.ErrPasswordRequired):
		writeError(w, http.StatusUnauthorized, "password_required", "password required", "")
	case errors.Is(err, shares.ErrPasswordMismatch):
		writeError(w, http.StatusUnauthorized, "password_mismatch", "wrong password", "")
	default:
		d.Logger.Warn("share resolve failed", "err", err.Error())
		writeError(w, http.StatusBadGateway, "backend_error", "could not fetch share", "")
	}
}

func (d *ShareDeps) writeServiceError(w http.ResponseWriter, err error, op string) {
	switch {
	case errors.Is(err, shares.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "share not found", "")
	case errors.Is(err, shares.ErrInvalidParams):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), "")
	case errors.Is(err, shares.ErrBackendGone):
		writeError(w, http.StatusBadGateway, "backend_error", "backend unavailable", "")
	default:
		d.Logger.Warn("share op failed", "op", op, "err", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "internal error", "")
	}
}
