// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"sync"

	"golang.org/x/time/rate"
)

// Limiter pairs a global token bucket with per-access-key token buckets.
// Zero values mean unlimited.
type Limiter struct {
	global *rate.Limiter

	mu       sync.Mutex
	perKey   map[string]*rate.Limiter
	perRate  float64
	perBurst int
}

// NewLimiter constructs a Limiter. globalRPS/perKeyRPS of 0 mean unlimited.
func NewLimiter(globalRPS float64, perKeyRPS float64) *Limiter {
	var g *rate.Limiter
	if globalRPS > 0 {
		g = rate.NewLimiter(rate.Limit(globalRPS), int(globalRPS*2)+1)
	}
	return &Limiter{
		global:   g,
		perKey:   map[string]*rate.Limiter{},
		perRate:  perKeyRPS,
		perBurst: int(perKeyRPS*2) + 1,
	}
}

// Allow consumes one token. Returns false when the caller should be rejected.
func (l *Limiter) Allow(accessKeyID string) bool {
	if l.global != nil && !l.global.Allow() {
		return false
	}
	if l.perRate <= 0 {
		return true
	}

	l.mu.Lock()
	lim, ok := l.perKey[accessKeyID]
	if !ok {
		lim = rate.NewLimiter(rate.Limit(l.perRate), l.perBurst)
		l.perKey[accessKeyID] = lim
	}
	l.mu.Unlock()
	return lim.Allow()
}
