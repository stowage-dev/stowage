// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/go-logr/logr"

	"github.com/stowage-dev/stowage/internal/audit"
	"github.com/stowage-dev/stowage/internal/sigv4verifier"
)

// QuotaEnforcer is the subset of the quotas service the proxy needs. Kept
// as a small interface so the package doesn't import the whole quotas
// package and so tests can plug in trivial mocks.
type QuotaEnforcer interface {
	CheckUpload(ctx context.Context, backendID, bucket string, addBytes int64) error
	Recorded(backendID, bucket string, addBytes int64)
}

// uploadOps is the set of classified S3 operations that consume bucket
// quota. CompleteMultipartUpload is intentionally omitted — its part
// uploads have already been quota-checked individually, and the final
// assembly carries no Content-Length we can pre-evaluate.
var uploadOps = map[string]struct{}{
	"PutObject":  {},
	"UploadPart": {},
}

func isUploadOp(op string) bool { _, ok := uploadOps[op]; return ok }

// Config governs server behavior. All fields are required unless noted.
type Config struct {
	// Source is the merged credential lookup. The proxy queries it for
	// every signed request (Lookup) and every unauthenticated request
	// (LookupAnon).
	Source        Source
	Backends      *BackendResolver
	Limiter       *Limiter
	IPLimiter     *IPLimiter
	Metrics       *Metrics
	Log           logr.Logger
	HostSuffixes  []string
	RequestIDFn   func() string
	BucketCreated time.Time

	// AnonymousEnabled is the cluster-wide kill switch. When false, the
	// proxy never enters the anonymous path even if a binding exists.
	AnonymousEnabled bool

	// TrustedProxies, when non-nil, is the set of CIDRs whose
	// X-Forwarded-For values the proxy honors for client-IP extraction.
	// nil means trust X-Forwarded-For unconditionally.
	TrustedProxies []*net.IPNet

	// Audit, when non-nil, receives one event per request after the
	// upstream response has been streamed back. UserID is always empty —
	// proxy events never tie back to a session. The credential's
	// access_key (and, for K8s-sourced credentials, the claim namespace /
	// name) are written to Detail for attribution.
	Audit audit.Recorder

	// Quotas, when non-nil, is consulted before forwarding upload
	// operations and updated on success. PUT/UploadPart respect the
	// pre-check; CompleteMultipartUpload is not pre-checked because the
	// individual parts already were.
	Quotas QuotaEnforcer

	// SuccessReadAuditRate is the fraction of read-shaped (GET / HEAD)
	// requests with a successful response that are written to the audit
	// log. 0.0 skips every successful read; 1.0 records every event.
	// Writes, deletes, and any non-2xx response are always recorded
	// regardless. Out-of-range values are clamped to [0.0, 1.0].
	//
	// Default 0.0: per-request access trail for reads is rarely useful
	// for forensics versus the audit-pipeline CPU it costs at scale.
	SuccessReadAuditRate float64

	// AdminCredsOverride, if non-nil, bypasses the BackendResolver's admin
	// credential lookup. Used by tests so the proxy handler can run against
	// a real object-store backend without standing up stowage's full
	// backend registry.
	AdminCredsOverride func(context.Context, BackendSpec) (aws.Credentials, error)
}

// Server implements http.Handler.
type Server struct {
	cfg       Config
	verifier  *sigv4verifier.Verifier
	transport http.RoundTripper

	admCredsForOverride func(context.Context, BackendSpec) (aws.Credentials, error)
}

// NewServer constructs a proxy server.
func NewServer(cfg Config) *Server {
	if cfg.RequestIDFn == nil {
		cfg.RequestIDFn = defaultRequestID
	}
	s := &Server{
		cfg: cfg,
		verifier: &sigv4verifier.Verifier{
			Resolver: sigv4verifier.ResolverFunc(func(akid string) (string, bool) {
				vc, ok := cfg.Source.Lookup(akid)
				if !ok {
					return "", false
				}
				return vc.SecretAccessKey, true
			}),
		},
		// Bespoke transport for the upstream pool. Go's DefaultTransport
		// caps idle conns per host at 2, which forces a fresh TCP dial on
		// almost every request once concurrency exceeds the cap; profiling
		// under bench load showed ~50% of CPU spent in syscall.connect().
		// Sizing the idle pool to the expected per-key concurrency avoids
		// that and lets keepalive carry steady-state load.
		transport: &http.Transport{
			MaxIdleConns:        512,
			MaxIdleConnsPerHost: 256,
			MaxConnsPerHost:     256,
			IdleConnTimeout:     90 * time.Second,
			ForceAttemptHTTP2:   true,
		},
	}
	if cfg.AdminCredsOverride != nil {
		s.admCredsForOverride = cfg.AdminCredsOverride
	}
	return s
}

// InvalidateSigningKeys drops the verifier's cached SigV4 signing keys.
// Wired into SQLiteSource.SetOnReload by the surrounding server package
// so a deleted/disabled/expired credential's derived key cannot serve a
// subsequent forged request even within the same UTC day.
func (s *Server) InvalidateSigningKeys() {
	s.verifier.InvalidateSigningKeys()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	reqID := s.cfg.RequestIDFn()
	w.Header().Set("x-amz-request-id", reqID)

	s.cfg.Metrics.Inflight.Inc()
	defer s.cfg.Metrics.Inflight.Dec()
	s.cfg.Metrics.CacheSize.Set(float64(s.cfg.Source.Size()))

	out := s.serve(w, r, reqID)
	elapsed := time.Since(start).Seconds()
	s.cfg.Metrics.Requests.WithLabelValues(r.Method, out.operation, strconv.Itoa(out.status), out.result, out.authMode).Inc()
	s.cfg.Metrics.Duration.WithLabelValues(out.operation).Observe(elapsed)

	// Successful responses log at higher verbosity so routine traffic
	// doesn't drown out the default stream. Set log.level: debug to see them.
	log := s.cfg.Log
	if out.status < 400 {
		log = log.V(1)
	}
	log.Info("request",
		"method", r.Method,
		"host", r.Host,
		"path", RedactPath(r.URL),
		"status", out.status,
		"operation", out.operation,
		"result", out.result,
		"auth_mode", out.authMode,
		"latency_ms", time.Since(start).Milliseconds(),
		"request_id", reqID,
	)

	if s.cfg.Audit != nil && s.shouldRecordAudit(r, out) {
		s.recordAudit(r, reqID, out)
	}
}

// shouldRecordAudit decides whether an event reaches the audit pipeline.
// Events that fail (any non-ok result) and writes (non-GET/HEAD methods)
// are always recorded — those are the security-relevant rows. Successful
// reads are sampled at SuccessReadAuditRate.
func (s *Server) shouldRecordAudit(r *http.Request, out servedRequest) bool {
	if out.result != "ok" {
		return true
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		// fall through to the sampling roll
	default:
		return true
	}
	rate := s.cfg.SuccessReadAuditRate
	switch {
	case rate <= 0:
		return false
	case rate >= 1:
		return true
	default:
		return rand.Float64() < rate
	}
}

// servedRequest carries the per-request labels the proxy needs after the
// HTTP response is committed (metrics, logs, audit). Returned by serve()
// so ServeHTTP can do post-response bookkeeping without a long return
// tuple.
type servedRequest struct {
	operation string
	status    int
	result    string
	authMode  string

	// Audit context, populated when a credential or anonymous binding
	// resolved successfully. Fields stay empty when the request failed
	// before identification (auth failure, malformed request).
	akid      string
	backend   string
	bucket    string
	key       string
	source    string // "sqlite" | "kubernetes" | "anonymous"
	claimNS   string
	claimName string
}

func (s *Server) recordAudit(r *http.Request, reqID string, out servedRequest) {
	status := "ok"
	switch {
	case out.status >= 500:
		status = "error"
	case out.status >= 400:
		status = "denied"
	}
	detail := map[string]any{
		"auth_mode": out.authMode,
		"result":    out.result,
	}
	if out.akid != "" {
		detail["access_key"] = out.akid
	}
	if out.source != "" {
		detail["source"] = out.source
	}
	if out.claimNS != "" {
		detail["claim_ns"] = out.claimNS
		detail["claim_name"] = out.claimName
	}
	_ = s.cfg.Audit.Record(r.Context(), audit.Event{
		Timestamp: time.Now().UTC(),
		Action:    "s3.proxy." + strings.ToLower(out.operation),
		Backend:   out.backend,
		Bucket:    out.bucket,
		Key:       out.key,
		RequestID: reqID,
		IP:        ClientIP(r, s.cfg.TrustedProxies),
		UserAgent: r.UserAgent(),
		Status:    status,
		Detail:    detail,
	})
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request, reqID string) servedRequest {
	out := servedRequest{operation: "Unknown", authMode: "signed"}

	// Anonymous fast-path: only consulted when (a) the request carries no
	// SigV4 credentials at all, and (b) the cluster opt-in is on. Malformed
	// Authorization headers stay on the signed path so SignatureDoesNotMatch
	// surfaces normally.
	if s.cfg.AnonymousEnabled && IsRequestUnauthenticated(r) {
		anon := s.serveAnonymous(w, r, reqID)
		anon.authMode = "anonymous"
		return anon
	}

	if r.URL.Query().Get("X-Amz-Signature") != "" {
		out.authMode = "presigned"
	}

	res, err := s.verifier.Verify(r)
	if err != nil {
		out.status, out.result = s.writeAuthFailure(w, err, reqID)
		return out
	}

	// For aws-chunked inbound bodies, swap r.Body for a signature-verifying
	// reader. Outbound goes as UNSIGNED-PAYLOAD with Content-Length taken
	// from X-Amz-Decoded-Content-Length.
	if res.PayloadHash == sigv4verifier.StreamingPayload {
		decoded, err := strconv.ParseInt(r.Header.Get("X-Amz-Decoded-Content-Length"), 10, 64)
		if err != nil {
			writeS3Error(w, http.StatusBadRequest, "InvalidRequest", "missing or bad X-Amz-Decoded-Content-Length", r.URL.Path, reqID)
			out.status, out.result = http.StatusBadRequest, "bad-chunked-header"
			return out
		}
		r.Body = io.NopCloser(sigv4verifier.NewChunkedReader(r.Body, res.SigningKey, res.SeedSignature, res.Region, res.Service, res.Date))
		r.ContentLength = decoded
	}

	vc, ok := s.cfg.Source.Lookup(res.AccessKeyID)
	if !ok {
		s.cfg.Metrics.AuthFailures.WithLabelValues("unknown-akid").Inc()
		writeS3Error(w, http.StatusForbidden, "InvalidAccessKeyId", "access key not recognized", r.URL.Path, reqID)
		out.akid = res.AccessKeyID
		out.status, out.result = http.StatusForbidden, "unknown-akid"
		return out
	}
	out.akid = vc.AccessKeyID
	out.backend = vc.BackendName
	out.source = vc.Source
	out.claimNS = vc.ClaimNamespace
	out.claimName = vc.ClaimName

	if !s.cfg.Limiter.Allow(vc.AccessKeyID) {
		writeS3Error(w, http.StatusServiceUnavailable, "SlowDown", "rate limit exceeded", r.URL.Path, reqID)
		out.status, out.result = http.StatusServiceUnavailable, "rate-limited"
		return out
	}

	route := ClassifyRoute(r, s.cfg.HostSuffixes)
	out.operation = classifyOperation(r, route)
	out.bucket = route.Bucket
	out.key = route.Key

	if route.Bucket == "" {
		// Service-level: ListBuckets synthesized per-tenant. Legacy 1:1
		// credentials carry a single-element BucketScopes; N:1 grants carry N.
		names := make([]string, 0, len(vc.BucketScopes))
		for _, sc := range vc.BucketScopes {
			names = append(names, sc.BucketName)
		}
		WriteSynthesizedListBuckets(w, names, s.cfg.BucketCreated)
		out.operation, out.status, out.result = "ListBuckets", http.StatusOK, "ok"
		return out
	}

	if !EnforceScope(vc.BucketScopes, route.Bucket) {
		s.cfg.Metrics.ScopeViolations.Inc()
		writeS3Error(w, http.StatusForbidden, "AccessDenied", "credential is not scoped to this bucket", r.URL.Path, reqID)
		out.status, out.result = http.StatusForbidden, "scope-violation"
		return out
	}

	// Pre-write quota check for upload ops with a known Content-Length.
	// Streaming uploads (StreamingPayload) get the decoded length above.
	if s.cfg.Quotas != nil && isUploadOp(out.operation) && r.ContentLength > 0 {
		if err := s.cfg.Quotas.CheckUpload(r.Context(), vc.BackendName, route.Bucket, r.ContentLength); err != nil {
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

	outReq, err := s.buildOutbound(r, route, route.Bucket, backendURL, spec)
	if err != nil {
		writeS3Error(w, http.StatusBadRequest, "InvalidRequest", err.Error(), r.URL.Path, reqID)
		out.status, out.result = http.StatusBadRequest, "bad-request"
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
		writeS3Error(w, http.StatusBadGateway, "InternalError", err.Error(), r.URL.Path, reqID)
		out.status, out.result = http.StatusBadGateway, "upstream-error"
		return out
	}
	defer resp.Body.Close()

	s.streamResponse(w, resp, outReq, out.operation, reqID)

	// Post-write usage update on a successful upload. Recorded is in-memory
	// only so calling it here doesn't add a hot-path round-trip.
	if s.cfg.Quotas != nil && isUploadOp(out.operation) && r.ContentLength > 0 && resp.StatusCode < 400 {
		s.cfg.Quotas.Recorded(vc.BackendName, route.Bucket, r.ContentLength)
	}

	out.status, out.result = resp.StatusCode, "ok"
	return out
}

// streamResponse copies an upstream response back to the client, stripping
// hop-by-hop headers and capturing 5xx body snippets for logs. Shared by the
// signed and anonymous paths.
func (s *Server) streamResponse(w http.ResponseWriter, resp *http.Response, outReq *http.Request, op, reqID string) {
	for k, vs := range resp.Header {
		lk := strings.ToLower(k)
		if _, strip := stripInbound[lk]; strip {
			continue
		}
		if lk == "server" {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	var (
		n       int64
		copyErr error
	)
	if resp.StatusCode >= 500 {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		s.cfg.Log.Info("backend 5xx",
			"method", outReq.Method,
			"url", outReq.URL.String(),
			"status", resp.StatusCode,
			"body", string(body),
			"request_id", reqID,
		)
		var written int
		written, copyErr = w.Write(body)
		n = int64(written)
		if readErr == nil {
			extra, err := io.Copy(w, resp.Body)
			n += extra
			if err != nil {
				copyErr = err
			}
		}
	} else {
		n, copyErr = io.Copy(w, resp.Body)
	}
	s.cfg.Metrics.BytesOut.WithLabelValues(op).Add(float64(n))
	if copyErr != nil {
		s.cfg.Log.Info("stream response copy error", "err", copyErr.Error(), "request_id", reqID)
	}
}

// serveAnonymous handles requests that arrive without any SigV4 credentials.
// The cluster-level enable check has already been performed by the caller.
func (s *Server) serveAnonymous(w http.ResponseWriter, r *http.Request, reqID string) servedRequest {
	out := servedRequest{operation: "Unknown", source: "anonymous"}

	route := ClassifyRoute(r, s.cfg.HostSuffixes)
	out.operation = classifyOperation(r, route)
	out.bucket = route.Bucket
	out.key = route.Key

	if route.Bucket == "" {
		s.cfg.Metrics.AuthFailures.WithLabelValues("missing-auth").Inc()
		writeS3Error(w, http.StatusForbidden, "MissingAuthenticationToken", "anonymous service-level requests are not allowed", r.URL.Path, reqID)
		out.status, out.result = http.StatusForbidden, "missing-auth"
		return out
	}

	binding, ok := s.cfg.Source.LookupAnon(route.Bucket)
	if !ok {
		s.cfg.Metrics.AnonymousRejects.WithLabelValues("unconfigured-bucket").Inc()
		s.cfg.Metrics.AuthFailures.WithLabelValues("missing-auth").Inc()
		writeS3Error(w, http.StatusForbidden, "MissingAuthenticationToken", "request requires authentication", r.URL.Path, reqID)
		out.status, out.result = http.StatusForbidden, "missing-auth"
		return out
	}
	out.backend = binding.BackendName

	if !AnonymousOpAllowed(binding.Mode, out.operation, r.URL.Query()) {
		reason := "op-not-allowed"
		for _, k := range anonymousBlockedSubresources {
			if r.URL.Query().Has(k) {
				reason = "subresource-not-allowed"
				break
			}
		}
		s.cfg.Metrics.AnonymousRejects.WithLabelValues(reason).Inc()
		writeS3Error(w, http.StatusForbidden, "AccessDenied", "operation not permitted on anonymous bucket", r.URL.Path, reqID)
		out.status, out.result = http.StatusForbidden, reason
		return out
	}

	ip := ClientIP(r, s.cfg.TrustedProxies)
	if !s.cfg.IPLimiter.AllowAt(ip, binding.PerSourceIPRPS) {
		s.cfg.Metrics.AnonymousRejects.WithLabelValues("rate-limited").Inc()
		writeS3Error(w, http.StatusServiceUnavailable, "SlowDown", "rate limit exceeded", r.URL.Path, reqID)
		out.status, out.result = http.StatusServiceUnavailable, "rate-limited"
		return out
	}

	backendURL, spec, err := s.cfg.Backends.Backend(r.Context(), binding.BackendName)
	if err != nil {
		writeS3Error(w, http.StatusBadGateway, "InternalError", err.Error(), r.URL.Path, reqID)
		out.status, out.result = http.StatusBadGateway, "backend-unavailable"
		return out
	}

	outReq, err := s.buildOutbound(r, route, binding.BucketName, backendURL, spec)
	if err != nil {
		writeS3Error(w, http.StatusBadRequest, "InvalidRequest", err.Error(), r.URL.Path, reqID)
		out.status, out.result = http.StatusBadRequest, "bad-request"
		return out
	}

	if err := s.signOutbound(r.Context(), outReq, binding.BackendName, spec); err != nil {
		writeS3Error(w, http.StatusBadGateway, "InternalError", fmt.Sprintf("sign outbound: %v", err), r.URL.Path, reqID)
		out.status, out.result = http.StatusBadGateway, "sign-error"
		return out
	}

	upStart := time.Now()
	resp, err := s.transport.RoundTrip(outReq)
	s.cfg.Metrics.Upstream.WithLabelValues(out.operation).Observe(time.Since(upStart).Seconds())
	if err != nil {
		writeS3Error(w, http.StatusBadGateway, "InternalError", err.Error(), r.URL.Path, reqID)
		out.status, out.result = http.StatusBadGateway, "upstream-error"
		return out
	}
	defer resp.Body.Close()

	s.streamResponse(w, resp, outReq, out.operation, reqID)
	s.cfg.Metrics.AnonymousRequests.WithLabelValues(out.operation, strconv.Itoa(resp.StatusCode)).Inc()
	out.status, out.result = resp.StatusCode, "ok"
	return out
}

func (s *Server) writeAuthFailure(w http.ResponseWriter, err error, reqID string) (int, string) {
	var code, reason string
	switch {
	case errors.Is(err, sigv4verifier.ErrMissingAuth):
		code, reason = "MissingAuthenticationToken", "missing-auth"
	case errors.Is(err, sigv4verifier.ErrInvalidAccessKey):
		code, reason = "InvalidAccessKeyId", "unknown-akid"
	case errors.Is(err, sigv4verifier.ErrSignatureMismatch):
		code, reason = "SignatureDoesNotMatch", "bad-sig"
	case errors.Is(err, sigv4verifier.ErrRequestExpired):
		code, reason = "AccessDenied", "expired"
	case errors.Is(err, sigv4verifier.ErrRequestTimeSkewed):
		code, reason = "RequestTimeTooSkewed", "skew"
	default:
		code, reason = "AccessDenied", "auth-error"
	}
	s.cfg.Metrics.AuthFailures.WithLabelValues(reason).Inc()
	writeS3Error(w, http.StatusForbidden, code, err.Error(), "", reqID)
	return http.StatusForbidden, reason
}

func (s *Server) buildOutbound(r *http.Request, route RouteInfo, realBucket string, backendURL *url.URL, spec BackendSpec) (*http.Request, error) {
	// Always go path-style outbound to the backend, regardless of inbound style.
	path := BuildOutboundPath(realBucket, route.Key)
	rawPath := BuildOutboundRawPath(realBucket, route.Key)
	outURL := *backendURL
	basePath := strings.TrimRight(outURL.Path, "/")
	outURL.Path = basePath + path
	// Carry the AWS-canonical encoding in RawPath so reserved characters in
	// the key (`+`, `=`, spaces, etc.) are `%XX` on the wire — otherwise the
	// proxy-side signature (computed over the literal path) and the strict
	// backend-side canonical URI disagree and the backend 403s.
	outURL.RawPath = basePath + rawPath
	// Drop the inbound SigV4 presigned query params — we re-sign via the
	// Authorization header below, and leaving them in makes the backend treat
	// the outbound as a query-string-signed request against the VC's AKID.
	outURL.RawQuery = StripPresignedQuery(r.URL.RawQuery)

	out, err := http.NewRequestWithContext(r.Context(), r.Method, outURL.String(), r.Body)
	if err != nil {
		return nil, err
	}
	out.Header = PrepareOutboundHeaders(r.Header)
	out.Header.Set("Host", backendURL.Host)
	out.Host = backendURL.Host
	if r.ContentLength > 0 {
		out.ContentLength = r.ContentLength
	}
	_ = spec
	return out, nil
}

func (s *Server) signOutbound(ctx context.Context, req *http.Request, backendName string, spec BackendSpec) error {
	region := spec.Region
	if region == "" {
		region = "us-east-1"
	}
	creds, err := s.admCredsFor(ctx, backendName, spec)
	if err != nil {
		return err
	}
	// Some S3 backends reject requests that don't carry x-amz-content-sha256
	// in SignedHeaders (Swift-based backends are the canonical case).
	// aws-sdk-go-v2's signer is supposed to add the header itself but in
	// practice doesn't always put it in SignedHeaders, so we set it
	// explicitly first — the canonicalizer then picks it up unconditionally.
	req.Header.Set("X-Amz-Content-Sha256", sigv4verifier.UnsignedPayload)
	signer := v4.NewSigner(func(o *v4.SignerOptions) {
		o.DisableURIPathEscaping = true
	})
	return signer.SignHTTP(ctx, creds, req, sigv4verifier.UnsignedPayload, "s3", region, time.Now().UTC())
}

func (s *Server) admCredsFor(ctx context.Context, backendName string, spec BackendSpec) (aws.Credentials, error) {
	if s.admCredsForOverride != nil {
		return s.admCredsForOverride(ctx, spec)
	}
	admin, _, err := s.cfg.Backends.AdminCreds(ctx, backendName)
	if err != nil {
		return aws.Credentials{}, err
	}
	return admin, nil
}

func defaultRequestID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36)
}

// classifyOperation is a best-effort mapping from HTTP method+query to the
// S3 operation name for metric labeling. Cardinality stays bounded; unknown
// shapes fall through to "Unknown".
func classifyOperation(r *http.Request, route RouteInfo) string {
	q := r.URL.Query()
	switch r.Method {
	case http.MethodGet:
		switch {
		case route.Bucket == "":
			return "ListBuckets"
		case route.Key == "" && q.Has("location"):
			return "GetBucketLocation"
		case route.Key == "" && q.Has("uploads"):
			return "ListMultipartUploads"
		case route.Key == "":
			return "ListObjects"
		case q.Has("uploadId"):
			return "ListParts"
		default:
			return "GetObject"
		}
	case http.MethodPut:
		switch {
		case route.Key == "":
			return "PutBucket"
		case q.Has("partNumber") && q.Has("uploadId"):
			return "UploadPart"
		default:
			return "PutObject"
		}
	case http.MethodHead:
		if route.Key == "" {
			return "HeadBucket"
		}
		return "HeadObject"
	case http.MethodDelete:
		switch {
		case route.Key == "":
			return "DeleteBucket"
		case q.Has("uploadId"):
			return "AbortMultipart"
		default:
			return "DeleteObject"
		}
	case http.MethodPost:
		switch {
		case q.Has("uploads"):
			return "CreateMultipartUpload"
		case q.Has("uploadId"):
			return "CompleteMultipartUpload"
		case q.Has("delete"):
			return "DeleteObjects"
		}
	}
	return "Unknown"
}
