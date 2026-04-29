// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"net/http"
	"strings"
)

// RouteInfo describes what an inbound request is addressing.
type RouteInfo struct {
	// Bucket is empty when the request is a service-level op (ListBuckets).
	Bucket string
	// Key is the object key (may be empty for bucket-level ops).
	Key string
	// PathStyle is true when the bucket appeared in the path.
	PathStyle bool
}

// ClassifyRoute decodes path-style vs virtual-hosted requests and extracts
// the target bucket and object key.
//
// Virtual-hosted: "<bucket>.s3.example.com/key"
// Path-style:     "s3.example.com/<bucket>/key"
// Service-level:  "s3.example.com/"
func ClassifyRoute(r *http.Request, proxyHostSuffixes []string) RouteInfo {
	host := strings.ToLower(r.Host)
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}

	for _, suf := range proxyHostSuffixes {
		if suf == "" {
			continue
		}
		suf = "." + strings.TrimPrefix(strings.ToLower(suf), ".")
		if strings.HasSuffix(host, suf) && len(host) > len(suf) {
			bucket := host[:len(host)-len(suf)]
			return RouteInfo{Bucket: bucket, Key: strings.TrimPrefix(r.URL.Path, "/"), PathStyle: false}
		}
	}

	// Path-style: first segment is the bucket.
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p == "" {
		return RouteInfo{PathStyle: true}
	}
	slash := strings.IndexByte(p, '/')
	if slash < 0 {
		return RouteInfo{Bucket: p, PathStyle: true}
	}
	return RouteInfo{Bucket: p[:slash], Key: p[slash+1:], PathStyle: true}
}
