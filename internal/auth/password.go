// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// argon2id profile: m=64 MiB, t=3, p=2. OWASP 2024 minimum.
const (
	argonMemory  uint32 = 64 * 1024 // 64 MiB in KiB
	argonTime    uint32 = 3
	argonThreads uint8  = 2
	argonKeyLen  uint32 = 32
	argonSaltLen int    = 16
)

// HashPassword returns a PHC-format argon2id hash:
// $argon2id$v=19$m=65536,t=3,p=2$<salt>$<hash>
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum),
	), nil
}

// VerifyPassword returns nil on match, an error otherwise.
func VerifyPassword(password, encoded string) error {
	parts := strings.Split(encoded, "$")
	// "", "argon2id", "v=19", "m=..,t=..,p=..", "<salt>", "<hash>"
	if len(parts) != 6 || parts[1] != "argon2id" {
		return errors.New("invalid password hash format")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return err
	}
	if version != argon2.Version {
		return fmt.Errorf("unsupported argon2 version %d", version)
	}

	var mem, time uint32
	var par uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &time, &par); err != nil {
		return err
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return err
	}

	got := argon2.IDKey([]byte(password), salt, time, mem, par, uint32(len(want)))
	if subtle.ConstantTimeCompare(want, got) == 1 {
		return nil
	}
	return ErrPasswordMismatch
}

// ErrPasswordMismatch is returned by VerifyPassword when the hash does not
// match. Callers must not surface this distinctly to unauthenticated users:
// login errors are uniform regardless of failure cause.
var ErrPasswordMismatch = errors.New("password does not match")

// PasswordPolicy validates a candidate password against config.
type PasswordPolicy struct {
	MinLength    int
	PreventReuse bool
}

// Check returns nil if the password meets policy, else a PolicyError.
func (p PasswordPolicy) Check(password string) error {
	if p.MinLength > 0 && len(password) < p.MinLength {
		return &PolicyError{Reason: fmt.Sprintf("password must be at least %d characters", p.MinLength)}
	}
	return nil
}

type PolicyError struct{ Reason string }

func (e *PolicyError) Error() string { return e.Reason }
