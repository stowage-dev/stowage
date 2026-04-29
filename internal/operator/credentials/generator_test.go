// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package credentials

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateVirtual(t *testing.T) {
	akidRE := regexp.MustCompile(`^AKIA[A-Z2-7]{16}$`)
	skRE := regexp.MustCompile(`^[A-Za-z0-9_-]{40}$`)

	seen := make(map[string]struct{})
	for i := 0; i < 1000; i++ {
		k, err := GenerateVirtual()
		require.NoError(t, err)
		require.Truef(t, akidRE.MatchString(k.AccessKeyID), "bad AKID %q", k.AccessKeyID)
		require.Truef(t, skRE.MatchString(k.SecretAccessKey), "bad SK %q", k.SecretAccessKey)
		_, dup := seen[k.AccessKeyID]
		require.False(t, dup, "duplicate AKID generated in small sample: %s", k.AccessKeyID)
		seen[k.AccessKeyID] = struct{}{}
	}
}
