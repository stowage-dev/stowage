// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package backend

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderBucketName_Override(t *testing.T) {
	out, err := RenderBucketName("my-override", "", "x", "y")
	require.NoError(t, err)
	require.Equal(t, "my-override", out)
}

func TestRenderBucketName_DefaultTemplate(t *testing.T) {
	out, err := RenderBucketName("", "", "my-app", "uploads")
	require.NoError(t, err)
	require.Equal(t, "my-app-uploads", out)
}

func TestRenderBucketName_Hash(t *testing.T) {
	out, err := RenderBucketName("", "bb-{{ .Hash }}", "a", "b")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(out, "bb-"))
	require.Len(t, out, len("bb-")+8)
}

func TestRenderBucketName_InvalidResult(t *testing.T) {
	// uppercase output is not a valid bucket name
	_, err := RenderBucketName("", "{{ .Name }}", "ns", "UPPER")
	require.Error(t, err)
}

func TestRenderBucketName_InvalidOverride(t *testing.T) {
	_, err := RenderBucketName("UPPER", "", "ns", "n")
	require.Error(t, err)
}

func TestValidateTemplate(t *testing.T) {
	require.NoError(t, ValidateTemplate("{{ .Namespace }}-{{ .Name }}"))
	require.Error(t, ValidateTemplate("{{ .NotAThing }}"))
}
