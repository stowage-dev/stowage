// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIPLimiter_AllowAtPerIP(t *testing.T) {
	l := NewIPLimiter(0)
	// Burst is int(rate*2)+1, so at 1 RPS the bucket starts with 3 tokens.
	// Drain them and then assert the next call from the same IP is rejected
	// while a fresh IP still gets a token from its own bucket.
	for i := 0; i < 3; i++ {
		require.True(t, l.AllowAt("10.0.0.1", 1))
	}
	require.False(t, l.AllowAt("10.0.0.1", 1))
	require.True(t, l.AllowAt("10.0.0.2", 1))
}

func TestIPLimiter_NilOrUnlimited(t *testing.T) {
	var l *IPLimiter
	require.True(t, l.Allow("any"))
	require.True(t, l.AllowAt("any", 0))

	l = NewIPLimiter(0)
	require.True(t, l.Allow("any"))
	require.True(t, l.AllowAt("any", 0))
}

func TestClientIP_NoXFFReturnsRemote(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "192.0.2.5:54321"
	require.Equal(t, "192.0.2.5", ClientIP(r, nil))
}

func TestClientIP_TrustsXFFWhenTrustedProxiesNil(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.1.2.3:1"
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 10.1.2.3")
	require.Equal(t, "203.0.113.7", ClientIP(r, nil))
}

func TestClientIP_RejectsXFFFromUntrustedProxy(t *testing.T) {
	cidrs, err := ParseCIDRs([]string{"10.0.0.0/8"})
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.99:1"
	r.Header.Set("X-Forwarded-For", "192.0.2.5")
	require.Equal(t, "203.0.113.99", ClientIP(r, cidrs))
}

func TestClientIP_AcceptsXFFFromTrustedProxy(t *testing.T) {
	cidrs, err := ParseCIDRs([]string{"10.0.0.0/8"})
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.5.5.5:1"
	r.Header.Set("X-Forwarded-For", "192.0.2.5")
	require.Equal(t, "192.0.2.5", ClientIP(r, cidrs))
}

func TestParseCIDRs_Empty(t *testing.T) {
	cidrs, err := ParseCIDRs(nil)
	require.NoError(t, err)
	require.Nil(t, cidrs)
}

func TestParseCIDRs_Bad(t *testing.T) {
	_, err := ParseCIDRs([]string{"not-a-cidr"})
	require.Error(t, err)
}
