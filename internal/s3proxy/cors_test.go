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

// stubCORSSource serves a fixed bucket → rules map. Lookups for unknown
// buckets return (nil, false), exercising the proxy's fall-through path.
type stubCORSSource struct {
	byBucket map[string][]BucketCORSRule
}

func (s *stubCORSSource) LookupCORS(bucket string) ([]BucketCORSRule, bool) {
	if s == nil {
		return nil, false
	}
	r, ok := s.byBucket[strings.ToLower(bucket)]
	return r, ok
}

// newCORSTestServer wraps newTestServer with a per-bucket CORS source
// injected. cors keys are bucket names; rules apply to anything routed
// to that bucket (path-style or virtual-hosted).
func newCORSTestServer(t *testing.T, ups *httptest.Server, vc *VirtualCredential, cors map[string][]BucketCORSRule) *httptest.Server {
	t.Helper()
	src := &fakeSource{
		byAKID: map[string]*VirtualCredential{vc.AccessKeyID: vc},
		byAnon: map[string]*AnonymousBinding{},
	}
	br := NewBackendResolver(&stubBackendLookup{endpointURL: ups.URL})
	lower := make(map[string][]BucketCORSRule, len(cors))
	for k, v := range cors {
		lower[strings.ToLower(k)] = v
	}
	srv := NewServer(Config{
		Source:        src,
		Backends:      br,
		Limiter:       NewLimiter(0, 0),
		IPLimiter:     NewIPLimiter(0),
		Metrics:       NewMetrics(prometheus.NewRegistry()),
		Log:           testr.New(t),
		HostSuffixes:  nil,
		BucketCreated: time.Now(),
		CORSSource:    &stubCORSSource{byBucket: lower},
		AdminCredsOverride: func(_ context.Context, _ BackendSpec) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "admin", SecretAccessKey: "secret"}, nil
		},
	})
	return httptest.NewServer(srv)
}

func TestCORS_PreflightAllowedOrigin(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newCORSTestServer(t, ups, newPostCred(), map[string][]BucketCORSRule{
		"uploads": {{
			AllowedOrigins: []string{"https://app.example.com"},
			AllowedMethods: []string{"POST", "PUT"},
		}},
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
	proxy := newCORSTestServer(t, ups, newPostCred(), map[string][]BucketCORSRule{
		"uploads": {{
			AllowedOrigins: []string{"*"},
			AllowedMethods: []string{"POST"},
		}},
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
	proxy := newCORSTestServer(t, ups, newPostCred(), map[string][]BucketCORSRule{
		"uploads": {{
			AllowedOrigins: []string{"https://approved.example.com"},
			AllowedMethods: []string{"POST"},
		}},
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

func TestCORS_PreflightMethodMismatchFallsThrough(t *testing.T) {
	// A rule that allows POST but the preflight asks for DELETE — the
	// preflight should not be answered with success.
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newCORSTestServer(t, ups, newPostCred(), map[string][]BucketCORSRule{
		"uploads": {{
			AllowedOrigins: []string{"*"},
			AllowedMethods: []string{"POST"},
		}},
	})
	defer proxy.Close()

	req, _ := http.NewRequest(http.MethodOptions, proxy.URL+"/uploads", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "DELETE")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.NotEqual(t, http.StatusNoContent, resp.StatusCode)
}

func TestCORS_PreflightMissingACRMFallsThrough(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newCORSTestServer(t, ups, newPostCred(), map[string][]BucketCORSRule{
		"uploads": {{
			AllowedOrigins: []string{"*"},
			AllowedMethods: []string{"POST"},
		}},
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
	proxy := newCORSTestServer(t, ups, newPostCred(), map[string][]BucketCORSRule{
		testPostBucket: {{
			AllowedOrigins: []string{"https://app.example.com"},
			AllowedMethods: []string{"POST"},
			ExposeHeaders:  []string{"ETag", "x-custom"},
		}},
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
	proxy := newTestServer(t, ups, newPostCred()) // no CORSSource
	defer proxy.Close()

	// Preflight without a CORSSource: falls through to dispatcher.
	req, _ := http.NewRequest(http.MethodOptions, proxy.URL+"/uploads", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestCORS_UnknownBucketFallsThrough(t *testing.T) {
	// CORSSource configured but the inbound bucket has no rules.
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newCORSTestServer(t, ups, newPostCred(), map[string][]BucketCORSRule{
		"other-bucket": {{
			AllowedOrigins: []string{"*"},
			AllowedMethods: []string{"POST"},
		}},
	})
	defer proxy.Close()

	req, _ := http.NewRequest(http.MethodOptions, proxy.URL+"/uploads", nil)
	req.Header.Set("Origin", "https://app.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.NotEqual(t, http.StatusNoContent, resp.StatusCode)
	require.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestCORS_MatchHelper(t *testing.T) {
	rules := []BucketCORSRule{
		{AllowedOrigins: []string{"https://a.example.com"}, AllowedMethods: []string{"POST"}},
		{AllowedOrigins: []string{"*"}, AllowedMethods: []string{"GET"}},
	}
	cases := []struct {
		name    string
		rules   []BucketCORSRule
		origin  string
		method  string
		want    string
		hitRule int // index into rules, -1 = no match
	}{
		{"nil rules", nil, "https://a.example.com", "POST", "", -1},
		{"empty origin", rules, "", "POST", "", -1},
		{"exact origin + method", rules, "https://a.example.com", "POST", "https://a.example.com", 0},
		{"port matters", rules, "https://a.example.com:8443", "POST", "", -1},
		{"wildcard rule matches any origin", rules, "https://x.example.org", "GET", "*", 1},
		{"method mismatch picks no rule", rules, "https://a.example.com", "GET", "*", 1},
		{"no match", rules, "https://b.example.org", "DELETE", "", -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule, allow := matchCORSRule(tc.rules, tc.origin, tc.method)
			require.Equal(t, tc.want, allow)
			if tc.hitRule < 0 {
				require.Nil(t, rule)
			} else {
				require.NotNil(t, rule)
				require.Equal(t, tc.rules[tc.hitRule].AllowedMethods, rule.AllowedMethods)
			}
		})
	}
}
