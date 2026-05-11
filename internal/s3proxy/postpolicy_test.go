// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// b64 encodes a JSON literal as the form's `policy` field would carry it.
func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestParsePostPolicy_Happy(t *testing.T) {
	doc := `{
		"expiration":"2030-01-01T00:00:00.000Z",
		"conditions":[
			{"bucket":"my-bucket"},
			["starts-with","$key","users/eric/"],
			{"acl":"public-read"},
			["eq","$Content-Type","image/png"],
			["starts-with","$x-amz-meta-tag",""],
			["content-length-range",1,1048576]
		]
	}`
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	p, err := ParsePostPolicy(b64(doc), now)
	require.NoError(t, err)

	require.Equal(t, "my-bucket", p.Equals["bucket"])
	require.Equal(t, "public-read", p.Equals["acl"])
	require.Equal(t, "image/png", p.Equals["content-type"], "field name should be lowercased")
	require.Equal(t, "users/eric/", p.StartsWith["key"])
	require.Equal(t, "", p.StartsWith["x-amz-meta-tag"])
	require.True(t, p.HasContentLengthRange())
	require.Equal(t, int64(1), p.ContentLengthMin)
	require.Equal(t, int64(1048576), p.ContentLengthMax)
}

func TestParsePostPolicy_Expired(t *testing.T) {
	doc := `{"expiration":"2020-01-01T00:00:00Z","conditions":[]}`
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	_, err := ParsePostPolicy(b64(doc), now)
	require.ErrorIs(t, err, ErrPolicyExpired)
}

func TestParsePostPolicy_BadBase64(t *testing.T) {
	_, err := ParsePostPolicy("!!!not base64!!!", time.Now())
	require.ErrorIs(t, err, ErrPolicyDecode)
}

func TestParsePostPolicy_URLSafeBase64(t *testing.T) {
	// Documents containing `+` or `/` in standard base64 sometimes arrive
	// URL-encoded. We accept either alphabet — exercise the fallback with
	// an input whose std encoding differs from its URL encoding.
	var policyJSON string
	for i := 0; i < 64; i++ {
		// `?` (0x3f) packs bits that, at varying alignments, produce `+`
		// or `/` in standard base64. We iterate until one alignment hits.
		candidate := `{"conditions":[{"key":"` + strings.Repeat("?", i) + `"}]}`
		std := base64.StdEncoding.EncodeToString([]byte(candidate))
		if strings.ContainsAny(std, "+/") {
			policyJSON = candidate
			break
		}
	}
	require.NotEmpty(t, policyJSON, "couldn't construct a policy whose b64 contains + or /")

	urlEncoded := base64.URLEncoding.EncodeToString([]byte(policyJSON))
	require.NotEqual(t, base64.StdEncoding.EncodeToString([]byte(policyJSON)), urlEncoded,
		"std and URL b64 must differ for this test to be meaningful")

	p, err := ParsePostPolicy(urlEncoded, time.Now())
	require.NoError(t, err)
	require.Contains(t, p.Equals, "key")
}

func TestParsePostPolicy_BadJSON(t *testing.T) {
	_, err := ParsePostPolicy(b64(`{"conditions": not-json`), time.Now())
	require.ErrorIs(t, err, ErrPolicyJSON)
}

func TestParsePostPolicy_OversizeRejected(t *testing.T) {
	// Build a policy that decodes to > MaxPolicyBytes.
	big := strings.Repeat("a", MaxPolicyBytes+1)
	doc := `{"conditions":[{"bucket":"` + big + `"}]}`
	_, err := ParsePostPolicy(b64(doc), time.Now())
	require.ErrorIs(t, err, ErrPolicyJSON)
}

func TestParsePostPolicy_MalformedConditions(t *testing.T) {
	cases := []string{
		`{"conditions":[["unknown-op","$key","x"]]}`,
		`{"conditions":[["starts-with","$key"]]}`, // wrong arity
		`{"conditions":[{"a":"b","c":"d"}]}`,      // two-key object
		`{"conditions":[42]}`,                      // bare number
		`{"conditions":[["content-length-range",-1,10]]}`,
		`{"conditions":[["content-length-range",10,1]]}`, // max < min
		`{"conditions":[["eq","$key",42]]}`,              // non-string value
	}
	for _, doc := range cases {
		t.Run(doc, func(t *testing.T) {
			_, err := ParsePostPolicy(b64(doc), time.Now())
			require.ErrorIs(t, err, ErrPolicyConditions)
		})
	}
}

func TestParsePostPolicy_NoExpiration(t *testing.T) {
	// Missing expiration should not error — caller may apply its own
	// freshness check based on x-amz-date in the form.
	doc := `{"conditions":[{"bucket":"b"}]}`
	p, err := ParsePostPolicy(b64(doc), time.Now())
	require.NoError(t, err)
	require.True(t, p.Expiration.IsZero())
}

func TestEnforcePolicy_Happy(t *testing.T) {
	policy := &PostPolicy{
		Equals: map[string]string{
			"bucket":       "my-bucket",
			"content-type": "image/png",
		},
		StartsWith: map[string]string{
			"key":            "users/eric/",
			"x-amz-meta-tag": "",
		},
		ContentLengthMin: -1,
		ContentLengthMax: -1,
	}
	fields := map[string]string{
		"bucket":           "my-bucket",
		"content-type":     "image/png",
		"key":              "users/eric/avatar.png",
		"x-amz-meta-tag":   "anything",
		"x-amz-credential": "AKID/20260101/us-east-1/s3/aws4_request", // signing field, ignored
		"x-amz-signature":  "deadbeef",
		"policy":           "anything",
		"file":             "binary",
	}
	require.NoError(t, EnforcePolicy(policy, fields))
}

func TestEnforcePolicy_EqualsMismatch(t *testing.T) {
	policy := &PostPolicy{
		Equals: map[string]string{"bucket": "expected"},
	}
	fields := map[string]string{"bucket": "wrong"}
	err := EnforcePolicy(policy, fields)
	var v *PolicyViolation
	require.ErrorAs(t, err, &v)
	require.Equal(t, "bucket", v.Field)
}

func TestEnforcePolicy_StartsWithMismatch(t *testing.T) {
	policy := &PostPolicy{
		StartsWith: map[string]string{"key": "users/eric/"},
	}
	fields := map[string]string{"key": "users/alice/avatar.png"}
	err := EnforcePolicy(policy, fields)
	var v *PolicyViolation
	require.ErrorAs(t, err, &v)
	require.Equal(t, "key", v.Field)
}

func TestEnforcePolicy_StartsWithEmptyMatchesAny(t *testing.T) {
	policy := &PostPolicy{
		StartsWith: map[string]string{"x-amz-meta-source": ""},
	}
	fields := map[string]string{"x-amz-meta-source": "arbitrary-value-from-client"}
	require.NoError(t, EnforcePolicy(policy, fields))
}

func TestEnforcePolicy_RejectsUnconditionedField(t *testing.T) {
	// A form field not in signingFormFields and not matched by any
	// condition is rejected. AWS does the same.
	policy := &PostPolicy{
		Equals: map[string]string{"bucket": "b"},
	}
	fields := map[string]string{
		"bucket":         "b",
		"x-amz-meta-pii": "snuck-in",
	}
	err := EnforcePolicy(policy, fields)
	var v *PolicyViolation
	require.ErrorAs(t, err, &v)
	require.Equal(t, "x-amz-meta-pii", v.Field)
}

func TestEnforcePolicy_MissingRequiredField(t *testing.T) {
	// A condition with no corresponding form field is a violation.
	policy := &PostPolicy{
		Equals: map[string]string{"bucket": "b"},
	}
	fields := map[string]string{} // empty
	err := EnforcePolicy(policy, fields)
	var v *PolicyViolation
	require.ErrorAs(t, err, &v)
	require.Equal(t, "bucket", v.Field)
}

func TestEnforcePolicy_SigningFieldsBypassConditionCheck(t *testing.T) {
	// The auth/signing fields don't need conditions matching them.
	policy := &PostPolicy{
		Equals: map[string]string{"bucket": "b"},
	}
	fields := map[string]string{
		"bucket":               "b",
		"policy":               "ignored",
		"x-amz-signature":      "ignored",
		"x-amz-algorithm":      "ignored",
		"x-amz-credential":     "ignored",
		"x-amz-date":           "ignored",
		"x-amz-security-token": "ignored",
		"file":                 "ignored",
	}
	require.NoError(t, EnforcePolicy(policy, fields))
}

func TestPolicyViolation_Error(t *testing.T) {
	v := &PolicyViolation{Field: "key", Reason: "value \"x\" does not start with \"y\""}
	require.Contains(t, v.Error(), "key")
	require.Contains(t, v.Error(), "does not start with")
}

func TestPolicyViolation_Wrap(t *testing.T) {
	// Confirm callers can fish out the violation via errors.As even if
	// it's been wrapped further up the stack.
	inner := &PolicyViolation{Field: "bucket", Reason: "mismatch"}
	wrapped := errors.Join(errors.New("upload rejected"), inner)
	var v *PolicyViolation
	require.ErrorAs(t, wrapped, &v)
	require.Equal(t, "bucket", v.Field)
}

func TestPostPolicy_HasContentLengthRange(t *testing.T) {
	p := &PostPolicy{ContentLengthMin: -1, ContentLengthMax: -1}
	require.False(t, p.HasContentLengthRange())
	p.ContentLengthMin = 0
	p.ContentLengthMax = 100
	require.True(t, p.HasContentLengthRange())
}
