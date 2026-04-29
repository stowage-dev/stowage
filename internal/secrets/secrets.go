// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package secrets seals/opens at-rest secrets (e.g. S3 secret keys stored in
// the backends table) under a single AES-256-GCM key loaded from the
// environment.
//
// Ciphertext layout on disk:
//
//	version(1) || key_id(1) || nonce(12) || ciphertext||tag
//
// version and key_id are reserved so a future multi-key rotation can extend
// Open without rewriting existing rows. v1 only knows version=0x01, key_id=0x00.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	version1  byte = 0x01
	keyIDV1   byte = 0x00
	nonceLen       = 12
	headerLen      = 2 + nonceLen // version + key_id + nonce
	keyLen         = 32           // AES-256
)

// Key is a 32-byte AES-256 key. The zero value is invalid.
type Key [keyLen]byte

// Sealer wraps a single key and exposes Seal/Open. Construct with Load.
type Sealer struct {
	key  Key
	aead cipher.AEAD
}

var (
	// ErrNoKey is returned by LoadFromEnv when the env var is unset.
	ErrNoKey = errors.New("secrets: env var unset")
	// ErrInvalidKey is returned when the env var is set but cannot be
	// parsed as 32 random bytes (hex64 or base64-32).
	ErrInvalidKey = errors.New("secrets: key must be 32 bytes encoded as 64 hex chars or 44 base64 chars")
	// ErrUnknownVersion is returned by Open when the leading version byte
	// is not recognised. v1 only knows 0x01.
	ErrUnknownVersion = errors.New("secrets: unknown ciphertext version")
	// ErrUnknownKeyID is returned by Open when the key_id byte is not 0x00.
	// Reserved for forward compatibility with key rotation.
	ErrUnknownKeyID = errors.New("secrets: unknown key id")
	// ErrCiphertextTooShort is returned when input is shorter than the
	// fixed header + tag.
	ErrCiphertextTooShort = errors.New("secrets: ciphertext too short")
)

// LoadFromEnv reads the named env var and parses it as a 32-byte key encoded
// as either 64 hex chars or standard base64 (44 chars including padding).
// Returns ErrNoKey if the var is unset or empty.
func LoadFromEnv(name string) (*Sealer, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return nil, ErrNoKey
	}
	return New(raw)
}

// LoadOrGenerateFile loads a key from path, or generates a fresh 32-byte key
// and writes it (hex-encoded, 0600) if the file does not yet exist. The
// generated bool reports which branch ran so the caller can log a one-time
// warning on first boot.
//
// The file holds the key in the same hex/base64 format New accepts; trailing
// whitespace is tolerated so editors that auto-append a newline don't break
// loads. The parent directory must already exist — we don't MkdirAll here
// because that would mask typos in operator-supplied paths.
func LoadOrGenerateFile(path string) (s *Sealer, generated bool, err error) {
	if path == "" {
		return nil, false, fmt.Errorf("secrets: empty key file path")
	}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		s, err := New(strings.TrimSpace(string(data)))
		if err != nil {
			return nil, false, fmt.Errorf("secrets: read %s: %w", path, err)
		}
		return s, false, nil
	case errors.Is(err, os.ErrNotExist):
		// fall through to generate
	default:
		return nil, false, fmt.Errorf("secrets: stat %s: %w", path, err)
	}

	var k Key
	if _, err := rand.Read(k[:]); err != nil {
		return nil, false, fmt.Errorf("secrets: rand: %w", err)
	}
	encoded := hex.EncodeToString(k[:])
	// O_EXCL guards against a concurrent boot also generating the key.
	// If two processes race, exactly one wins; the loser falls back to
	// re-reading the winning file on the next call.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return LoadOrGenerateFile(path)
		}
		return nil, false, fmt.Errorf("secrets: create %s: %w", filepath.Clean(path), err)
	}
	if _, werr := f.WriteString(encoded); werr != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, false, fmt.Errorf("secrets: write %s: %w", path, werr)
	}
	if cerr := f.Close(); cerr != nil {
		return nil, false, fmt.Errorf("secrets: close %s: %w", path, cerr)
	}
	sealer, err := New(encoded)
	if err != nil {
		return nil, false, err
	}
	return sealer, true, nil
}

// New parses an encoded key string and returns a ready Sealer.
func New(encoded string) (*Sealer, error) {
	var k Key
	switch len(encoded) {
	case hex.EncodedLen(keyLen): // 64
		b, err := hex.DecodeString(encoded)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
		}
		copy(k[:], b)
	case base64.StdEncoding.EncodedLen(keyLen): // 44
		b, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(b) != keyLen {
			return nil, ErrInvalidKey
		}
		copy(k[:], b)
	default:
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(k[:])
	if err != nil {
		return nil, fmt.Errorf("secrets: aes: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: gcm: %w", err)
	}
	return &Sealer{key: k, aead: aead}, nil
}

// Seal encrypts plaintext and returns version || key_id || nonce || ct||tag.
// An empty plaintext returns a nil slice — callers that want "no secret
// stored" can persist nil and skip Open entirely.
func (s *Sealer) Seal(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, nil
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secrets: rand: %w", err)
	}
	out := make([]byte, 0, headerLen+len(plaintext)+s.aead.Overhead())
	out = append(out, version1, keyIDV1)
	out = append(out, nonce...)
	out = s.aead.Seal(out, nonce, plaintext, headerOnly(out))
	return out, nil
}

// Open reverses Seal. Empty input returns an empty plaintext (matches the
// Seal-of-empty contract). Rejects unknown version / key_id bytes so a
// future multi-key rollout can refuse to silently downgrade.
func (s *Sealer) Open(ct []byte) ([]byte, error) {
	if len(ct) == 0 {
		return nil, nil
	}
	if len(ct) < headerLen+s.aead.Overhead() {
		return nil, ErrCiphertextTooShort
	}
	if ct[0] != version1 {
		return nil, ErrUnknownVersion
	}
	if ct[1] != keyIDV1 {
		return nil, ErrUnknownKeyID
	}
	nonce := ct[2:headerLen]
	body := ct[headerLen:]
	pt, err := s.aead.Open(nil, nonce, body, ct[:headerLen])
	if err != nil {
		return nil, fmt.Errorf("secrets: open: %w", err)
	}
	return pt, nil
}

// headerOnly returns the leading version+key_id+nonce bytes of out, used
// as AEAD additional-data so any tampering with the header fails Open.
func headerOnly(out []byte) []byte { return out[:headerLen] }
