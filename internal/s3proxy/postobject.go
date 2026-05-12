// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/stowage-dev/stowage/internal/sigv4verifier"
)

// Per-part and aggregate caps on multipart parsing. POST Object forms in
// the wild have ~10 small fields plus the file; even generous limits stay
// far below the values these caps enforce.
const (
	// maxFieldBytes is the cap on any single non-file form field. Policy
	// documents are bounded separately by MaxPolicyBytes (64 KiB); other
	// fields are short identifiers, MIME types, or metadata strings.
	maxFieldBytes = 64 * 1024

	// maxNonFileBytes is the aggregate cap across every non-file field in
	// a single form. The file part itself is unbounded here — it's
	// streamed and limited by the policy's content-length-range.
	maxNonFileBytes = 1 * 1024 * 1024

	// maxFormFields is the cap on the number of non-file fields. Forms
	// with more than this are almost certainly malicious; AWS doesn't
	// document a hard limit but real-world forms have <20 fields.
	maxFormFields = 64

	// defaultFileMax is the per-upload ceiling when the policy carries no
	// content-length-range. Refusing unbounded uploads is safer than
	// trusting a missing condition; this matches S3's documented 5 GiB
	// single-PUT cap.
	defaultFileMax = 5 * 1024 * 1024 * 1024
)

// errFileTooLarge is signaled by countingLimitedReader when the file
// stream exceeds the policy's content-length-range max. It surfaces to
// the proxy as a 4xx response and the upstream PUT is aborted mid-stream.
var errFileTooLarge = errors.New("post object: file exceeds content-length-range max")

// countingLimitedReader wraps the multipart file part with a hard byte
// cap and a count. On overrun it returns errFileTooLarge so the
// outbound RoundTrip aborts. Total is read after the upstream call to
// drive the post-hoc min check and to size the quota update.
type countingLimitedReader struct {
	r      io.Reader
	max    int64
	total  int64
	closed bool
}

func (c *countingLimitedReader) Read(p []byte) (int, error) {
	if c.closed {
		return 0, io.EOF
	}
	if c.total >= c.max {
		// One byte over the cap is enough to know the file is too large.
		// Return an error so the http.Transport aborts the request.
		return 0, errFileTooLarge
	}
	// Cap the read so we never let through more than max+0 bytes.
	remaining := c.max - c.total
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := c.r.Read(p)
	c.total += int64(n)
	return n, err
}

// servePostObject handles a browser-form POST Object request. It runs
// entirely outside the SigV4-header / presigned auth paths because the
// signature it must verify lives in the form body, not in headers.
//
// Flow:
//  1. Stream-parse the multipart body, collecting non-file fields up to
//     the file part (which by spec is last).
//  2. Verify the policy signature using the cred in x-amz-credential.
//  3. Look up the virtual credential, rate-limit, enforce bucket scope.
//  4. Decode the policy, enforce expiration and conditions.
//  5. Quota pre-check using the policy's content-length-range max.
//  6. Forward the file part upstream as PUT <bucket>/<key>, re-signed
//     with admin creds.
//  7. Translate the upstream response according to success_action_*.
//  8. Record quota and audit.
func (s *Server) servePostObject(w http.ResponseWriter, r *http.Request, route RouteInfo, reqID string) servedRequest {
	out := servedRequest{operation: "PostObject", authMode: "post-policy", bucket: route.Bucket}

	if route.Bucket == "" {
		writeS3Error(w, http.StatusBadRequest, "InvalidRequest", "POST Object requires a bucket", r.URL.Path, reqID)
		out.status, out.result = http.StatusBadRequest, "no-bucket"
		return out
	}

	mr, err := r.MultipartReader()
	if err != nil {
		writeS3Error(w, http.StatusBadRequest, "MalformedPOSTRequest", err.Error(), r.URL.Path, reqID)
		out.status, out.result = http.StatusBadRequest, "bad-multipart"
		return out
	}

	fields, filePart, fileName, parseErr := readPostForm(mr)
	if parseErr != nil {
		writeS3Error(w, http.StatusBadRequest, "MalformedPOSTRequest", parseErr.Error(), r.URL.Path, reqID)
		out.status, out.result = http.StatusBadRequest, "bad-form"
		return out
	}
	if filePart == nil {
		writeS3Error(w, http.StatusBadRequest, "MalformedPOSTRequest", "form has no file field", r.URL.Path, reqID)
		out.status, out.result = http.StatusBadRequest, "no-file"
		return out
	}
	defer filePart.Close()

	auth := sigv4verifier.PolicyAuth{
		Algorithm:  fields["x-amz-algorithm"],
		Credential: fields["x-amz-credential"],
		Date:       fields["x-amz-date"],
		Signature:  fields["x-amz-signature"],
		Policy:     fields["policy"],
	}
	res, err := s.verifier.VerifyPolicy(auth)
	if err != nil {
		out.status, out.result = s.writeAuthFailure(w, err, reqID)
		return out
	}
	out.akid = res.AccessKeyID

	vc, ok := s.cfg.Source.Lookup(res.AccessKeyID)
	if !ok {
		s.cfg.Metrics.AuthFailures.WithLabelValues("unknown-akid").Inc()
		writeS3Error(w, http.StatusForbidden, "InvalidAccessKeyId", "access key not recognized", r.URL.Path, reqID)
		out.status, out.result = http.StatusForbidden, "unknown-akid"
		return out
	}
	out.backend = vc.BackendName
	out.source = vc.Source
	out.claimNS = vc.ClaimNamespace
	out.claimName = vc.ClaimName

	if !s.cfg.Limiter.Allow(vc.AccessKeyID) {
		writeS3Error(w, http.StatusServiceUnavailable, "SlowDown", "rate limit exceeded", r.URL.Path, reqID)
		out.status, out.result = http.StatusServiceUnavailable, "rate-limited"
		return out
	}

	if !EnforceScope(vc.BucketScopes, route.Bucket) {
		s.cfg.Metrics.ScopeViolations.Inc()
		writeS3Error(w, http.StatusForbidden, "AccessDenied", "credential is not scoped to this bucket", r.URL.Path, reqID)
		out.status, out.result = http.StatusForbidden, "scope-violation"
		return out
	}

	// Decode and parse the policy. The signature has already been
	// verified, so the bytes are trusted to be from the credential owner.
	policy, err := ParsePostPolicy(auth.Policy, time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, ErrPolicyExpired):
			writeS3Error(w, http.StatusForbidden, "AccessDenied", "post policy expired", r.URL.Path, reqID)
			out.status, out.result = http.StatusForbidden, "policy-expired"
		default:
			writeS3Error(w, http.StatusBadRequest, "MalformedPOSTRequest", err.Error(), r.URL.Path, reqID)
			out.status, out.result = http.StatusBadRequest, "bad-policy"
		}
		return out
	}

	// The bucket is conveyed by the URL, not by a form field. If the
	// form does carry an explicit `bucket` field, it must agree with the
	// URL bucket — AWS rejects mismatches outright. Synthesize the field
	// from the route so policy `{"bucket": "..."}` conditions validate.
	if formBucket, ok := fields["bucket"]; ok && formBucket != route.Bucket {
		writeS3Error(w, http.StatusForbidden, "AccessDenied",
			fmt.Sprintf("form bucket %q does not match URL bucket %q", formBucket, route.Bucket),
			r.URL.Path, reqID)
		out.status, out.result = http.StatusForbidden, "bucket-mismatch"
		return out
	}
	fields["bucket"] = route.Bucket

	if err := EnforcePolicy(policy, fields); err != nil {
		writeS3Error(w, http.StatusForbidden, "AccessDenied", err.Error(), r.URL.Path, reqID)
		out.status, out.result = http.StatusForbidden, "policy-violation"
		return out
	}

	key, ok := fields["key"]
	if !ok || key == "" {
		writeS3Error(w, http.StatusBadRequest, "MalformedPOSTRequest", "form is missing key field", r.URL.Path, reqID)
		out.status, out.result = http.StatusBadRequest, "no-key"
		return out
	}
	// AWS lets the client use "${filename}" as a placeholder that gets
	// replaced with the uploaded file's filename. Useful for forms that
	// take whatever the user drops in.
	key = strings.ReplaceAll(key, "${filename}", fileName)
	out.key = key

	// Validate the optional success_action_redirect before we forward,
	// so a malformed URL surfaces as a clean 400 rather than a 5xx after
	// the upstream commit.
	redirect, err := parseRedirect(fields["success_action_redirect"])
	if err != nil {
		writeS3Error(w, http.StatusBadRequest, "MalformedPOSTRequest", err.Error(), r.URL.Path, reqID)
		out.status, out.result = http.StatusBadRequest, "bad-redirect"
		return out
	}

	// Quota pre-check uses the policy's content-length-range max — the
	// only declared upper bound we have before the body streams. Refuse
	// uploads without a max condition: silently allowing unbounded
	// uploads would defeat the quota.
	uploadMax := int64(defaultFileMax)
	if policy.HasContentLengthRange() {
		uploadMax = policy.ContentLengthMax
	}
	if s.cfg.Quotas != nil {
		if err := s.cfg.Quotas.CheckUpload(r.Context(), vc.BackendName, route.Bucket, uploadMax); err != nil {
			writeS3Error(w, http.StatusInsufficientStorage, "EntityTooLarge", err.Error(), r.URL.Path, reqID)
			out.status, out.result = http.StatusInsufficientStorage, "quota-exceeded"
			return out
		}
	}

	backendURL, spec, err := s.cfg.Backends.Backend(r.Context(), vc.BackendName)
	if err != nil {
		writeS3Error(w, http.StatusBadGateway, "InternalError", err.Error(), r.URL.Path, reqID)
		out.status, out.result = http.StatusBadGateway, "backend-unavailable"
		return out
	}

	// Wrap the file part so the upstream PUT aborts cleanly if the body
	// goes past the policy's max, and so we know the actual size after.
	counter := &countingLimitedReader{r: filePart, max: uploadMax + 1}

	outReq, err := s.buildPostUpstream(r.Context(), backendURL, route.Bucket, key, counter, fields)
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error(), r.URL.Path, reqID)
		out.status, out.result = http.StatusInternalServerError, "build-error"
		return out
	}

	if err := s.signOutbound(r.Context(), outReq, vc.BackendName, spec); err != nil {
		writeS3Error(w, http.StatusBadGateway, "InternalError", fmt.Sprintf("sign outbound: %v", err), r.URL.Path, reqID)
		out.status, out.result = http.StatusBadGateway, "sign-error"
		return out
	}

	upStart := time.Now()
	resp, err := s.transport.RoundTrip(outReq)
	s.cfg.Metrics.Upstream.WithLabelValues(out.operation).Observe(time.Since(upStart).Seconds())
	if err != nil {
		// Distinguish a client-supplied oversized file from a generic
		// upstream failure so the audit row and status code are honest.
		if errors.Is(err, errFileTooLarge) {
			writeS3Error(w, http.StatusRequestEntityTooLarge, "EntityTooLarge",
				fmt.Sprintf("file exceeds policy max of %d bytes", uploadMax),
				r.URL.Path, reqID)
			out.status, out.result = http.StatusRequestEntityTooLarge, "file-too-large"
			return out
		}
		writeS3Error(w, http.StatusBadGateway, "InternalError", err.Error(), r.URL.Path, reqID)
		out.status, out.result = http.StatusBadGateway, "upstream-error"
		return out
	}
	defer resp.Body.Close()

	// Now we know the actual file size; enforce the policy's lower bound.
	// The upload already committed — best-effort delete the orphan so
	// undersize uploads don't leave junk in the bucket. If cleanup fails
	// we keep the client-facing error but log loud enough for an
	// operator to notice; the next reconciliation/quota scan will
	// surface the orphan.
	if policy.HasContentLengthRange() && counter.total < policy.ContentLengthMin {
		writeS3Error(w, http.StatusBadRequest, "EntityTooSmall",
			fmt.Sprintf("file size %d below policy min %d", counter.total, policy.ContentLengthMin),
			r.URL.Path, reqID)
		out.status, out.result = http.StatusBadRequest, "file-too-small"
		// resp.Body must be drained before another request reuses the
		// connection; do that before we issue the DELETE so the same
		// keep-alive can carry it.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if delErr := s.deleteUpstreamObject(r.Context(), backendURL, vc.BackendName, spec, route.Bucket, key); delErr != nil {
			s.cfg.Log.Info("post-object orphan cleanup failed",
				"backend", vc.BackendName, "bucket", route.Bucket, "key", key,
				"bytes", counter.total, "min", policy.ContentLengthMin,
				"cleanup_err", delErr.Error(), "request_id", reqID)
		} else {
			s.cfg.Log.Info("post-object min violation; orphan cleaned up",
				"backend", vc.BackendName, "bucket", route.Bucket, "key", key,
				"bytes", counter.total, "min", policy.ContentLengthMin, "request_id", reqID)
		}
		return out
	}

	if resp.StatusCode >= 400 {
		s.streamResponse(w, resp, outReq, out.operation, reqID)
		out.status, out.result = resp.StatusCode, "upstream-error"
		return out
	}

	etag := resp.Header.Get("ETag")
	versionID := resp.Header.Get("x-amz-version-id")

	// Drain and discard the upstream body — POST responses don't carry
	// the upstream PUT's empty body to the client.
	_, _ = io.Copy(io.Discard, resp.Body)

	status := writePostSuccess(w, route, key, etag, versionID, fields, redirect, r, s.cfg.PublicHostname)
	out.status, out.result = status, "ok"

	if s.cfg.Quotas != nil && counter.total > 0 {
		s.cfg.Quotas.Recorded(vc.BackendName, route.Bucket, counter.total)
	}

	return out
}

// readPostForm walks the multipart body. Returns the collected non-file
// fields (lowercased keys), the file part still open for streaming, and
// the filename declared in the file part's Content-Disposition (used to
// expand ${filename} in the key). The file part by AWS convention is the
// last part — when encountered we stop iteration and hand the open part
// back to the caller.
func readPostForm(mr *multipart.Reader) (map[string]string, *multipart.Part, string, error) {
	fields := make(map[string]string)
	var aggregate int64

	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			return fields, nil, "", nil
		}
		if err != nil {
			return nil, nil, "", fmt.Errorf("read part: %w", err)
		}

		name := strings.ToLower(part.FormName())
		if name == "" {
			part.Close()
			continue
		}
		if name == "file" {
			return fields, part, part.FileName(), nil
		}

		if len(fields) >= maxFormFields {
			part.Close()
			return nil, nil, "", fmt.Errorf("form has more than %d fields", maxFormFields)
		}

		// Cap each field, plus the cross-field aggregate, so a hostile
		// form can't OOM us with many medium-sized fields.
		buf, err := io.ReadAll(io.LimitReader(part, maxFieldBytes+1))
		part.Close()
		if err != nil {
			return nil, nil, "", fmt.Errorf("read field %q: %w", name, err)
		}
		if len(buf) > maxFieldBytes {
			return nil, nil, "", fmt.Errorf("form field %q exceeds %d bytes", name, maxFieldBytes)
		}
		aggregate += int64(len(buf))
		if aggregate > maxNonFileBytes {
			return nil, nil, "", fmt.Errorf("non-file fields exceed %d bytes total", maxNonFileBytes)
		}

		fields[name] = string(buf)
	}
}

// parseRedirect validates and normalizes a success_action_redirect form
// value. Allowed schemes are http and https only — anything else risks
// turning the proxy into an open redirector into a non-web protocol. An
// empty input is fine; the caller treats absent redirect as "use
// success_action_status / default".
func parseRedirect(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("success_action_redirect: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("success_action_redirect scheme %q not allowed", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("success_action_redirect missing host")
	}
	return u, nil
}

// buildPostUpstream constructs the PUT request that carries the form's
// file part to the backend. Headers are derived from the form fields the
// policy permitted: Content-Type, Cache-Control, Content-Encoding,
// Content-Disposition, Expires, x-amz-acl, x-amz-storage-class,
// x-amz-server-side-encryption, x-amz-meta-*. Everything else is dropped.
//
// ContentLength is intentionally -1: the file size is not known when
// the PUT is dispatched (it's still being streamed out of the multipart
// reader), so Go uses Transfer-Encoding: chunked. The upstream signer
// uses UNSIGNED-PAYLOAD, which is compatible with chunked.
func (s *Server) buildPostUpstream(ctx context.Context, backendURL *url.URL, bucket, key string, body io.Reader, fields map[string]string) (*http.Request, error) {
	outURL := *backendURL
	basePath := strings.TrimRight(outURL.Path, "/")
	outURL.Path = basePath + BuildOutboundPath(bucket, key)
	outURL.RawPath = basePath + BuildOutboundRawPath(bucket, key)
	outURL.RawQuery = ""

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, outURL.String(), body)
	if err != nil {
		return nil, err
	}
	req.ContentLength = -1
	req.Header = make(http.Header)
	req.Header.Set("Host", backendURL.Host)
	req.Host = backendURL.Host

	if v := fields["content-type"]; v != "" {
		req.Header.Set("Content-Type", v)
	} else {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	for _, h := range []string{
		"cache-control", "content-disposition", "content-encoding", "expires",
	} {
		if v := fields[h]; v != "" {
			req.Header.Set(h, v)
		}
	}
	for _, h := range []string{
		"x-amz-acl", "x-amz-storage-class", "x-amz-server-side-encryption",
		"x-amz-website-redirect-location", "x-amz-tagging",
	} {
		if v := fields[h]; v != "" {
			req.Header.Set(h, v)
		}
	}
	// User metadata: every form field whose name starts with x-amz-meta-.
	for k, v := range fields {
		if strings.HasPrefix(k, "x-amz-meta-") {
			req.Header.Set(k, v)
		}
	}
	return req, nil
}

// deleteUpstreamObject issues a best-effort signed DELETE for an
// already-committed object. Used to clean up the orphan a post-hoc
// content-length-range min violation leaves behind. Returns the first
// error encountered — caller decides whether to surface or log it.
func (s *Server) deleteUpstreamObject(ctx context.Context, backendURL *url.URL, backendName string, spec BackendSpec, bucket, key string) error {
	outURL := *backendURL
	basePath := strings.TrimRight(outURL.Path, "/")
	outURL.Path = basePath + BuildOutboundPath(bucket, key)
	outURL.RawPath = basePath + BuildOutboundRawPath(bucket, key)
	outURL.RawQuery = ""

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, outURL.String(), nil)
	if err != nil {
		return fmt.Errorf("build delete: %w", err)
	}
	req.Header = make(http.Header)
	req.Header.Set("Host", backendURL.Host)
	req.Host = backendURL.Host
	if err := s.signOutbound(ctx, req, backendName, spec); err != nil {
		return fmt.Errorf("sign delete: %w", err)
	}
	resp, err := s.transport.RoundTrip(req)
	if err != nil {
		return fmt.Errorf("send delete: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound {
		// 404 is fine — the object never made it upstream (e.g. backend
		// rejected the PUT after partial commit). Any other 4xx/5xx is
		// a real failure worth logging.
		return fmt.Errorf("delete returned %d", resp.StatusCode)
	}
	return nil
}

// postObjectResponse is the body S3 returns for success_action_status=201.
type postObjectResponse struct {
	XMLName  xml.Name `xml:"PostResponse"`
	Location string   `xml:"Location"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	ETag     string   `xml:"ETag"`
}

// writePostSuccess emits the response per the form's success_action_*
// fields. Precedence: redirect > status > default (204).
//
// The 201 Location URL is built from the inbound request's Host and
// scheme so it matches the URL the client actually reached the proxy on.
// X-Forwarded-Proto is honored on plain-HTTP inbound requests (typical
// when a TLS-terminating ingress sits in front of stowage).
func writePostSuccess(w http.ResponseWriter, route RouteInfo, key, etag, versionID string, fields map[string]string, redirect *url.URL, r *http.Request, publicHostname string) int {
	if etag != "" {
		w.Header().Set("ETag", etag)
	}
	if versionID != "" {
		w.Header().Set("x-amz-version-id", versionID)
	}

	if redirect != nil {
		q := redirect.Query()
		q.Set("bucket", route.Bucket)
		q.Set("key", key)
		if etag != "" {
			// AWS strips the surrounding double-quotes in the query value.
			q.Set("etag", strings.Trim(etag, `"`))
		}
		dest := *redirect
		dest.RawQuery = q.Encode()
		w.Header().Set("Location", dest.String())
		w.WriteHeader(http.StatusSeeOther)
		return http.StatusSeeOther
	}

	status := fields["success_action_status"]
	switch status {
	case "200":
		w.WriteHeader(http.StatusOK)
		return http.StatusOK
	case "201":
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusCreated)
		_ = xml.NewEncoder(w).Encode(postObjectResponse{
			Location: inboundObjectLocation(r, route, key, publicHostname),
			Bucket:   route.Bucket,
			Key:      key,
			ETag:     etag,
		})
		return http.StatusCreated
	default:
		// Default (no field, or any unrecognized value including "204") is
		// 204 No Content. AWS behaves the same.
		w.WriteHeader(http.StatusNoContent)
		return http.StatusNoContent
	}
}

// inboundObjectLocation builds the public URL of the just-uploaded
// object as it would be addressed against the same proxy endpoint the
// client just POSTed to. Scheme honors X-Forwarded-Proto when the
// inbound connection is plain HTTP (the common case behind an ingress);
// otherwise it tracks r.TLS.
//
// Path layout follows the inbound addressing style: virtual-hosted
// requests put the bucket in the host, so the URL path is just the key;
// path-style requests prefix the path with the bucket.
//
// publicHostname, when non-empty, overrides r.Host. Path-style requests
// substitute the host directly; virtual-hosted requests prepend the
// bucket subdomain to the configured value so the Location still
// addresses the right bucket.
func inboundObjectLocation(r *http.Request, route RouteInfo, key, publicHostname string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	} else if xp := strings.ToLower(r.Header.Get("X-Forwarded-Proto")); xp == "https" {
		scheme = "https"
	}
	host := r.Host
	if publicHostname != "" {
		if route.PathStyle {
			host = publicHostname
		} else {
			host = route.Bucket + "." + publicHostname
		}
	}
	loc := &url.URL{Scheme: scheme, Host: host}
	if route.PathStyle {
		loc.Path = BuildOutboundPath(route.Bucket, key)
		loc.RawPath = BuildOutboundRawPath(route.Bucket, key)
	} else {
		loc.Path = "/" + key
		// awsKeyEscape preserves slashes inside the key while percent-encoding
		// reserved characters per AWS SigV4 canonical-URI rules.
		loc.RawPath = "/" + awsKeyEscape(key)
	}
	return loc.String()
}
