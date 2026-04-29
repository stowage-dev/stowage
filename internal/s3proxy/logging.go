// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"net/url"
	"strings"
)

// redactedQueryKeys are redacted out of logged URLs.
var redactedQueryKeys = map[string]struct{}{
	"X-Amz-Signature":      {},
	"X-Amz-Credential":     {},
	"X-Amz-Security-Token": {},
}

// RedactPath returns the path + query of u with known-sensitive query params
// replaced with "REDACTED". The intent is that logged URLs are safe to ship
// to a log aggregator without leaking any part of a SigV4 signature.
func RedactPath(u *url.URL) string {
	if u == nil {
		return ""
	}
	if u.RawQuery == "" {
		return u.Path
	}
	q := u.Query()
	for k := range q {
		if _, redact := redactedQueryKeys[k]; redact {
			q.Set(k, "REDACTED")
		}
	}
	return u.Path + "?" + q.Encode()
}

// RedactHeaders returns a shallow copy of h with all auth / SigV4 fields
// dropped. Used when we need to include request headers in a log.
func RedactHeaders(h map[string][]string) map[string][]string {
	out := make(map[string][]string, len(h))
	for k, v := range h {
		lk := strings.ToLower(k)
		switch lk {
		case "authorization", "x-amz-security-token":
			out[k] = []string{"REDACTED"}
			continue
		}
		if strings.HasPrefix(lk, "x-amz-signature") {
			out[k] = []string{"REDACTED"}
			continue
		}
		out[k] = append([]string(nil), v...)
	}
	return out
}
