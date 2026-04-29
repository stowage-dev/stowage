// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactPath_StripsSignature(t *testing.T) {
	u, _ := url.Parse("/b/k?X-Amz-Signature=deadbeef&X-Amz-Credential=akid/20260424/us-east-1/s3/aws4_request&foo=bar")
	got := RedactPath(u)
	require.NotContains(t, got, "deadbeef")
	require.NotContains(t, got, "akid/20260424")
	require.Contains(t, got, "REDACTED")
	require.Contains(t, got, "foo=bar")
}

func TestRedactHeaders(t *testing.T) {
	in := map[string][]string{
		"Authorization":        {"AWS4-HMAC-SHA256 Credential=akid/...,Signature=cafe"},
		"X-Amz-Security-Token": {"token"},
		"Host":                 {"example.com"},
	}
	out := RedactHeaders(in)
	require.Equal(t, "REDACTED", out["Authorization"][0])
	require.Equal(t, "REDACTED", out["X-Amz-Security-Token"][0])
	require.Equal(t, "example.com", out["Host"][0])
}
