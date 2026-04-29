// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package vcstore manages the two Secrets produced for every BucketClaim:
//
//  1. Internal — in the operator namespace, authoritative mapping from
//     access_key_id to secret_access_key + bucket + backend. Proxy reads these.
//  2. Consumer — in the claim's namespace, the AWS_* env vars tenants mount.
//
// Nothing else in the system is allowed to write to these Secrets; all access
// funnels through this package so the label scheme stays consistent.
package vcstore

const (
	LabelRole        = "broker.stowage.io/role"
	LabelClaimNS     = "broker.stowage.io/claim-namespace"
	LabelClaimName   = "broker.stowage.io/claim-name"
	LabelClaimUID    = "broker.stowage.io/claim-uid"
	LabelAccessKeyID = "broker.stowage.io/access-key-id"
	LabelBackendName = "broker.stowage.io/backend"
	LabelBucketName  = "broker.stowage.io/bucket"
	LabelRotationGen = "broker.stowage.io/rotation-generation"

	RoleVirtualCredential = "virtual-credential"
	RoleConsumerSecret    = "consumer-secret"
	RoleAnonymousBinding  = "anonymous-binding"

	AnnotationExpiresAt = "broker.stowage.io/expires-at"

	DataAccessKeyID     = "access_key_id"
	DataSecretAccessKey = "secret_access_key"
	DataBucketName      = "bucket_name"
	DataClaimUID        = "claim_uid"
	DataBackend         = "backend"
	DataAnonMode        = "anonymous_mode"
	DataAnonRPS         = "anonymous_per_source_ip_rps"
	// DataBucketScopes, when present, is a JSON-encoded []BucketScope that
	// authoritatively lists every (bucket, backend) the credential grants
	// access to. Readers that see this key MUST prefer it over the singular
	// DataBucketName/DataBackend, which stay populated with the primary scope
	// so legacy consumers keep working.
	DataBucketScopes = "bucket_scopes"

	// DataQuotaSoftBytes / DataQuotaHardBytes are decimal byte counts the
	// operator copies from BucketClaim.spec.quota into the proxy-facing
	// virtual-credential Secret. Stowage's KubernetesLimitSource reads
	// these to populate its in-memory limit cache; absent / empty means
	// "no limit". Decimal (not k8s Quantity) so the proxy doesn't need to
	// link in apimachinery just to parse a number.
	DataQuotaSoftBytes = "quota_soft_bytes"
	DataQuotaHardBytes = "quota_hard_bytes"

	EnvAccessKeyID     = "AWS_ACCESS_KEY_ID"
	EnvSecretAccessKey = "AWS_SECRET_ACCESS_KEY"
	EnvRegion          = "AWS_REGION"
	EnvEndpointURL     = "AWS_ENDPOINT_URL"
	EnvEndpointURLS3   = "AWS_ENDPOINT_URL_S3"
	EnvBucketName      = "BUCKET_NAME"
	EnvAddressingStyle = "S3_ADDRESSING_STYLE"
)

// InternalSecretName derives a stable, unique-per-credential name from an access key id.
// Input is already constrained to [A-Z0-9], so lower-casing gives a DNS-safe label.
func InternalSecretName(accessKeyID string) string {
	b := make([]byte, 0, len("vc-")+len(accessKeyID))
	b = append(b, []byte("vc-")...)
	for i := 0; i < len(accessKeyID); i++ {
		c := accessKeyID[i]
		if c >= 'A' && c <= 'Z' {
			c = c + ('a' - 'A')
		}
		b = append(b, c)
	}
	return string(b)
}

// AnonymousBindingSecretName derives a deterministic Secret name for the
// anonymous binding of a given (claim namespace, claim name) pair. Names are
// short and DNS-safe: input is already constrained to label-style characters.
func AnonymousBindingSecretName(claimNS, claimName string) string {
	return "anon-" + claimNS + "-" + claimName
}
