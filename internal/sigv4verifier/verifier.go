// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sigv4verifier

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MaxClockSkew is how far inbound X-Amz-Date / X-Amz-Credential dates are
// allowed to drift from server time.
const MaxClockSkew = 15 * time.Minute

// Classified verification errors. These map cleanly onto S3 error codes.
var (
	ErrMissingAuth       = errors.New("missing authentication token")
	ErrMalformedAuth     = errors.New("malformed authorization")
	ErrInvalidAccessKey  = errors.New("invalid access key id")
	ErrSignatureMismatch = errors.New("signature does not match")
	ErrRequestExpired    = errors.New("request expired")
	ErrRequestTimeSkewed = errors.New("request time skewed")
	ErrUnsupportedAlgo   = errors.New("unsupported signing algorithm")
	ErrInvalidCredential = errors.New("invalid credential scope")
)

// Resolver looks up the secret access key for a given access key id.
// Returning "" signals unknown credential.
type Resolver interface {
	Resolve(accessKeyID string) (secretAccessKey string, ok bool)
}

// ResolverFunc adapts a plain function to Resolver.
type ResolverFunc func(string) (string, bool)

func (f ResolverFunc) Resolve(a string) (string, bool) { return f(a) }

// Result is populated on successful verification.
type Result struct {
	AccessKeyID   string
	Region        string
	Service       string
	Date          time.Time
	SignedHeaders []string
	PayloadHash   string
	Presigned     bool
	Expires       time.Duration // only for presigned URLs

	// The following are populated only when PayloadHash == StreamingPayload.
	// The chunk reader needs them to verify subsequent chunk signatures.
	SigningKey    []byte
	SeedSignature string
}

// Verifier verifies inbound SigV4 signatures. Safe for concurrent use; the
// internal signing-key cache is a sync.Map keyed by (akid, date, region,
// service) so lookups are lock-free on the hot path.
type Verifier struct {
	Resolver Resolver

	// Now returns the current time; override in tests.
	Now func() time.Time

	// MaxSkew overrides MaxClockSkew.
	MaxSkew time.Duration

	// signingKeys caches HMAC chains by (akid, date, region, service).
	// Without it, every Verify() rebuilds kDate -> kRegion -> kService
	// -> kSigning -> final-HMAC, i.e. five HMAC-SHA256 ops per request.
	// With it, four of those five become a map hit on steady state.
	// Entries are bound to a fingerprint of the secret so a credential
	// rotated under the same access key id can't replay the cached key.
	signingKeys sync.Map // signingKeyKey -> signingKeyEntry
}

type signingKeyKey struct {
	akid, date, region, service string
}

type signingKeyEntry struct {
	// fp is FNV-64a of the resolved secret. Cheap to compute, sufficient
	// for an "is this still the same secret?" guard — collisions would
	// at worst reuse an old derived key, but the final HMAC over the
	// StringToSign would still mismatch and surface as a normal
	// SignatureDoesNotMatch.
	fp  uint64
	key []byte
}

// signingKey returns kSigning for the given credential scope, building it
// (and cacheing it) on miss. Cache invalidation happens implicitly: the
// date component of the key changes daily, so old day's entries fall out
// of the lookup path and never get hit again.
func (v *Verifier) signingKey(secret, akid, date, region, service string) []byte {
	k := signingKeyKey{akid: akid, date: date, region: region, service: service}
	fp := secretFingerprint(secret)
	if got, ok := v.signingKeys.Load(k); ok {
		e := got.(signingKeyEntry)
		if e.fp == fp {
			return e.key
		}
		// Same akid/date but a different secret means the credential was
		// rotated under us — fall through to re-derive and overwrite.
	}
	derived := deriveSigningKey(secret, date, region, service)
	v.signingKeys.Store(k, signingKeyEntry{fp: fp, key: derived})
	return derived
}

func secretFingerprint(secret string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(secret))
	return h.Sum64()
}

// InvalidateSigningKeys drops every cached signing key. Wired into the
// Source.Reload path so a deleted/disabled credential can never have its
// key reused after the source map has dropped it.
func (v *Verifier) InvalidateSigningKeys() {
	v.signingKeys.Range(func(k, _ any) bool {
		v.signingKeys.Delete(k)
		return true
	})
}

func (v *Verifier) now() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now().UTC()
}

func (v *Verifier) skew() time.Duration {
	if v.MaxSkew > 0 {
		return v.MaxSkew
	}
	return MaxClockSkew
}

// Verify verifies the SigV4 signature on r. The request body is NOT consumed.
func (v *Verifier) Verify(r *http.Request) (*Result, error) {
	if q := r.URL.Query(); q.Get("X-Amz-Signature") != "" {
		return v.verifyPresigned(r, q)
	}
	return v.verifyHeader(r)
}

// ---- Header variant -----------------------------------------------------

type headerAuth struct {
	Algorithm     string
	AccessKeyID   string
	Date          string
	Region        string
	Service       string
	SignedHeaders []string
	Signature     string
}

func parseHeaderAuth(h string) (*headerAuth, error) {
	const prefix = Algorithm + " "
	if !strings.HasPrefix(h, prefix) {
		return nil, ErrUnsupportedAlgo
	}
	h = h[len(prefix):]
	parts := strings.Split(h, ",")
	a := &headerAuth{Algorithm: Algorithm}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		eq := strings.IndexByte(p, '=')
		if eq < 0 {
			return nil, ErrMalformedAuth
		}
		k, val := p[:eq], p[eq+1:]
		switch k {
		case "Credential":
			cs := strings.Split(val, "/")
			if len(cs) != 5 || cs[4] != "aws4_request" {
				return nil, ErrInvalidCredential
			}
			a.AccessKeyID = cs[0]
			a.Date = cs[1]
			a.Region = cs[2]
			a.Service = cs[3]
		case "SignedHeaders":
			a.SignedHeaders = ExtractSignedHeaders(val)
		case "Signature":
			a.Signature = strings.ToLower(val)
		default:
			return nil, ErrMalformedAuth
		}
	}
	if a.AccessKeyID == "" || len(a.SignedHeaders) == 0 || a.Signature == "" {
		return nil, ErrMalformedAuth
	}
	return a, nil
}

func (v *Verifier) verifyHeader(r *http.Request) (*Result, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return nil, ErrMissingAuth
	}
	a, err := parseHeaderAuth(auth)
	if err != nil {
		return nil, err
	}

	dateStr := r.Header.Get("X-Amz-Date")
	if dateStr == "" {
		dateStr = r.Header.Get("Date")
	}
	reqTime, err := parseDate(dateStr)
	if err != nil {
		return nil, ErrMalformedAuth
	}
	if abs(v.now().Sub(reqTime)) > v.skew() {
		return nil, ErrRequestTimeSkewed
	}

	secret, ok := v.Resolver.Resolve(a.AccessKeyID)
	if !ok {
		return nil, ErrInvalidAccessKey
	}

	payloadHash := r.Header.Get("X-Amz-Content-Sha256")
	if payloadHash == "" {
		payloadHash = UnsignedPayload
	}

	// Ensure r.Header carries Host for canonicalization.
	headers := cloneHeaderWithHost(r)

	canonical := CanonicalRequest(r.Method, r.URL, headers, a.SignedHeaders, payloadHash)
	sts := stringToSign(reqTime, a.Region, a.Service, canonical)
	signingKey := v.signingKey(secret, a.AccessKeyID, a.Date, a.Region, a.Service)
	sig := hex.EncodeToString(hmacSHA256(signingKey, []byte(sts)))

	if subtle.ConstantTimeCompare([]byte(a.Signature), []byte(sig)) != 1 {
		return nil, ErrSignatureMismatch
	}

	res := &Result{
		AccessKeyID:   a.AccessKeyID,
		Region:        a.Region,
		Service:       a.Service,
		Date:          reqTime,
		SignedHeaders: a.SignedHeaders,
		PayloadHash:   payloadHash,
	}
	if payloadHash == StreamingPayload {
		res.SigningKey = signingKey
		res.SeedSignature = sig
	}
	return res, nil
}

// ---- Presigned variant --------------------------------------------------

func (v *Verifier) verifyPresigned(r *http.Request, q url.Values) (*Result, error) {
	if q.Get("X-Amz-Algorithm") != Algorithm {
		return nil, ErrUnsupportedAlgo
	}
	credential := q.Get("X-Amz-Credential")
	signedHeadersRaw := q.Get("X-Amz-SignedHeaders")
	dateStr := q.Get("X-Amz-Date")
	expiresStr := q.Get("X-Amz-Expires")
	presented := strings.ToLower(q.Get("X-Amz-Signature"))

	if credential == "" || signedHeadersRaw == "" || dateStr == "" || presented == "" {
		return nil, ErrMalformedAuth
	}

	cs := strings.Split(credential, "/")
	if len(cs) != 5 || cs[4] != "aws4_request" {
		return nil, ErrInvalidCredential
	}
	akid, date, region, service := cs[0], cs[1], cs[2], cs[3]

	reqTime, err := parseDate(dateStr)
	if err != nil {
		return nil, ErrMalformedAuth
	}
	if abs(v.now().Sub(reqTime)) > v.skew() {
		return nil, ErrRequestTimeSkewed
	}
	var expires time.Duration
	if expiresStr != "" {
		n, err := strconv.Atoi(expiresStr)
		if err != nil || n < 0 || n > 7*24*3600 {
			return nil, ErrMalformedAuth
		}
		expires = time.Duration(n) * time.Second
		if v.now().After(reqTime.Add(expires)) {
			return nil, ErrRequestExpired
		}
	}

	secret, ok := v.Resolver.Resolve(akid)
	if !ok {
		return nil, ErrInvalidAccessKey
	}

	// Rebuild query without X-Amz-Signature for canonicalization.
	q2 := make(url.Values, len(q))
	for k, vs := range q {
		if k == "X-Amz-Signature" {
			continue
		}
		q2[k] = vs
	}
	clone := *r.URL
	clone.RawQuery = q2.Encode()

	headers := cloneHeaderWithHost(r)
	signedHeaders := ExtractSignedHeaders(signedHeadersRaw)
	canonical := CanonicalRequest(r.Method, &clone, headers, signedHeaders, UnsignedPayload)
	sts := stringToSign(reqTime, region, service, canonical)
	signingKey := v.signingKey(secret, akid, date, region, service)
	sig := hex.EncodeToString(hmacSHA256(signingKey, []byte(sts)))

	if subtle.ConstantTimeCompare([]byte(presented), []byte(sig)) != 1 {
		return nil, ErrSignatureMismatch
	}

	return &Result{
		AccessKeyID:   akid,
		Region:        region,
		Service:       service,
		Date:          reqTime,
		SignedHeaders: signedHeaders,
		PayloadHash:   UnsignedPayload,
		Presigned:     true,
		Expires:       expires,
	}, nil
}

// ---- Helpers -------------------------------------------------------------

func cloneHeaderWithHost(r *http.Request) http.Header {
	h := make(http.Header, len(r.Header)+2)
	for k, vs := range r.Header {
		h[k] = append([]string(nil), vs...)
	}
	if h.Get("Host") == "" && r.Host != "" {
		h.Set("Host", r.Host)
	}
	// net/http (and httptest) do not populate r.Header["Content-Length"]; it
	// lives only on r.ContentLength. aws-sdk-go-v2's signer reads the value
	// the same way, so we mirror it here before canonicalizing.
	if h.Get("Content-Length") == "" && r.ContentLength > 0 {
		h.Set("Content-Length", strconv.FormatInt(r.ContentLength, 10))
	}
	return h
}

func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date")
	}
	// SigV4 uses ISO-8601 basic format.
	if t, err := time.Parse("20060102T150405Z", s); err == nil {
		return t.UTC(), nil
	}
	// Fall back to RFC 1123 for the Date header.
	if t, err := time.Parse(time.RFC1123, s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("bad date %q", s)
}

func stringToSign(reqTime time.Time, region, service, canonical string) string {
	hash := HashCanonical(canonical)
	date := reqTime.UTC().Format("20060102")
	scope := fmt.Sprintf("%s/%s/%s/aws4_request", date, region, service)
	return Algorithm + "\n" + reqTime.UTC().Format("20060102T150405Z") + "\n" + scope + "\n" + hash
}

func sign(secret, date, region, service, stringToSign string) string {
	k := deriveSigningKey(secret, date, region, service)
	return hex.EncodeToString(hmacSHA256(k, []byte(stringToSign)))
}

func deriveSigningKey(secret, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(date))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
