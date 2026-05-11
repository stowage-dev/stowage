// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sigv4verifier

import (
	"crypto/subtle"
	"encoding/hex"
	"strings"
	"time"
)

// PolicyAuth is the subset of a multipart/form-data S3 POST-Object request
// that carries authentication. Field names map 1:1 to the form fields
// documented at
// https://docs.aws.amazon.com/AmazonS3/latest/API/sigv4-authentication-HTTPPOST.html
//
// All values are taken verbatim from the form; case-folding and trimming are
// the caller's responsibility.
type PolicyAuth struct {
	Algorithm  string // x-amz-algorithm, must equal Algorithm
	Credential string // x-amz-credential: <akid>/<yyyymmdd>/<region>/<service>/aws4_request
	Date       string // x-amz-date: 20060102T150405Z
	Signature  string // x-amz-signature: hex(HMAC-SHA256(signingKey, policyBase64))
	Policy     string // base64-encoded policy document, verbatim as it appears in the form
}

// PolicyResult is populated on successful verification of a POST-policy form.
// It does not include the policy document — VerifyPolicy is signature-only,
// and the caller decodes/enforces the policy separately.
type PolicyResult struct {
	AccessKeyID string
	Region      string
	Service     string
	Date        time.Time
}

// VerifyPolicy validates the signature carried by a POST-Object multipart
// form against the resolver-provided secret. It checks the algorithm,
// credential scope shape, and time skew of the form's x-amz-date, then
// recomputes the signature as hex(HMAC-SHA256(signingKey, policyBase64))
// and compares constant-time against the presented value.
//
// VerifyPolicy does NOT parse or enforce the policy document. The signature
// covers the base64 bytes literally, so the caller must:
//
//  1. Pass Policy exactly as it appeared in the form field — no trimming or
//     re-encoding.
//  2. After this call returns nil, base64-decode and walk the policy's
//     conditions against the rest of the form fields.
//
// The signing-key cache is shared with the SigV4 header/presigned paths so
// a credential's derived key is computed at most once per UTC day across
// all auth variants.
func (v *Verifier) VerifyPolicy(auth PolicyAuth) (*PolicyResult, error) {
	if auth.Algorithm != Algorithm {
		return nil, ErrUnsupportedAlgo
	}
	if auth.Credential == "" || auth.Date == "" || auth.Signature == "" || auth.Policy == "" {
		return nil, ErrMalformedAuth
	}

	cs := strings.Split(auth.Credential, "/")
	if len(cs) != 5 || cs[4] != "aws4_request" {
		return nil, ErrInvalidCredential
	}
	akid, date, region, service := cs[0], cs[1], cs[2], cs[3]

	reqTime, err := parseDate(auth.Date)
	if err != nil {
		return nil, ErrMalformedAuth
	}
	if abs(v.now().Sub(reqTime)) > v.skew() {
		return nil, ErrRequestTimeSkewed
	}

	// The credential's date component must match the x-amz-date day. AWS
	// derives the signing key from the credential's date, so a mismatch
	// would only ever produce signatures the client couldn't have computed.
	if reqTime.UTC().Format("20060102") != date {
		return nil, ErrInvalidCredential
	}

	secret, ok := v.Resolver.Resolve(akid)
	if !ok {
		return nil, ErrInvalidAccessKey
	}

	signingKey := v.signingKey(secret, akid, date, region, service)
	expected := hex.EncodeToString(hmacSHA256(signingKey, []byte(auth.Policy)))
	presented := strings.ToLower(auth.Signature)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(presented)) != 1 {
		return nil, ErrSignatureMismatch
	}

	return &PolicyResult{
		AccessKeyID: akid,
		Region:      region,
		Service:     service,
		Date:        reqTime,
	}, nil
}
