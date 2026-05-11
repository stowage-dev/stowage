// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/go-logr/logr/testr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// newCORSTestServer wraps newTestServer with a CORS config injected.
func newCORSTestServer(t *testing.T, ups *httptest.Server, vc *VirtualCredential, cors *CORSConfig) *httptest.Server {
	t.Helper()
	src := &fakeSource{
		byAKID: map[string]*VirtualCredential{vc.AccessKeyID: vc},
		byAnon: map[string]*AnonymousBinding{},
	}
	br := NewBackendResolver(&stubBackendLookup{endpointURL: ups.URL})
	srv := NewServer(Config{
		Source:        src,
		Backends:      br,
		Limiter:       NewLimiter(0, 0),
		IPLimiter:     NewIPLimiter(0),
		Metrics:       NewMetrics(prometheus.NewRegistry()),
		Log:           testr.New(t),
		HostSuffixes:  nil,
		BucketCreated: time.Now(),
		CORS:          cors,
		AdminCredsOverride: func(_ context.Context, _ BackendSpec) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "admin", SecretAccessKey: "secret"}, nil
		},
	})
	return httptest.NewServer(srv)
}

func TestCORS_PreflightAllowedOrigin(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newCORSTestServer(t, ups, newPostCred(), &CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
	})
	defer proxy.Close()

	req, _ := http.NewRequest(http.MethodOptions, proxy.URL+"/uploads", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, x-amz-date")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, "https://app.example.com", resp.Header.Get("Access-Control-Allow-Origin"))
	require.Contains(t, resp.Header.Get("Access-Control-Allow-Methods"), "POST")
	require.NotEmpty(t, resp.Header.Get("Access-Control-Allow-Headers"))
	require.NotEmpty(t, resp.Header.Get("Access-Control-Max-Age"))
	vary := strings.Join(resp.Header.Values("Vary"), ",")
	require.Contains(t, vary, "Origin")
}

func TestCORS_PreflightWildcardOrigin(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newCORSTestServer(t, ups, newPostCred(), &CORSConfig{
		AllowedOrigins: []string{"*"},
	})
	defer proxy.Close()

	req, _ := http.NewRequest(http.MethodOptions, proxy.URL+"/uploads", nil)
	req.Header.Set("Origin", "https://anywhere.example.org")
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, "*", resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestCORS_PreflightDisallowedOriginFallsThrough(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newCORSTestServer(t, ups, newPostCred(), &CORSConfig{
		AllowedOrigins: []string{"https://approved.example.com"},
	})
	defer proxy.Close()

	req, _ := http.NewRequest(http.MethodOptions, proxy.URL+"/uploads", nil)
	req.Header.Set("Origin", "https://malicious.example.org")
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Falls through to the regular dispatcher: OPTIONS isn't a classified
	// S3 op, so it returns 403 with no CORS headers.
	require.NotEqual(t, http.StatusNoContent, resp.StatusCode)
	require.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestCORS_PreflightMissingACRMFallsThrough(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newCORSTestServer(t, ups, newPostCred(), &CORSConfig{
		AllowedOrigins: []string{"*"},
	})
	defer proxy.Close()

	// OPTIONS with an Origin but no Access-Control-Request-Method header
	// isn't a preflight. Should fall through to the dispatcher.
	req, _ := http.NewRequest(http.MethodOptions, proxy.URL+"/uploads", nil)
	req.Header.Set("Origin", "https://app.example.com")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.NotEqual(t, http.StatusNoContent, resp.StatusCode)
}

func TestCORS_DecoratesActualResponses(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newCORSTestServer(t, ups, newPostCred(), &CORSConfig{
		AllowedOrigins: []string{"https://app.example.com"},
		ExposedHeaders: []string{"ETag", "x-custom"},
	})
	defer proxy.Close()

	form := buildSignedPostForm(t, testPostAKID, testPostSecret, testPostBucket,
		"k", []string{
			`{"bucket":"` + testPostBucket + `"}`,
			`{"key":"k"}`,
		},
		nil,
		[]byte("data"))
	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/"+testPostBucket, form.body)
	req.Header.Set("Content-Type", form.contentType)
	req.Header.Set("Origin", "https://app.example.com")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Equal(t, "https://app.example.com", resp.Header.Get("Access-Control-Allow-Origin"))
	require.Equal(t, "ETag, x-custom", resp.Header.Get("Access-Control-Expose-Headers"))
}

func TestCORS_DisabledByDefault(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newTestServer(t, ups, newPostCred()) // no CORS config
	defer proxy.Close()

	// Preflight without CORS config: falls through to dispatcher.
	req, _ := http.NewRequest(http.MethodOptions, proxy.URL+"/uploads", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestCORS_AllowCredentials(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newCORSTestServer(t, ups, newPostCred(), &CORSConfig{
		AllowedOrigins:   []string{"https://app.example.com"},
		AllowCredentials: true,
	})
	defer proxy.Close()

	req, _ := http.NewRequest(http.MethodOptions, proxy.URL+"/uploads", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, "true", resp.Header.Get("Access-Control-Allow-Credentials"))
}

func TestCORS_AllowCredentialsSuppressedForWildcard(t *testing.T) {
	// Browsers reject credentials with wildcard origin; we suppress the
	// header even if the operator set AllowCredentials=true.
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newCORSTestServer(t, ups, newPostCred(), &CORSConfig{
		AllowedOrigins:   []string{"*"},
		AllowCredentials: true,
	})
	defer proxy.Close()

	req, _ := http.NewRequest(http.MethodOptions, proxy.URL+"/uploads", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Empty(t, resp.Header.Get("Access-Control-Allow-Credentials"))
}

func TestCORS_AllowedOriginHelper(t *testing.T) {
	cases := []struct {
		name   string
		cfg    *CORSConfig
		origin string
		want   string
	}{
		{"nil cfg", nil, "https://a.example.com", ""},
		{"empty origin", &CORSConfig{AllowedOrigins: []string{"*"}}, "", ""},
		{"wildcard match", &CORSConfig{AllowedOrigins: []string{"*"}}, "https://a.example.com", "*"},
		{"exact match", &CORSConfig{AllowedOrigins: []string{"https://a.example.com"}}, "https://a.example.com", "https://a.example.com"},
		{"port matters", &CORSConfig{AllowedOrigins: []string{"https://a.example.com"}}, "https://a.example.com:8443", ""},
		{"no match", &CORSConfig{AllowedOrigins: []string{"https://a.example.com"}}, "https://b.example.org", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, allowedOrigin(tc.cfg, tc.origin))
		})
	}
}
