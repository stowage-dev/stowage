// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package proxyurl

import "testing"

func TestResolve(t *testing.T) {
	const clusterFallback = "http://stowage-proxy.stowage-system.svc.cluster.local:8080"

	cases := []struct {
		name       string
		public     string
		fallback   string
		bucket     string
		pathStyle  bool
		wantResult string
	}{
		{
			name:       "public_hostname base",
			public:     "s3.example.com",
			fallback:   clusterFallback,
			wantResult: "https://s3.example.com",
		},
		{
			name:       "public_hostname path-style bucket",
			public:     "s3.example.com",
			fallback:   clusterFallback,
			bucket:     "mybucket",
			pathStyle:  true,
			wantResult: "https://s3.example.com/mybucket",
		},
		{
			name:       "public_hostname virtual-hosted bucket",
			public:     "s3.example.com",
			fallback:   clusterFallback,
			bucket:     "mybucket",
			pathStyle:  false,
			wantResult: "https://mybucket.s3.example.com",
		},
		{
			name:       "public_hostname with port",
			public:     "s3.example.com:8443",
			bucket:     "b",
			pathStyle:  true,
			wantResult: "https://s3.example.com:8443/b",
		},
		{
			name:       "fallback used when no public_hostname (base)",
			fallback:   clusterFallback,
			wantResult: clusterFallback,
		},
		{
			name:       "fallback used path-style bucket",
			fallback:   clusterFallback,
			bucket:     "mybucket",
			pathStyle:  true,
			wantResult: "http://stowage-proxy.stowage-system.svc.cluster.local:8080/mybucket",
		},
		{
			name:       "fallback used virtual-hosted bucket",
			fallback:   clusterFallback,
			bucket:     "mybucket",
			pathStyle:  false,
			wantResult: "http://mybucket.stowage-proxy.stowage-system.svc.cluster.local:8080",
		},
		{
			name:       "public_hostname wins over fallback even when fallback is https",
			public:     "s3.example.com",
			fallback:   "https://internal.example.com",
			wantResult: "https://s3.example.com",
		},
		{
			name:       "neither set returns empty",
			wantResult: "",
		},
		{
			name:       "unparseable fallback returns empty",
			fallback:   "::not-a-url::",
			wantResult: "",
		},
		{
			name:       "whitespace-only public_hostname treated as unset",
			public:     "   ",
			fallback:   clusterFallback,
			wantResult: clusterFallback,
		},
		{
			name:       "fallback with sub-path preserved as base",
			fallback:   "https://proxy.example.com/s3",
			wantResult: "https://proxy.example.com/s3",
		},
		{
			name:       "fallback with sub-path + path-style bucket",
			fallback:   "https://proxy.example.com/s3",
			bucket:     "mybucket",
			pathStyle:  true,
			wantResult: "https://proxy.example.com/s3/mybucket",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Resolve(tc.public, tc.fallback, tc.bucket, tc.pathStyle)
			if got != tc.wantResult {
				t.Fatalf("Resolve(%q,%q,%q,%v) = %q, want %q",
					tc.public, tc.fallback, tc.bucket, tc.pathStyle, got, tc.wantResult)
			}
		})
	}
}
