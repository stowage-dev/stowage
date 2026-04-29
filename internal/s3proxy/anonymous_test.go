// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/go-logr/logr/testr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestAnonymous_GetAllowed(t *testing.T) {
	upstream := newUpstream()
	upstream.objs["my-app-uploads/public.txt"] = []byte("hello")
	ups := httptest.NewServer(upstream)
	defer ups.Close()

	anon := map[string]*AnonymousBinding{
		"my-app-uploads": {BucketName: "my-app-uploads", BackendName: "primary", Mode: "ReadOnly"},
	}
	proxy := newAnonTestServer(t, ups, anon, true)
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.URL)
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/%s/%s", proxy.URL, "my-app-uploads", "public.txt"), nil)
	req.Host = proxyURL.Host

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, "hello", string(body))
}

func TestAnonymous_PutDenied(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	anon := map[string]*AnonymousBinding{
		"my-app-uploads": {BucketName: "my-app-uploads", BackendName: "primary", Mode: "ReadOnly"},
	}
	proxy := newAnonTestServer(t, ups, anon, true)
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.URL)
	req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/%s/key", proxy.URL, "my-app-uploads"), strings.NewReader("x"))
	req.Host = proxyURL.Host
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestAnonymous_SubresourceDenied(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	anon := map[string]*AnonymousBinding{
		"my-app-uploads": {BucketName: "my-app-uploads", BackendName: "primary", Mode: "ReadOnly"},
	}
	proxy := newAnonTestServer(t, ups, anon, true)
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.URL)
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/%s/?acl", proxy.URL, "my-app-uploads"), nil)
	req.Host = proxyURL.Host
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestAnonymous_UnconfiguredBucketReturnsMissingAuth(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newAnonTestServer(t, ups, map[string]*AnonymousBinding{}, true)
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.URL)
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/%s/key", proxy.URL, "no-such-bucket"), nil)
	req.Host = proxyURL.Host
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(body), "MissingAuthenticationToken")
}

func TestAnonymous_DisabledClusterWide(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	anon := map[string]*AnonymousBinding{
		"my-app-uploads": {BucketName: "my-app-uploads", BackendName: "primary", Mode: "ReadOnly"},
	}
	proxy := newAnonTestServer(t, ups, anon, false)
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.URL)
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/%s/key", proxy.URL, "my-app-uploads"), nil)
	req.Host = proxyURL.Host
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestAnonymous_ServiceLevelDenied(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	anon := map[string]*AnonymousBinding{
		"my-app-uploads": {BucketName: "my-app-uploads", BackendName: "primary", Mode: "ReadOnly"},
	}
	proxy := newAnonTestServer(t, ups, anon, true)
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.URL)
	req, _ := http.NewRequest(http.MethodGet, proxy.URL+"/", nil)
	req.Host = proxyURL.Host
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestAnonymous_OpAllowedHelper(t *testing.T) {
	q := url.Values{}
	require.True(t, AnonymousOpAllowed("ReadOnly", "GetObject", q))
	require.True(t, AnonymousOpAllowed("ReadOnly", "ListObjects", q))
	require.True(t, AnonymousOpAllowed("ReadOnly", "HeadObject", q))
	require.True(t, AnonymousOpAllowed("ReadOnly", "HeadBucket", q))
	require.False(t, AnonymousOpAllowed("ReadOnly", "PutObject", q))
	require.False(t, AnonymousOpAllowed("ReadOnly", "DeleteObject", q))
	require.False(t, AnonymousOpAllowed("ReadOnly", "CreateMultipartUpload", q))
	require.False(t, AnonymousOpAllowed("None", "GetObject", q))
	require.False(t, AnonymousOpAllowed("ReadWrite", "GetObject", q))

	q.Set("acl", "")
	require.False(t, AnonymousOpAllowed("ReadOnly", "GetObject", q))
}

func TestAnonymous_OpAllowed_AllBlockedSubresources(t *testing.T) {
	for _, k := range anonymousBlockedSubresources {
		q := url.Values{}
		q.Set(k, "")
		require.False(t, AnonymousOpAllowed("ReadOnly", "GetObject", q),
			"sub-resource %q must block GetObject", k)
	}
}

func TestIsRequestUnauthenticated(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	require.True(t, IsRequestUnauthenticated(r))

	r2 := httptest.NewRequest(http.MethodGet, "/", nil)
	r2.Header.Set("Authorization", "AWS4-HMAC-SHA256 ...")
	require.False(t, IsRequestUnauthenticated(r2))

	r3 := httptest.NewRequest(http.MethodGet, "/?X-Amz-Signature=deadbeef", nil)
	require.False(t, IsRequestUnauthenticated(r3))
}

func TestAnonymous_HeadObjectAllowed(t *testing.T) {
	upstream := newUpstream()
	upstream.objs["my-app-uploads/public.txt"] = []byte("hello")
	ups := httptest.NewServer(upstream)
	defer ups.Close()

	anon := map[string]*AnonymousBinding{
		"my-app-uploads": {BucketName: "my-app-uploads", BackendName: "primary", Mode: "ReadOnly"},
	}
	proxy := newAnonTestServer(t, ups, anon, true)
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.URL)
	req, _ := http.NewRequest(http.MethodHead, fmt.Sprintf("%s/%s/%s", proxy.URL, "my-app-uploads", "public.txt"), nil)
	req.Host = proxyURL.Host
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestAnonymous_ListObjectsAllowed(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	anon := map[string]*AnonymousBinding{
		"my-app-uploads": {BucketName: "my-app-uploads", BackendName: "primary", Mode: "ReadOnly"},
	}
	proxy := newAnonTestServer(t, ups, anon, true)
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.URL)
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/%s/", proxy.URL, "my-app-uploads"), nil)
	req.Host = proxyURL.Host
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.NotEqual(t, http.StatusForbidden, resp.StatusCode,
		"ListObjects must pass the anonymous gate; upstream is responsible for body shape")
}

func TestAnonymous_DeleteDenied(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	anon := map[string]*AnonymousBinding{
		"my-app-uploads": {BucketName: "my-app-uploads", BackendName: "primary", Mode: "ReadOnly"},
	}
	proxy := newAnonTestServer(t, ups, anon, true)
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.URL)
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/%s/key", proxy.URL, "my-app-uploads"), nil)
	req.Host = proxyURL.Host
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestAnonymous_RateLimited(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	anon := map[string]*AnonymousBinding{
		"my-app-uploads": {
			BucketName:     "my-app-uploads",
			BackendName:    "primary",
			Mode:           "ReadOnly",
			PerSourceIPRPS: 1,
		},
	}
	src := &fakeSource{
		byAKID: map[string]*VirtualCredential{},
		byAnon: anon,
	}
	br := NewBackendResolver(&stubBackendLookup{endpointURL: ups.URL})
	srv := NewServer(Config{
		Source:           src,
		Backends:         br,
		Limiter:          NewLimiter(0, 0),
		IPLimiter:        NewIPLimiter(0),
		Metrics:          NewMetrics(prometheus.NewRegistry()),
		Log:              testr.New(t),
		BucketCreated:    time.Now(),
		AnonymousEnabled: true,
		AdminCredsOverride: func(_ context.Context, _ BackendSpec) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "admin", SecretAccessKey: "secret"}, nil
		},
	})
	proxy := httptest.NewServer(srv)
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.URL)

	// Burst = int(rate*2)+1 = 3 tokens at 1 RPS. Drain them, then assert 503.
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/%s/key", proxy.URL, "my-app-uploads"), nil)
		req.Host = proxyURL.Host
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
	}
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/%s/key", proxy.URL, "my-app-uploads"), nil)
	req.Host = proxyURL.Host
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.Contains(t, string(body), "SlowDown")
}

// TestAnonymous_MalformedAuthDoesNotFallThrough confirms a present-but-broken
// Authorization header keeps the request on the signed path so
// SignatureDoesNotMatch surfaces, instead of silently being treated as
// anonymous.
func TestAnonymous_MalformedAuthDoesNotFallThrough(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	anon := map[string]*AnonymousBinding{
		"my-app-uploads": {BucketName: "my-app-uploads", BackendName: "primary", Mode: "ReadOnly"},
	}
	proxy := newAnonTestServer(t, ups, anon, true)
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.URL)
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/%s/key", proxy.URL, "my-app-uploads"), nil)
	req.Host = proxyURL.Host
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 garbage-not-a-real-sigv4-header")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	require.NotContains(t, string(body), "<ListBucketResult")
}
