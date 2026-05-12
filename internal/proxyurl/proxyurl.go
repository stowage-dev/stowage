// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package proxyurl resolves the URL clients use to reach the S3 proxy.
// Two consumers share the same logic: the operator stamps the result into
// the AWS_ENDPOINT_URL value of the consumer Secret it writes, and the
// dashboard API surfaces it on credential DTOs so the UI can show users
// the address their bucket lives at.
package proxyurl

import (
	"net/url"
	"strings"
)

// Resolve returns a proxy URL. publicHostname is the operator-configured
// bare hostname override (s3_proxy.public_hostname). When non-empty it
// wins outright and the scheme defaults to https — a public hostname is
// assumed to be externally reachable over TLS. When empty, fallback is
// parsed and reused verbatim (typically the cluster-internal
// operator.proxy_url, http://stowage-proxy.svc.cluster.local:8080).
//
// bucket controls the addressing applied:
//   - "" returns the base endpoint (what AWS SDKs expect in
//     AWS_ENDPOINT_URL; the SDK then prepends the bucket per its own
//     addressing-style setting).
//   - non-empty + pathStyle=true puts the bucket in the path:
//     "https://s3.example.com/mybucket".
//   - non-empty + pathStyle=false prepends it as a subdomain:
//     "https://mybucket.s3.example.com".
//
// Returns "" when neither publicHostname nor a parseable fallback is
// available — callers should treat that as "no public URL configured"
// and omit the field from whatever they're rendering.
func Resolve(publicHostname, fallback, bucket string, pathStyle bool) string {
	scheme, host, basePath := schemeAndHost(publicHostname, fallback)
	if host == "" {
		return ""
	}

	if bucket == "" {
		return assemble(scheme, host, basePath)
	}
	if pathStyle {
		return assemble(scheme, host, joinPath(basePath, bucket))
	}
	return assemble(scheme, bucket+"."+host, basePath)
}

// schemeAndHost picks the scheme/host pair to build the URL from.
// publicHostname wins when set; otherwise fallback is parsed for both.
// The returned basePath carries any path prefix the fallback URL had
// (rare — a proxy mounted under a sub-path), so a bucket appended later
// lands at the right place.
func schemeAndHost(publicHostname, fallback string) (scheme, host, basePath string) {
	if h := strings.TrimSpace(publicHostname); h != "" {
		// public_hostname is a bare host (validated at config load).
		// Default to https — operators set this for external/TLS access.
		return "https", h, ""
	}
	if fallback == "" {
		return "", "", ""
	}
	u, err := url.Parse(fallback)
	if err != nil || u.Host == "" {
		return "", "", ""
	}
	return u.Scheme, u.Host, strings.TrimRight(u.Path, "/")
}

func assemble(scheme, host, path string) string {
	u := url.URL{Scheme: scheme, Host: host, Path: path}
	return u.String()
}

func joinPath(base, segment string) string {
	if base == "" {
		return "/" + segment
	}
	return base + "/" + segment
}
