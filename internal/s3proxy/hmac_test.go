// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"crypto/hmac"
	"crypto/sha256"
	"hash"
)

// newHMAC returns an HMAC-SHA256 hasher keyed with key. Separate file so the
// single symbol is available from any _test.go in the package.
func newHMAC(key []byte) hash.Hash { return hmac.New(sha256.New, key) }
