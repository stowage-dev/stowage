// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProxyTrustEmptyTrustsAll(t *testing.T) {
	pt, err := NewProxyTrust(nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	for _, addr := range []string{"10.0.0.1:1234", "8.8.8.8:443", "[::1]:80"} {
		if !pt.IsTrusted(addr) {
			t.Errorf("empty trust list should trust %q", addr)
		}
	}
	// nil receiver remains the "no proxy wired" escape hatch.
	var nilPT *ProxyTrust
	if nilPT.IsTrusted("10.0.0.1:1234") {
		t.Fatal("nil ProxyTrust must still distrust everyone")
	}
}

func TestProxyTrustAcceptsCIDRAndBareIP(t *testing.T) {
	pt, err := NewProxyTrust([]string{"10.0.0.0/8", "192.168.1.5", "::1"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	cases := []struct {
		addr   string
		expect bool
	}{
		{"10.4.5.6:1234", true},
		{"192.168.1.5:80", true},
		{"192.168.1.6:80", false},
		{"[::1]:443", true},
		{"8.8.8.8:53", false},
		{"garbage", false},
	}
	for _, c := range cases {
		if got := pt.IsTrusted(c.addr); got != c.expect {
			t.Errorf("IsTrusted(%q)=%v want %v", c.addr, got, c.expect)
		}
	}
}

func TestProxyTrustClientIPRespectsTrust(t *testing.T) {
	trustingOnly10 := mustTrust(t, "10.0.0.0/8")

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.1.2.3:9000"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	if got := trustingOnly10.ClientIP(r); got != "1.2.3.4" {
		t.Errorf("trusted XFF: got %q want %q", got, "1.2.3.4")
	}

	// Same XFF, untrusted peer → fall back to RemoteAddr.
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.RemoteAddr = "8.8.8.8:9000"
	r2.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := trustingOnly10.ClientIP(r2); got != "8.8.8.8" {
		t.Errorf("untrusted XFF: got %q want %q", got, "8.8.8.8")
	}
}

func TestProxyTrustIsHTTPS(t *testing.T) {
	pt := mustTrust(t, "10.0.0.0/8")

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:80"
	r.Header.Set("X-Forwarded-Proto", "https")
	if !pt.IsHTTPS(r) {
		t.Fatal("trusted XFP=https should be true")
	}

	// Same header, untrusted peer.
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.RemoteAddr = "8.8.8.8:80"
	r2.Header.Set("X-Forwarded-Proto", "https")
	if pt.IsHTTPS(r2) {
		t.Fatal("untrusted XFP=https must be ignored")
	}
}

func TestProxyTrustMiddlewareRewritesRemoteAddr(t *testing.T) {
	pt := mustTrust(t, "127.0.0.0/8")

	var sawRemote string
	h := pt.Middleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		sawRemote = req.RemoteAddr
	}))

	// Trusted peer + XFF → rewritten.
	r1 := httptest.NewRequest(http.MethodGet, "/", nil)
	r1.RemoteAddr = "127.0.0.1:55001"
	r1.Header.Set("X-Forwarded-For", "9.9.9.9")
	h.ServeHTTP(httptest.NewRecorder(), r1)
	if sawRemote != "9.9.9.9:55001" {
		t.Errorf("trusted rewrite: got %q want %q", sawRemote, "9.9.9.9:55001")
	}

	// Untrusted peer + XFF → unchanged.
	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.RemoteAddr = "8.8.8.8:55001"
	r2.Header.Set("X-Forwarded-For", "9.9.9.9")
	h.ServeHTTP(httptest.NewRecorder(), r2)
	if sawRemote != "8.8.8.8:55001" {
		t.Errorf("untrusted preserve: got %q want %q", sawRemote, "8.8.8.8:55001")
	}
}

func mustTrust(t *testing.T, cidrs ...string) *ProxyTrust {
	t.Helper()
	pt, err := NewProxyTrust(cidrs)
	if err != nil {
		t.Fatalf("new trust: %v", err)
	}
	return pt
}
