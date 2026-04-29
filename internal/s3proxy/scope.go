// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import "strings"

// EnforceScope returns true iff the request targets any bucket the credential
// is scoped to. The check is set-membership across the credential's
// BucketScopes; for legacy 1:1 credentials with a single scope it degenerates
// to the previous exact-match behaviour. Service-level (no-bucket) requests
// are allowed through for specific synthesised handlers (ListBuckets).
func EnforceScope(scopes []BucketScope, requestBucket string) bool {
	if requestBucket == "" {
		// Service-level ops are handled elsewhere; scope check is not relevant.
		return true
	}
	for _, s := range scopes {
		if strings.EqualFold(s.BucketName, requestBucket) {
			return true
		}
	}
	return false
}
