// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import "net/http"

// anonymousReadAllowedOps is the closed allowlist of operation names a
// ReadOnly bucket permits to anonymous callers. Anything not in this set —
// including sub-resource queries like ?acl, ?policy, ?versioning — is
// rejected before it can reach the backend.
var anonymousReadAllowedOps = map[string]struct{}{
	"GetObject":   {},
	"HeadObject":  {},
	"HeadBucket":  {},
	"ListObjects": {},
}

// anonymousBlockedSubresources lists query parameters that flip a request
// from a basic read into a sub-resource operation. Even when the HTTP method
// is GET/HEAD, presence of any of these denies the request anonymously.
var anonymousBlockedSubresources = []string{
	"acl",
	"policy",
	"versioning",
	"logging",
	"tagging",
	"cors",
	"lifecycle",
	"encryption",
	"replication",
	"notification",
	"analytics",
	"inventory",
	"metrics",
	"accelerate",
	"requestPayment",
	"website",
	"object-lock",
	"legal-hold",
	"retention",
	"uploads",
	"uploadId",
	"delete",
	"restore",
	"select",
	"versionId",
	"torrent",
}

// IsRequestUnauthenticated returns true when the request carries neither a
// SigV4 Authorization header nor a presigned X-Amz-Signature query parameter.
// These are the only two ways a client can prove identity, so absence means
// the request is a candidate for the anonymous path.
func IsRequestUnauthenticated(r *http.Request) bool {
	if r.Header.Get("Authorization") != "" {
		return false
	}
	if r.URL.Query().Get("X-Amz-Signature") != "" {
		return false
	}
	return true
}

// AnonymousOpAllowed reports whether the classified operation is permitted
// for the given anonymous mode. Sub-resource queries are rejected regardless
// of mode.
func AnonymousOpAllowed(mode, operation string, q map[string][]string) bool {
	if mode != AnonModeReadOnly {
		return false
	}
	if _, ok := anonymousReadAllowedOps[operation]; !ok {
		return false
	}
	for _, k := range anonymousBlockedSubresources {
		if _, has := q[k]; has {
			return false
		}
	}
	return true
}

// AnonModeReadOnly is the only currently supported anonymous-binding mode.
// Stored as a string column in the s3_anonymous_bindings table so future
// modes (e.g. "ReadWrite") can be added without a migration.
const AnonModeReadOnly = "ReadOnly"
