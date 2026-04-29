// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// ProxyTrust decides whether to honour the X-Forwarded-For / X-Real-IP /
// X-Forwarded-Proto headers on an incoming request. When the configured
// CIDR list is empty the gate is open — every immediate peer is treated
// as a trusted proxy and forwarded headers are honoured. Operators that
// need to lock this down list the proxy's CIDR(s) in
// server.trusted_proxies; once the list is non-empty only peers within
// it are trusted.
//
// A nil receiver still trusts nobody so callers that haven't wired a
// ProxyTrust at all keep their previous semantics.
type ProxyTrust struct {
	cidrs []*net.IPNet
}

// NewProxyTrust parses the given CIDR list. An empty list yields a non-nil
// ProxyTrust that trusts every immediate peer — callers don't need to
// nil-check, and the default deployment "just works" behind a proxy
// without any extra config.
func NewProxyTrust(cidrs []string) (*ProxyTrust, error) {
	out := &ProxyTrust{}
	for _, s := range cidrs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		// Accept a bare IP as a /32 or /128 for ergonomics.
		if !strings.Contains(s, "/") {
			if ip := net.ParseIP(s); ip != nil {
				if ip.To4() != nil {
					s += "/32"
				} else {
					s += "/128"
				}
			}
		}
		_, ipnet, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("trusted_proxies: %q: %w", s, err)
		}
		out.cidrs = append(out.cidrs, ipnet)
	}
	return out, nil
}

// IsTrusted reports whether the given remote address (host:port or just
// host) is allowed to set forwarded headers. With no CIDRs configured
// every peer is trusted; with a non-empty list only peers inside it
// are. A nil receiver still trusts nobody, and parse errors fail closed.
func (p *ProxyTrust) IsTrusted(remoteAddr string) bool {
	if p == nil {
		return false
	}
	if len(p.cidrs) == 0 {
		return true
	}
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, c := range p.cidrs {
		if c.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP returns the originating client IP for r, honouring proxy headers
// only when the immediate peer (r.RemoteAddr) is in the trust list. The
// returned value is the bare IP — port stripped — to keep callers from
// re-implementing SplitHostPort.
//
// Order of precedence when the source is trusted:
//  1. leftmost X-Forwarded-For entry
//  2. X-Real-IP
//  3. RemoteAddr
//
// When the source is not trusted, only #3 is used.
func (p *ProxyTrust) ClientIP(r *http.Request) string {
	if p.IsTrusted(r.RemoteAddr) {
		if v := r.Header.Get("X-Forwarded-For"); v != "" {
			// leftmost wins
			if i := strings.IndexByte(v, ','); i >= 0 {
				return strings.TrimSpace(v[:i])
			}
			return strings.TrimSpace(v)
		}
		if v := r.Header.Get("X-Real-IP"); v != "" {
			return strings.TrimSpace(v)
		}
	}
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return h
	}
	return r.RemoteAddr
}

// IsHTTPS reports whether the request rode over TLS at the user agent. r.TLS
// is authoritative on direct TLS termination; otherwise we trust
// X-Forwarded-Proto only when the immediate peer is a configured proxy.
func (p *ProxyTrust) IsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if !p.IsTrusted(r.RemoteAddr) {
		return false
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// Middleware rewrites r.RemoteAddr to the trusted client IP so downstream
// middlewares (chi's RequestID logger, our rate limiters keyed on RemoteAddr,
// etc.) all see the same value. Replaces chi/middleware.RealIP, which trusts
// any X-Forwarded-* unconditionally.
func (p *ProxyTrust) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p.IsTrusted(r.RemoteAddr) {
			if ip := p.headerSourcedIP(r); ip != "" {
				// Preserve port if RemoteAddr had one — chi's logger
				// expects host:port shape.
				if _, port, err := net.SplitHostPort(r.RemoteAddr); err == nil {
					r.RemoteAddr = net.JoinHostPort(ip, port)
				} else {
					r.RemoteAddr = ip
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (p *ProxyTrust) headerSourcedIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := strings.IndexByte(v, ','); i >= 0 {
			return strings.TrimSpace(v[:i])
		}
		return strings.TrimSpace(v)
	}
	return strings.TrimSpace(r.Header.Get("X-Real-IP"))
}
