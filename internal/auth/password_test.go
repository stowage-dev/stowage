// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"errors"
	"testing"
)

func TestHashAndVerifyRoundTrip(t *testing.T) {
	const pw = "correct-horse-battery-staple-1!"
	h, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := VerifyPassword(pw, h); err != nil {
		t.Fatalf("verify(correct): %v", err)
	}
	if err := VerifyPassword("wrong", h); !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("verify(wrong) = %v; want ErrPasswordMismatch", err)
	}
}

func TestHashesAreDistinct(t *testing.T) {
	// Same input should produce different hashes due to random salts.
	a, _ := HashPassword("password")
	b, _ := HashPassword("password")
	if a == b {
		t.Fatalf("two hashes of same password are identical; salt not random")
	}
}

func TestPolicyCheck(t *testing.T) {
	p := PasswordPolicy{MinLength: 12}

	cases := []struct {
		name    string
		pw      string
		wantErr bool
	}{
		{"too-short", "aA1!aa", true},
		{"length-only-repetitive", "aaaaaaaaaaaa", false},
		{"length-only-one-class", "aaaaaaaaaaaaaaaa", false},
		{"length-12", "abcdEFGH1234", false},
		{"strong-mixed", "S3cr3t-Passw0rd!", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := p.Check(c.pw)
			if (err != nil) != c.wantErr {
				t.Fatalf("Check(%q) err=%v wantErr=%v", c.pw, err, c.wantErr)
			}
		})
	}
}

func TestRandomTokenEntropy(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 100; i++ {
		tok, err := RandomToken()
		if err != nil {
			t.Fatalf("RandomToken: %v", err)
		}
		if len(tok) < 40 {
			t.Fatalf("unexpectedly short token: %q", tok)
		}
		if _, dup := seen[tok]; dup {
			t.Fatalf("duplicate token after only %d iterations", i)
		}
		seen[tok] = struct{}{}
	}
}
