// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func newCred(akid, sk, bucket, backendName string) *VirtualCredential {
	return &VirtualCredential{
		AccessKeyID:     akid,
		SecretAccessKey: sk,
		BackendName:     backendName,
		BucketScopes:    []BucketScope{{BucketName: bucket, BackendName: backendName}},
	}
}

func TestProxy_PutAndGet(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()

	vc := newCred("AKIATESTCLAIM0000000",
		"secretsecretsecretsecretsecretsecretsecr",
		"my-app-uploads", "primary")
	proxy := newTestServer(t, ups, vc)
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.URL)

	// PUT
	body := []byte("hello world")
	putURL := fmt.Sprintf("%s/%s/%s", proxy.URL, vc.BucketScopes[0].BucketName, "greeting.txt")
	putReq, _ := http.NewRequest(http.MethodPut, putURL, bytes.NewReader(body))
	putReq.Host = proxyURL.Host
	putReq.ContentLength = int64(len(body))
	signVirtual(t, putReq, vc.AccessKeyID, vc.SecretAccessKey, body)

	resp, err := http.DefaultClient.Do(putReq)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, readAll(resp.Body))

	// GET
	getURL := fmt.Sprintf("%s/%s/%s", proxy.URL, vc.BucketScopes[0].BucketName, "greeting.txt")
	getReq, _ := http.NewRequest(http.MethodGet, getURL, nil)
	getReq.Host = proxyURL.Host
	signVirtual(t, getReq, vc.AccessKeyID, vc.SecretAccessKey, nil)

	resp2, err := http.DefaultClient.Do(getReq)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)
	require.Equal(t, string(body), readAll(resp2.Body))
}

func TestProxy_ScopeViolation(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()

	vc := newCred("AKIATESTCLAIM0000000",
		"secretsecretsecretsecretsecretsecretsecr",
		"my-app-uploads", "primary")
	proxy := newTestServer(t, ups, vc)
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.URL)
	otherBucket := "other-tenant-secret"
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/%s/key", proxy.URL, otherBucket), nil)
	req.Host = proxyURL.Host
	signVirtual(t, req, vc.AccessKeyID, vc.SecretAccessKey, nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	require.Contains(t, readAll(resp.Body), "AccessDenied")
}

func TestProxy_UnknownAccessKey(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()

	vc := newCred("AKIATESTCLAIM0000000",
		"secretsecretsecretsecretsecretsecretsecr",
		"my-app-uploads", "primary")
	proxy := newTestServer(t, ups, vc)
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.URL)
	req, _ := http.NewRequest(http.MethodGet, proxy.URL+"/my-app-uploads/key", nil)
	req.Host = proxyURL.Host
	signVirtual(t, req, "AKIADOESNOTEXIST0000", vc.SecretAccessKey, nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestProxy_ListBucketsSynthesized(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()

	vc := newCred("AKIATESTCLAIM0000000",
		"secretsecretsecretsecretsecretsecretsecr",
		"my-app-uploads", "primary")
	proxy := newTestServer(t, ups, vc)
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.URL)
	req, _ := http.NewRequest(http.MethodGet, proxy.URL+"/", nil)
	req.Host = proxyURL.Host
	signVirtual(t, req, vc.AccessKeyID, vc.SecretAccessKey, nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body := readAll(resp.Body)
	require.Contains(t, body, vc.BucketScopes[0].BucketName)
	require.NotContains(t, body, "other-bucket")
}

func TestShouldRecordAudit(t *testing.T) {
	cases := []struct {
		name   string
		method string
		result string
		rate   float64
		want   bool
	}{
		// All non-ok results recorded regardless of rate.
		{"denied always recorded", http.MethodGet, "denied", 0.0, true},
		{"error always recorded", http.MethodHead, "error", 0.0, true},
		{"unknown-akid always recorded", http.MethodGet, "unknown-akid", 0.0, true},
		// Writes always recorded regardless of rate.
		{"PUT always recorded", http.MethodPut, "ok", 0.0, true},
		{"DELETE always recorded", http.MethodDelete, "ok", 0.0, true},
		{"POST always recorded", http.MethodPost, "ok", 0.0, true},
		// Successful reads sampled.
		{"GET ok rate=0 dropped", http.MethodGet, "ok", 0.0, false},
		{"HEAD ok rate=0 dropped", http.MethodHead, "ok", 0.0, false},
		{"GET ok rate=1 recorded", http.MethodGet, "ok", 1.0, true},
		{"HEAD ok rate=1 recorded", http.MethodHead, "ok", 1.0, true},
		// Out-of-range clamps to the same behaviour as the boundary.
		{"GET ok rate=-1 dropped", http.MethodGet, "ok", -1.0, false},
		{"GET ok rate=2 recorded", http.MethodGet, "ok", 2.0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{cfg: Config{SuccessReadAuditRate: tc.rate}}
			r, _ := http.NewRequest(tc.method, "/bench/probe.bin", nil)
			got := s.shouldRecordAudit(r, servedRequest{result: tc.result})
			require.Equal(t, tc.want, got)
		})
	}
}
