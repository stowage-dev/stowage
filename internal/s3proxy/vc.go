// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import "time"

// VirtualCredential is one access key issued to a tenant SDK. It carries
// the secret used to verify the inbound SigV4 signature plus the bucket
// scope the credential is authorised for. A credential may be sourced from
// the SQLite store (admin-managed) or from a Kubernetes Secret (operator-
// managed); the merged source layer hides the origin from the proxy.
type VirtualCredential struct {
	AccessKeyID     string
	SecretAccessKey string

	// BucketScopes is the authoritative scope set. The proxy enforces
	// set-membership per request. Legacy 1:1 credentials carry a single
	// element; N:1 grants carry N.
	BucketScopes []BucketScope

	// BackendName is the stowage backend id the credential's traffic is
	// forwarded to. All scopes must agree on backend; the source layer is
	// responsible for synthesising one entry per (backend, bucket) pair.
	BackendName string

	// ExpiresAt, when non-nil and in the past, causes Lookup to miss.
	ExpiresAt *time.Time

	// UserID is set when a SQLite-managed credential names a stowage user
	// for audit attribution. Empty for K8s-sourced credentials. Audit
	// events always log NULL user_id; the field is here only so the audit
	// detail can carry the attribution context.
	UserID string

	// ClaimNamespace / ClaimName are populated for K8s-sourced credentials
	// and exposed to the audit log via the Detail map; empty for SQLite-
	// sourced credentials.
	ClaimNamespace string
	ClaimName      string

	// Source identifies where the credential was loaded from
	// ("sqlite" | "kubernetes"). Used for the K8s-wins logging only.
	Source string
}

// BucketScope is one (backend, bucket) tuple a credential is authorised
// for. The proxy enforces scope before forwarding any bucket-level op.
//
// JSON tags MUST match the stowage operator's wire format
// (`{"bucket":"…","backend":"…"}`) so K8s-sourced secrets carrying a
// bucket_scopes field deserialise correctly.
type BucketScope struct {
	BucketName  string `json:"bucket"`
	BackendName string `json:"backend"`
}

// AnonymousBinding governs unauthenticated reads against a single bucket.
// The cluster-wide kill switch lives in config (s3_proxy.anonymous_enabled);
// individual bindings are persisted in the SQLite store or surfaced from
// Kubernetes via the same informer that powers VirtualCredential.
type AnonymousBinding struct {
	BackendName    string
	BucketName     string
	Mode           string // currently always "ReadOnly"
	PerSourceIPRPS float64
	Source         string
}
