// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package secrets_test

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stowage-dev/stowage/internal/secrets"
)

func newSealer(t *testing.T) *secrets.Sealer {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	s, err := secrets.New(hex.EncodeToString(key))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestSealOpenRoundTrip(t *testing.T) {
	s := newSealer(t)
	for _, pt := range [][]byte{
		[]byte("hunter2"),
		[]byte(""),
		bytes.Repeat([]byte("x"), 1024),
	} {
		ct, err := s.Seal(pt)
		if err != nil {
			t.Fatalf("Seal: %v", err)
		}
		got, err := s.Open(ct)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if !bytes.Equal(got, pt) {
			t.Fatalf("round-trip mismatch: got %q want %q", got, pt)
		}
	}
}

func TestSealEmptyIsNil(t *testing.T) {
	s := newSealer(t)
	ct, err := s.Seal(nil)
	if err != nil || ct != nil {
		t.Fatalf("Seal(nil) = (%v, %v), want (nil, nil)", ct, err)
	}
	pt, err := s.Open(nil)
	if err != nil || pt != nil {
		t.Fatalf("Open(nil) = (%v, %v), want (nil, nil)", pt, err)
	}
}

func TestSealHeaderBytes(t *testing.T) {
	s := newSealer(t)
	ct, err := s.Seal([]byte("hi"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if ct[0] != 0x01 {
		t.Fatalf("version byte = 0x%02x, want 0x01", ct[0])
	}
	if ct[1] != 0x00 {
		t.Fatalf("key_id byte = 0x%02x, want 0x00", ct[1])
	}
	// 1+1 header + 12-byte nonce + at least 1 byte plaintext + 16-byte tag
	if len(ct) < 2+12+1+16 {
		t.Fatalf("ciphertext too short: %d", len(ct))
	}
}

func TestNonceUnique(t *testing.T) {
	s := newSealer(t)
	a, _ := s.Seal([]byte("same"))
	b, _ := s.Seal([]byte("same"))
	if bytes.Equal(a[2:14], b[2:14]) {
		t.Fatal("nonce reused across two Seal calls")
	}
	if bytes.Equal(a, b) {
		t.Fatal("identical ciphertext for identical plaintext (nonce reuse?)")
	}
}

func TestOpenRejectsUnknownVersion(t *testing.T) {
	s := newSealer(t)
	ct, _ := s.Seal([]byte("hi"))
	ct[0] = 0x02
	_, err := s.Open(ct)
	if !errors.Is(err, secrets.ErrUnknownVersion) {
		t.Fatalf("Open with bad version: err=%v, want ErrUnknownVersion", err)
	}
}

func TestOpenRejectsUnknownKeyID(t *testing.T) {
	s := newSealer(t)
	ct, _ := s.Seal([]byte("hi"))
	ct[1] = 0x07
	_, err := s.Open(ct)
	if !errors.Is(err, secrets.ErrUnknownKeyID) {
		t.Fatalf("Open with bad key_id: err=%v, want ErrUnknownKeyID", err)
	}
}

func TestOpenRejectsHeaderTamper(t *testing.T) {
	// Header is bound as additional-data, so flipping a reserved bit (after
	// passing the version/key_id checks) must still fail authentication.
	s := newSealer(t)
	ct, _ := s.Seal([]byte("hi"))
	// Flip a nonce byte without touching version/key_id.
	ct[5] ^= 0x01
	_, err := s.Open(ct)
	if err == nil || errors.Is(err, secrets.ErrUnknownVersion) || errors.Is(err, secrets.ErrUnknownKeyID) {
		t.Fatalf("expected GCM auth failure, got %v", err)
	}
}

func TestOpenRejectsTooShort(t *testing.T) {
	s := newSealer(t)
	_, err := s.Open([]byte{0x01, 0x00, 0x00})
	if !errors.Is(err, secrets.ErrCiphertextTooShort) {
		t.Fatalf("err=%v, want ErrCiphertextTooShort", err)
	}
}

func TestNewAcceptsHexAndBase64(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i * 7)
	}
	hexEnc := hex.EncodeToString(raw)
	b64Enc := base64.StdEncoding.EncodeToString(raw)

	for _, enc := range []string{hexEnc, b64Enc} {
		s, err := secrets.New(enc)
		if err != nil {
			t.Fatalf("New(%q): %v", enc, err)
		}
		ct, _ := s.Seal([]byte("ok"))
		pt, err := s.Open(ct)
		if err != nil || string(pt) != "ok" {
			t.Fatalf("round-trip with %q: pt=%q err=%v", enc, pt, err)
		}
	}
}

func TestNewRejectsBadEncoding(t *testing.T) {
	for _, bad := range []string{
		"",
		"too-short",
		strings.Repeat("z", 64), // hex length but non-hex chars
		strings.Repeat("!", 44), // base64 length but invalid
	} {
		if _, err := secrets.New(bad); err == nil {
			t.Fatalf("New(%q) accepted bad input", bad)
		}
	}
}

func TestKeysDontInterop(t *testing.T) {
	a, _ := secrets.New(hex.EncodeToString(bytes.Repeat([]byte{0xAA}, 32)))
	b, _ := secrets.New(hex.EncodeToString(bytes.Repeat([]byte{0xBB}, 32)))
	ct, _ := a.Seal([]byte("hi"))
	if _, err := b.Open(ct); err == nil {
		t.Fatal("Open succeeded with wrong key")
	}
}

func TestLoadOrGenerateFile_Generate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stowage.key")

	s, generated, err := secrets.LoadOrGenerateFile(path)
	if err != nil {
		t.Fatalf("LoadOrGenerateFile: %v", err)
	}
	if !generated {
		t.Fatal("expected generated=true on first call")
	}
	if s == nil {
		t.Fatal("nil sealer")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("perm = %o, want 0600", perm)
		}
	}

	// Round-trip through the freshly generated sealer.
	ct, err := s.Seal([]byte("hi"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	pt, err := s.Open(ct)
	if err != nil || string(pt) != "hi" {
		t.Fatalf("round-trip: pt=%q err=%v", pt, err)
	}
}

func TestLoadOrGenerateFile_LoadExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stowage.key")

	// Pre-seed with a known key (plus trailing newline to confirm trim).
	raw := bytes.Repeat([]byte{0x5A}, 32)
	if err := os.WriteFile(path, []byte(hex.EncodeToString(raw)+"\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	s, generated, err := secrets.LoadOrGenerateFile(path)
	if err != nil {
		t.Fatalf("LoadOrGenerateFile: %v", err)
	}
	if generated {
		t.Fatal("expected generated=false when file already exists")
	}

	// A sealer built directly from the same hex must interop.
	ref, err := secrets.New(hex.EncodeToString(raw))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ct, _ := ref.Seal([]byte("ping"))
	pt, err := s.Open(ct)
	if err != nil || string(pt) != "ping" {
		t.Fatalf("loaded sealer doesn't match seeded key: pt=%q err=%v", pt, err)
	}
}

func TestLoadOrGenerateFile_StableAcrossCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stowage.key")

	first, gen1, err := secrets.LoadOrGenerateFile(path)
	if err != nil || !gen1 {
		t.Fatalf("first call: gen=%v err=%v", gen1, err)
	}
	second, gen2, err := secrets.LoadOrGenerateFile(path)
	if err != nil || gen2 {
		t.Fatalf("second call: gen=%v err=%v", gen2, err)
	}

	ct, _ := first.Seal([]byte("persist"))
	pt, err := second.Open(ct)
	if err != nil || string(pt) != "persist" {
		t.Fatalf("second sealer can't open first's ct: pt=%q err=%v", pt, err)
	}
}

func TestLoadOrGenerateFile_RejectsCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stowage.key")
	if err := os.WriteFile(path, []byte("not-a-real-key"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, _, err := secrets.LoadOrGenerateFile(path)
	if err == nil {
		t.Fatal("expected error on corrupt key file")
	}
}

func TestLoadOrGenerateFile_EmptyPath(t *testing.T) {
	if _, _, err := secrets.LoadOrGenerateFile(""); err == nil {
		t.Fatal("expected error for empty path")
	}
}
