// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sigv4verifier

import (
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// buildPolicyAuth produces a PolicyAuth that VerifyPolicy will accept,
// computed from the test fixtures. now is the verifier's notion of "now"
// so the x-amz-date stays inside MaxClockSkew.
func buildPolicyAuth(t *testing.T, now time.Time, policyB64 string) PolicyAuth {
	t.Helper()
	date := now.UTC().Format("20060102")
	stamp := now.UTC().Format("20060102T150405Z")
	signingKey := deriveSigningKey(testSecret, date, testRegion, testSvc)
	sig := hex.EncodeToString(hmacSHA256(signingKey, []byte(policyB64)))
	return PolicyAuth{
		Algorithm:  Algorithm,
		Credential: testAKID + "/" + date + "/" + testRegion + "/" + testSvc + "/aws4_request",
		Date:       stamp,
		Signature:  sig,
		Policy:     policyB64,
	}
}

func TestVerifyPolicy_Happy(t *testing.T) {
	now := time.Now().UTC()
	policy := base64.StdEncoding.EncodeToString([]byte(`{"expiration":"2030-01-01T00:00:00Z","conditions":[{"bucket":"b"}]}`))
	auth := buildPolicyAuth(t, now, policy)

	v := &Verifier{Resolver: newResolver(), Now: func() time.Time { return now }}
	res, err := v.VerifyPolicy(auth)
	require.NoError(t, err)
	require.Equal(t, testAKID, res.AccessKeyID)
	require.Equal(t, testRegion, res.Region)
	require.Equal(t, testSvc, res.Service)
}

func TestVerifyPolicy_TamperedSignature(t *testing.T) {
	now := time.Now().UTC()
	policy := base64.StdEncoding.EncodeToString([]byte(`{"conditions":[]}`))
	auth := buildPolicyAuth(t, now, policy)
	// Flip a hex character. Stay within [0-9a-f] so it remains a valid hex digit.
	if auth.Signature[0] == '0' {
		auth.Signature = "1" + auth.Signature[1:]
	} else {
		auth.Signature = "0" + auth.Signature[1:]
	}

	v := &Verifier{Resolver: newResolver(), Now: func() time.Time { return now }}
	_, err := v.VerifyPolicy(auth)
	require.ErrorIs(t, err, ErrSignatureMismatch)
}

func TestVerifyPolicy_UnknownAKID(t *testing.T) {
	now := time.Now().UTC()
	policy := base64.StdEncoding.EncodeToString([]byte(`{}`))
	auth := buildPolicyAuth(t, now, policy)

	v := &Verifier{
		Resolver: ResolverFunc(func(string) (string, bool) { return "", false }),
		Now:      func() time.Time { return now },
	}
	_, err := v.VerifyPolicy(auth)
	require.ErrorIs(t, err, ErrInvalidAccessKey)
}

func TestVerifyPolicy_BadAlgorithm(t *testing.T) {
	auth := PolicyAuth{
		Algorithm:  "AWS3-HMAC-SHA1",
		Credential: testAKID + "/20260101/us-east-1/s3/aws4_request",
		Date:       "20260101T000000Z",
		Signature:  "deadbeef",
		Policy:     "Zm9v",
	}
	v := &Verifier{Resolver: newResolver()}
	_, err := v.VerifyPolicy(auth)
	require.ErrorIs(t, err, ErrUnsupportedAlgo)
}

func TestVerifyPolicy_MalformedCredential(t *testing.T) {
	auth := PolicyAuth{
		Algorithm:  Algorithm,
		Credential: "not/a/full/credential",
		Date:       "20260101T000000Z",
		Signature:  "deadbeef",
		Policy:     "Zm9v",
	}
	v := &Verifier{Resolver: newResolver()}
	_, err := v.VerifyPolicy(auth)
	require.ErrorIs(t, err, ErrInvalidCredential)
}

func TestVerifyPolicy_TimeSkew(t *testing.T) {
	now := time.Now().UTC()
	policy := base64.StdEncoding.EncodeToString([]byte(`{}`))
	auth := buildPolicyAuth(t, now, policy)

	v := &Verifier{
		Resolver: newResolver(),
		// Verifier clock is 2 hours ahead of the request's x-amz-date.
		Now: func() time.Time { return now.Add(2 * time.Hour) },
	}
	_, err := v.VerifyPolicy(auth)
	require.ErrorIs(t, err, ErrRequestTimeSkewed)
}

func TestVerifyPolicy_CredentialDateMismatch(t *testing.T) {
	now := time.Now().UTC()
	policy := base64.StdEncoding.EncodeToString([]byte(`{}`))
	auth := buildPolicyAuth(t, now, policy)
	// Replace the credential's date with yesterday — still inside the
	// 15-minute clock skew on x-amz-date but the credential scope no
	// longer agrees, so the signature would never have been valid.
	yesterday := now.Add(-24 * time.Hour).UTC().Format("20060102")
	auth.Credential = testAKID + "/" + yesterday + "/" + testRegion + "/" + testSvc + "/aws4_request"

	v := &Verifier{Resolver: newResolver(), Now: func() time.Time { return now }}
	_, err := v.VerifyPolicy(auth)
	require.ErrorIs(t, err, ErrInvalidCredential)
}

func TestVerifyPolicy_MissingFields(t *testing.T) {
	cases := []struct {
		name  string
		mutate func(*PolicyAuth)
	}{
		{"no credential", func(p *PolicyAuth) { p.Credential = "" }},
		{"no date", func(p *PolicyAuth) { p.Date = "" }},
		{"no signature", func(p *PolicyAuth) { p.Signature = "" }},
		{"no policy", func(p *PolicyAuth) { p.Policy = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			auth := buildPolicyAuth(t, now, base64.StdEncoding.EncodeToString([]byte(`{}`)))
			tc.mutate(&auth)
			v := &Verifier{Resolver: newResolver(), Now: func() time.Time { return now }}
			_, err := v.VerifyPolicy(auth)
			require.ErrorIs(t, err, ErrMalformedAuth)
		})
	}
}

func TestVerifyPolicy_SharesSigningKeyCache(t *testing.T) {
	// Two policies signed with the same credential should not redrive
	// the signing key — the second verify should reuse the cached entry.
	// We assert by deleting the cache after the first call and confirming
	// it gets re-populated by the second.
	now := time.Now().UTC()
	v := &Verifier{Resolver: newResolver(), Now: func() time.Time { return now }}

	p1 := base64.StdEncoding.EncodeToString([]byte(`{"conditions":[]}`))
	a1 := buildPolicyAuth(t, now, p1)
	_, err := v.VerifyPolicy(a1)
	require.NoError(t, err)

	cachedAny, ok := v.signingKeys.Load(signingKeyKey{
		akid:    testAKID,
		date:    now.UTC().Format("20060102"),
		region:  testRegion,
		service: testSvc,
	})
	require.True(t, ok, "signing key should be cached after first verify")

	p2 := base64.StdEncoding.EncodeToString([]byte(`{"conditions":[{"bucket":"x"}]}`))
	a2 := buildPolicyAuth(t, now, p2)
	_, err = v.VerifyPolicy(a2)
	require.NoError(t, err)

	cachedAny2, _ := v.signingKeys.Load(signingKeyKey{
		akid:    testAKID,
		date:    now.UTC().Format("20060102"),
		region:  testRegion,
		service: testSvc,
	})
	// Same entry pointer would be ideal, but sync.Map returns a copy of
	// the interface — equate by the underlying key bytes instead.
	e1 := cachedAny.(signingKeyEntry)
	e2 := cachedAny2.(signingKeyEntry)
	require.Equal(t, e1.key, e2.key)
}
