// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package credentials

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"fmt"
	"strings"
)

// VirtualKey is a tenant-facing access/secret pair. AccessKeyID is formatted
// to satisfy AWS SDK validators ("AKIA" + 16 base32 chars, upper-case, no
// padding). No QuObjects prefix is ever present.
type VirtualKey struct {
	AccessKeyID     string
	SecretAccessKey string
}

// GenerateVirtual produces a fresh random virtual credential. The output is
// statistically unique; callers may additionally check for collisions against
// an existing index.
func GenerateVirtual() (VirtualKey, error) {
	akid, err := generateAccessKeyID()
	if err != nil {
		return VirtualKey{}, err
	}
	sk, err := generateSecretKey()
	if err != nil {
		return VirtualKey{}, err
	}
	return VirtualKey{AccessKeyID: akid, SecretAccessKey: sk}, nil
}

func generateAccessKeyID() (string, error) {
	var raw [10]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("rand read: %w", err)
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:])
	enc = strings.ToUpper(enc)
	if len(enc) < 16 {
		return "", fmt.Errorf("unexpected base32 length %d", len(enc))
	}
	return "AKIA" + enc[:16], nil
}

func generateSecretKey() (string, error) {
	var raw [30]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("rand read: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
