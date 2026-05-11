// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// PostPolicy is the decoded form of an S3 browser-form POST policy document.
// Conditions are stored in two parallel maps so the matcher can answer
// "does form field X satisfy a condition?" without re-walking the
// heterogeneous JSON array on every lookup.
//
// Reference:
// https://docs.aws.amazon.com/AmazonS3/latest/API/sigv4-create-signed-request.html
// (browser-based uploads — POST policy)
type PostPolicy struct {
	Expiration time.Time

	// Equals maps lowercased field name -> required exact-match value.
	// Built from `{"field":"value"}` object conditions and `["eq","$field","value"]`
	// array conditions.
	Equals map[string]string

	// StartsWith maps lowercased field name -> required prefix.
	// Built from `["starts-with","$field","prefix"]` array conditions.
	// An empty prefix matches any value (per AWS docs).
	StartsWith map[string]string

	// ContentLengthMin/Max bound the body size in bytes. Both -1 when no
	// content-length-range condition is present.
	ContentLengthMin int64
	ContentLengthMax int64
}

// HasContentLengthRange reports whether the policy carried a
// content-length-range condition. When false, the caller should treat the
// upload as unbounded and apply its own ceiling.
func (p *PostPolicy) HasContentLengthRange() bool {
	return p.ContentLengthMin >= 0 && p.ContentLengthMax >= 0
}

// Errors returned by ParsePostPolicy. Wrapping in errors.Is is supported.
var (
	ErrPolicyDecode     = errors.New("post policy: base64 decode failed")
	ErrPolicyJSON       = errors.New("post policy: malformed json")
	ErrPolicyExpired    = errors.New("post policy: expired")
	ErrPolicyConditions = errors.New("post policy: malformed condition")
)

// MaxPolicyBytes caps the decoded JSON length to bound parser work. AWS's
// own documented limit on the policy document is much smaller; this is a
// defensive ceiling.
const MaxPolicyBytes = 64 * 1024

// ParsePostPolicy decodes the base64 policy field of a POST Object form
// and extracts its conditions into a structured form.
//
// The input string is the form field as it appears on the wire. The
// signature has already been verified against the same bytes — this
// function does not need to be constant-time.
//
// now is used to evaluate the policy's `expiration` field; pass the same
// clock the verifier uses (typically time.Now().UTC()).
func ParsePostPolicy(policyB64 string, now time.Time) (*PostPolicy, error) {
	raw, err := base64.StdEncoding.DecodeString(policyB64)
	if err != nil {
		// AWS SDKs sometimes URL-encode `+` and `/`. Try the URL alphabet
		// before giving up — failing this on a recoverable encoding would
		// make stowage incompatible with otherwise-conformant clients.
		raw, err = base64.URLEncoding.DecodeString(policyB64)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrPolicyDecode, err)
		}
	}
	if len(raw) > MaxPolicyBytes {
		return nil, fmt.Errorf("%w: %d bytes exceeds %d", ErrPolicyJSON, len(raw), MaxPolicyBytes)
	}

	var doc struct {
		Expiration string            `json:"expiration"`
		Conditions []json.RawMessage `json:"conditions"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPolicyJSON, err)
	}

	policy := &PostPolicy{
		Equals:           make(map[string]string),
		StartsWith:       make(map[string]string),
		ContentLengthMin: -1,
		ContentLengthMax: -1,
	}

	if doc.Expiration != "" {
		// AWS uses ISO 8601; the documented format is "2007-12-01T12:00:00.000Z".
		exp, err := parsePolicyExpiration(doc.Expiration)
		if err != nil {
			return nil, fmt.Errorf("%w: bad expiration %q", ErrPolicyConditions, doc.Expiration)
		}
		policy.Expiration = exp
		if now.After(exp) {
			return nil, ErrPolicyExpired
		}
	}

	for i, c := range doc.Conditions {
		if err := mergeCondition(policy, c); err != nil {
			return nil, fmt.Errorf("%w (index %d): %v", ErrPolicyConditions, i, err)
		}
	}

	return policy, nil
}

func parsePolicyExpiration(s string) (time.Time, error) {
	// Try the common ISO 8601 layouts AWS clients emit, most-specific first.
	layouts := []string{
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		time.RFC3339Nano,
		time.RFC3339,
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable")
}

// mergeCondition folds a single JSON condition value into policy. AWS
// allows two shapes:
//
//	{"field": "value"}                 -> exact match
//	["op", "$field", "value"]          -> op = "eq" | "starts-with"
//	["content-length-range", min, max] -> size bounds
//
// "eq" is equivalent to the object form and treated as such.
func mergeCondition(policy *PostPolicy, raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return fmt.Errorf("empty condition")
	}
	switch trimmed[0] {
	case '{':
		var m map[string]string
		if err := json.Unmarshal(trimmed, &m); err != nil {
			return fmt.Errorf("object condition: %v", err)
		}
		if len(m) != 1 {
			return fmt.Errorf("object condition must have exactly one entry, got %d", len(m))
		}
		for k, v := range m {
			policy.Equals[normalizeField(k)] = v
		}
		return nil
	case '[':
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return fmt.Errorf("array condition: %v", err)
		}
		if len(arr) != 3 {
			return fmt.Errorf("array condition must have 3 elements, got %d", len(arr))
		}
		var op string
		if err := json.Unmarshal(arr[0], &op); err != nil {
			return fmt.Errorf("operator: %v", err)
		}
		switch op {
		case "eq":
			var field, value string
			if err := json.Unmarshal(arr[1], &field); err != nil {
				return fmt.Errorf("eq field: %v", err)
			}
			if err := json.Unmarshal(arr[2], &value); err != nil {
				return fmt.Errorf("eq value: %v", err)
			}
			policy.Equals[normalizeField(stripDollar(field))] = value
			return nil
		case "starts-with":
			var field, value string
			if err := json.Unmarshal(arr[1], &field); err != nil {
				return fmt.Errorf("starts-with field: %v", err)
			}
			if err := json.Unmarshal(arr[2], &value); err != nil {
				return fmt.Errorf("starts-with value: %v", err)
			}
			policy.StartsWith[normalizeField(stripDollar(field))] = value
			return nil
		case "content-length-range":
			var lo, hi int64
			if err := json.Unmarshal(arr[1], &lo); err != nil {
				return fmt.Errorf("content-length-range min: %v", err)
			}
			if err := json.Unmarshal(arr[2], &hi); err != nil {
				return fmt.Errorf("content-length-range max: %v", err)
			}
			if lo < 0 || hi < lo {
				return fmt.Errorf("content-length-range bounds invalid: [%d,%d]", lo, hi)
			}
			policy.ContentLengthMin = lo
			policy.ContentLengthMax = hi
			return nil
		default:
			return fmt.Errorf("unknown operator %q", op)
		}
	default:
		return fmt.Errorf("condition must be object or array")
	}
}

// stripDollar removes the leading "$" that AWS uses to reference form
// fields inside array conditions ("$key", "$Content-Type", etc).
func stripDollar(s string) string {
	if strings.HasPrefix(s, "$") {
		return s[1:]
	}
	return s
}

// normalizeField lowercases the field name. AWS treats POST form field
// names as case-insensitive on the server side (matching the
// case-insensitivity of HTTP header names that several form fields
// proxy: Content-Type, Content-Disposition, etc).
func normalizeField(s string) string {
	return strings.ToLower(s)
}

// signingFormFields is the set of form fields that participate in
// authenticating the request rather than carrying user data. They are
// implicitly trusted (their values are covered by the policy signature
// or are the signature itself) and do NOT need a matching condition.
var signingFormFields = map[string]struct{}{
	"policy":               {},
	"x-amz-signature":      {},
	"x-amz-algorithm":      {},
	"x-amz-credential":     {},
	"x-amz-date":           {},
	"x-amz-security-token": {},
	"file":                 {},
	// AWS quietly ignores these — they're for the browser/JS form layer.
	"awsaccesskeyid":       {},
	"signature":            {}, // pre-sigv4 legacy field
}

// PolicyViolation describes the first form field that didn't satisfy the
// policy. The field name is lowercased; Reason is human-readable for
// error messages, never parsed.
type PolicyViolation struct {
	Field  string
	Reason string
}

func (v *PolicyViolation) Error() string {
	return fmt.Sprintf("policy violation on %q: %s", v.Field, v.Reason)
}

// EnforcePolicy walks every supplied form field and checks it against the
// parsed policy. The rules, matching AWS:
//
//  1. Every non-signing field present in the form MUST have a matching
//     condition (Equals or StartsWith). Extras are rejected.
//  2. Every Equals condition MUST be satisfied by a form field of the
//     same name with the same value.
//  3. Every StartsWith condition MUST be satisfied by a form field of the
//     same name whose value has the required prefix.
//  4. content-length-range, when present, is enforced by the caller on
//     the actual file body — EnforcePolicy doesn't see the bytes.
//
// fields is a flat map of lowercased field name -> single value. POST
// Object forms repeat field names only for `file` (always exactly one)
// and never for signing/data fields, so a single-value map is sufficient
// for policy enforcement. The caller has already extracted these from
// the multipart reader.
//
// Returns *PolicyViolation on the first miss, or nil if the form
// satisfies the policy.
func EnforcePolicy(policy *PostPolicy, fields map[string]string) error {
	for name := range fields {
		if _, signing := signingFormFields[name]; signing {
			continue
		}
		if _, ok := policy.Equals[name]; ok {
			continue
		}
		if _, ok := policy.StartsWith[name]; ok {
			continue
		}
		return &PolicyViolation{Field: name, Reason: "no matching condition"}
	}

	for name, want := range policy.Equals {
		got, ok := fields[name]
		if !ok {
			return &PolicyViolation{Field: name, Reason: "required condition field missing from form"}
		}
		if got != want {
			return &PolicyViolation{Field: name, Reason: fmt.Sprintf("value %q does not equal required %q", got, want)}
		}
	}

	for name, prefix := range policy.StartsWith {
		got, ok := fields[name]
		if !ok {
			return &PolicyViolation{Field: name, Reason: "required prefix condition field missing from form"}
		}
		if !strings.HasPrefix(got, prefix) {
			return &PolicyViolation{Field: name, Reason: fmt.Sprintf("value %q does not start with %q", got, prefix)}
		}
	}

	return nil
}
