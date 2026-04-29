// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"net"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/time/rate"
)

// IPLimiter is a per-source-IP token-bucket limiter used by the anonymous
// path. Zero perIPRPS means unlimited.
type IPLimiter struct {
	mu      sync.Mutex
	perIP   map[string]*rate.Limiter
	perRate float64
	burst   int
}

// NewIPLimiter constructs an IPLimiter with the given per-IP RPS. perIPRPS
// of 0 disables limiting.
func NewIPLimiter(perIPRPS float64) *IPLimiter {
	return &IPLimiter{
		perIP:   map[string]*rate.Limiter{},
		perRate: perIPRPS,
		burst:   int(perIPRPS*2) + 1,
	}
}

// Allow consumes one token for the given client IP. Returns false when the
// caller should be rejected.
func (l *IPLimiter) Allow(ip string) bool {
	if l == nil || l.perRate <= 0 {
		return true
	}
	l.mu.Lock()
	lim, ok := l.perIP[ip]
	if !ok {
		lim = rate.NewLimiter(rate.Limit(l.perRate), l.burst)
		l.perIP[ip] = lim
	}
	l.mu.Unlock()
	return lim.Allow()
}

// AllowAt is like Allow but uses an explicit per-IP rate, falling back to the
// limiter's default when bindingRPS == 0.
func (l *IPLimiter) AllowAt(ip string, bindingRPS float64) bool {
	if l == nil {
		return true
	}
	r := bindingRPS
	if r <= 0 {
		r = l.perRate
	}
	if r <= 0 {
		return true
	}
	l.mu.Lock()
	lim, ok := l.perIP[ip]
	if !ok || lim.Limit() != rate.Limit(r) {
		lim = rate.NewLimiter(rate.Limit(r), int(r*2)+1)
		l.perIP[ip] = lim
	}
	l.mu.Unlock()
	return lim.Allow()
}

// ClientIP extracts the source IP for an inbound request. trustedProxies is a
// list of CIDRs whose X-Forwarded-For values we honor; when nil we trust XFF
// unconditionally (the deployment is expected to terminate at a trusted ingress
// in that case).
func ClientIP(r *http.Request, trustedProxies []*net.IPNet) string {
	remote := remoteIP(r.RemoteAddr)
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return remote
	}
	if trustedProxies == nil || ipIsTrusted(remote, trustedProxies) {
		// Take the left-most XFF entry — the original client per RFC 7239.
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	return remote
}

func remoteIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func ipIsTrusted(ip string, cidrs []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range cidrs {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

// ParseCIDRs parses a list of CIDR strings into *net.IPNet. Returns nil when
// the input list is empty (caller treats nil as "trust all", matching the
// install-time default).
func ParseCIDRs(in []string) ([]*net.IPNet, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]*net.IPNet, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}
