// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package sigv4verifier verifies AWS SigV4 signatures on inbound HTTP
// requests. It implements the spec at
// https://docs.aws.amazon.com/general/latest/gr/sigv4_signing.html
// and is referenced against MinIO's cmd/signature-v4.go.
//
// Security notes:
//
//   - All signature comparison is constant-time (crypto/subtle).
//   - Date skew is enforced at +-15 minutes.
//   - Presigned URLs have their X-Amz-Expires honored before the signature
//     is verified.
//   - Canonicalization is defensive: unknown X-Amz-* headers don't accidentally
//     influence the canonical form unless explicitly signed.
//
// The verifier never touches request bodies; the caller is responsible for
// enforcing any payload-hash semantics downstream of this package.
package sigv4verifier
