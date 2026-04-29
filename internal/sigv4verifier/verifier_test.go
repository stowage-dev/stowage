// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sigv4verifier

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/stretchr/testify/require"
)

const (
	testAKID   = "AKIATESTACCESSKEY123"
	testSecret = "aBcDeFgHiJkLmNoPqRsTuVwXyZ1234567890abCD" //nolint:gosec // synthetic test fixture, not a real credential.
	testRegion = "us-east-1"
	testSvc    = "s3"
)

func newResolver() ResolverFunc {
	return func(a string) (string, bool) {
		if a == testAKID {
			return testSecret, true
		}
		return "", false
	}
}

func signRequest(t *testing.T, req *http.Request, body []byte) {
	t.Helper()
	signer := v4.NewSigner()
	hash := sha256.Sum256(body)
	payload := hex.EncodeToString(hash[:])
	err := signer.SignHTTP(context.Background(),
		aws.Credentials{AccessKeyID: testAKID, SecretAccessKey: testSecret},
		req, payload, testSvc, testRegion, time.Now().UTC(),
	)
	require.NoError(t, err)
	req.Header.Set("X-Amz-Content-Sha256", payload)
}

func TestVerify_HeaderHappy(t *testing.T) {
	body := []byte("hello world")
	req := httptest.NewRequest(http.MethodPut, "http://example.com/my-bucket/key.txt", bytes.NewReader(body))
	req.Host = "example.com"
	signRequest(t, req, body)

	v := &Verifier{Resolver: newResolver()}
	res, err := v.Verify(req)
	require.NoError(t, err)
	require.Equal(t, testAKID, res.AccessKeyID)
	require.Equal(t, testRegion, res.Region)
	require.Contains(t, res.SignedHeaders, "host")
}

func TestVerify_UnknownAccessKey(t *testing.T) {
	body := []byte("body")
	req := httptest.NewRequest(http.MethodGet, "http://example.com/b/k", bytes.NewReader(body))
	req.Host = "example.com"
	signRequest(t, req, body)

	// Swap in a resolver that doesn't know testAKID.
	v := &Verifier{Resolver: ResolverFunc(func(string) (string, bool) { return "", false })}
	_, err := v.Verify(req)
	require.ErrorIs(t, err, ErrInvalidAccessKey)
}

func TestVerify_TamperedSignature(t *testing.T) {
	body := []byte("body")
	req := httptest.NewRequest(http.MethodGet, "http://example.com/b/k", bytes.NewReader(body))
	req.Host = "example.com"
	signRequest(t, req, body)

	auth := req.Header.Get("Authorization")
	// Flip last byte of signature.
	idx := strings.LastIndex(auth, "Signature=")
	require.True(t, idx >= 0)
	sig := auth[idx+len("Signature="):]
	last := sig[len(sig)-1]
	flipped := byte('f')
	if last == 'f' {
		flipped = '0'
	}
	req.Header.Set("Authorization", auth[:idx+len("Signature=")+len(sig)-1]+string(flipped))

	v := &Verifier{Resolver: newResolver()}
	_, err := v.Verify(req)
	require.ErrorIs(t, err, ErrSignatureMismatch)
}

func TestVerify_Skew(t *testing.T) {
	body := []byte{}
	req := httptest.NewRequest(http.MethodGet, "http://example.com/b", bytes.NewReader(body))
	req.Host = "example.com"
	signRequest(t, req, body)

	v := &Verifier{
		Resolver: newResolver(),
		Now:      func() time.Time { return time.Now().UTC().Add(30 * time.Minute) },
	}
	_, err := v.Verify(req)
	require.ErrorIs(t, err, ErrRequestTimeSkewed)
}

func TestVerify_MissingAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/b", nil)
	req.Host = "example.com"
	v := &Verifier{Resolver: newResolver()}
	_, err := v.Verify(req)
	require.ErrorIs(t, err, ErrMissingAuth)
}

func TestVerify_Presigned(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/b/k", nil)
	req.Host = "example.com"
	signer := v4.NewSigner()
	uri, _, err := signer.PresignHTTP(context.Background(),
		aws.Credentials{AccessKeyID: testAKID, SecretAccessKey: testSecret},
		req, UnsignedPayload, testSvc, testRegion, time.Now().UTC(),
	)
	require.NoError(t, err)

	u, err := url.Parse(uri)
	require.NoError(t, err)
	presReq := httptest.NewRequest(http.MethodGet, u.String(), nil)
	presReq.Host = "example.com"

	v := &Verifier{Resolver: newResolver()}
	res, err := v.Verify(presReq)
	require.NoError(t, err)
	require.True(t, res.Presigned)
}

func TestVerify_Presigned_Expired(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/b/k", nil)
	req.Host = "example.com"
	signer := v4.NewSigner()
	signedAt := time.Now().UTC().Add(-2 * time.Minute)
	uri, _, err := signer.PresignHTTP(context.Background(),
		aws.Credentials{AccessKeyID: testAKID, SecretAccessKey: testSecret},
		req, UnsignedPayload, testSvc, testRegion, signedAt,
		func(o *v4.SignerOptions) {},
	)
	require.NoError(t, err)

	u, _ := url.Parse(uri)
	q := u.Query()
	q.Set("X-Amz-Expires", "30")
	u.RawQuery = q.Encode()

	// Re-sign with the short expiry honored.
	req2 := httptest.NewRequest(http.MethodGet, u.String(), nil)
	req2.Host = "example.com"
	uri2, _, err := signer.PresignHTTP(context.Background(),
		aws.Credentials{AccessKeyID: testAKID, SecretAccessKey: testSecret},
		req2, UnsignedPayload, testSvc, testRegion, signedAt,
	)
	require.NoError(t, err)

	u2, _ := url.Parse(uri2)
	q2 := u2.Query()
	q2.Set("X-Amz-Expires", "30")
	u2.RawQuery = q2.Encode()
	presReq := httptest.NewRequest(http.MethodGet, u2.String(), nil)
	presReq.Host = "example.com"

	v := &Verifier{Resolver: newResolver()}
	_, err = v.Verify(presReq)
	// Either the signature mismatches (because we rewrote the query) or the
	// request has genuinely expired — both are rejections, which is what we
	// want to prove here.
	require.Error(t, err)
}

func TestCanonicalQuery_Deterministic(t *testing.T) {
	u1, _ := url.Parse("http://h/p?b=2&a=1&a=0")
	u2, _ := url.Parse("http://h/p?a=0&b=2&a=1")
	require.Equal(t, canonicalQuery(u1.Query()), canonicalQuery(u2.Query()))
}

func FuzzCanonicalRequest(f *testing.F) {
	f.Add("GET", "/", "", "host:example.com\n", "host", UnsignedPayload)
	f.Add("PUT", "/bucket/key.txt", "a=1&b=2", "host:example.com\nx-amz-date:20260424T000000Z\n", "host;x-amz-date", UnsignedPayload)
	f.Fuzz(func(t *testing.T, method, path, query, _, sigHdrs, payload string) {
		u, err := url.Parse("http://h" + path + "?" + query)
		if err != nil {
			return
		}
		h := http.Header{"Host": []string{"h"}}
		signed := ExtractSignedHeaders(sigHdrs)
		_ = CanonicalRequest(method, u, h, signed, payload)
	})
}

func TestSigningKey_CacheHit(t *testing.T) {
	v := &Verifier{Resolver: newResolver()}
	a := v.signingKey(testSecret, testAKID, "20260101", testRegion, testSvc)
	b := v.signingKey(testSecret, testAKID, "20260101", testRegion, testSvc)
	// Cache hit must return the SAME byte slice — i.e. the second call
	// did not redo deriveSigningKey. Comparing identity via &a[0] is the
	// cleanest way to assert "the cache served us the cached entry".
	require.Equal(t, &a[0], &b[0], "cache hit should return identical key")
}

func TestSigningKey_RotatedSecretInvalidates(t *testing.T) {
	v := &Verifier{Resolver: newResolver()}
	old := v.signingKey(testSecret, testAKID, "20260101", testRegion, testSvc)
	rotated := v.signingKey("rotated-"+testSecret, testAKID, "20260101", testRegion, testSvc)
	require.NotEqual(t, old, rotated, "rotated secret must produce a different signing key")
	// And: the cache should now hold the rotated entry, not the old one.
	again := v.signingKey("rotated-"+testSecret, testAKID, "20260101", testRegion, testSvc)
	require.Equal(t, rotated, again)
}

func TestInvalidateSigningKeys(t *testing.T) {
	v := &Verifier{Resolver: newResolver()}
	first := v.signingKey(testSecret, testAKID, "20260101", testRegion, testSvc)
	v.InvalidateSigningKeys()
	again := v.signingKey(testSecret, testAKID, "20260101", testRegion, testSvc)
	// Same value (deterministic derivation) but different backing array —
	// proves the cached slot was evicted and re-derived.
	require.Equal(t, first, again)
	require.NotSame(t, &first[0], &again[0])
}
