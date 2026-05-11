// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/go-logr/logr/testr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/stowage-dev/stowage/internal/backend"
)

// fakeSource is the Source implementation tests inject. It carries virtual
// credentials keyed by AKID and anonymous bindings keyed by bucket. Safe
// for concurrent reads.
type fakeSource struct {
	mu     sync.RWMutex
	byAKID map[string]*VirtualCredential
	byAnon map[string]*AnonymousBinding
}

func (f *fakeSource) Lookup(akid string) (*VirtualCredential, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	v, ok := f.byAKID[akid]
	if !ok {
		return nil, false
	}
	cp := *v
	return &cp, true
}

func (f *fakeSource) LookupAnon(bucket string) (*AnonymousBinding, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	a, ok := f.byAnon[strings.ToLower(bucket)]
	if !ok {
		return nil, false
	}
	cp := *a
	return &cp, true
}

func (f *fakeSource) Size() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.byAKID)
}

// stubBackendLookup makes every backend id resolve to the same upstream.
// The proxy doesn't use the AccessKey/SecretKey here because tests inject
// AdminCredsOverride to avoid signing against the fake upstream's lax
// handler.
type stubBackendLookup struct {
	endpointURL string
}

func (s *stubBackendLookup) ProxyTarget(name string) (backend.ProxyTarget, bool, error) {
	u, err := parseEndpoint(s.endpointURL)
	if err != nil {
		return backend.ProxyTarget{}, false, err
	}
	return backend.ProxyTarget{
		Endpoint:  u,
		Region:    "us-east-1",
		PathStyle: true,
		AccessKey: "admin",
		SecretKey: "secret",
	}, true, nil
}

func parseEndpoint(s string) (*url.URL, error) { return url.Parse(s) }

// upstream is a minimal S3-ish HTTP handler: ignores auth, returns stored
// object content, persists PUTs to an in-memory map. Enough for the
// integration smoke tests without pulling in gofakes3.
type upstream struct {
	mu   sync.Mutex
	objs map[string][]byte
}

func newUpstream() *upstream { return &upstream{objs: map[string][]byte{}} }

func (u *upstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/")
	switch r.Method {
	case http.MethodPut:
		b, _ := io.ReadAll(r.Body)
		u.mu.Lock()
		u.objs[key] = b
		u.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	case http.MethodGet, http.MethodHead:
		if key == "" {
			w.WriteHeader(http.StatusOK)
			return
		}
		u.mu.Lock()
		b, ok := u.objs[key]
		u.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(b)
		}
	case http.MethodDelete:
		u.mu.Lock()
		delete(u.objs, key)
		u.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// newTestServer builds a proxy server backed by a single virtual credential
// and the given upstream. Returns the test httpserver and a buffer the
// caller can read structured logs from (currently unused — logr/testr writes
// to the testing.T directly).
func newTestServer(t *testing.T, ups *httptest.Server, vc *VirtualCredential) *httptest.Server {
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
		AdminCredsOverride: func(_ context.Context, _ BackendSpec) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "admin", SecretAccessKey: "secret"}, nil
		},
	})
	return httptest.NewServer(srv)
}

// newTestProxyWithQuota mirrors newTestServer but lets a test inject a
// QuotaEnforcer. Used by post-object tests to exercise quota rejection
// without standing up the full quotas service.
func newTestProxyWithQuota(t *testing.T, ups *httptest.Server, vc *VirtualCredential, q QuotaEnforcer) *httptest.Server {
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
		Quotas:        q,
		AdminCredsOverride: func(_ context.Context, _ BackendSpec) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "admin", SecretAccessKey: "secret"}, nil
		},
	})
	return httptest.NewServer(srv)
}

// newAnonTestServer is newTestServer with the anonymous fast-path enabled
// and an empty credential map (only anonymous bindings matter).
func newAnonTestServer(t *testing.T, ups *httptest.Server, anon map[string]*AnonymousBinding, enabled bool) *httptest.Server {
	t.Helper()
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
		HostSuffixes:     nil,
		BucketCreated:    time.Now(),
		AnonymousEnabled: enabled,
		AdminCredsOverride: func(_ context.Context, _ BackendSpec) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "admin", SecretAccessKey: "secret"}, nil
		},
	})
	return httptest.NewServer(srv)
}

// signVirtual signs req with the supplied virtual credential, mirroring the
// behaviour of an AWS SDK client.
func signVirtual(t *testing.T, req *http.Request, akid, sk string, body []byte) {
	t.Helper()
	signer := v4.NewSigner()
	hash := sha256.Sum256(body)
	payload := hex.EncodeToString(hash[:])
	err := signer.SignHTTP(context.Background(),
		aws.Credentials{AccessKeyID: akid, SecretAccessKey: sk},
		req, payload, "s3", "us-east-1", time.Now().UTC(),
	)
	require.NoError(t, err)
	req.Header.Set("X-Amz-Content-Sha256", payload)
}

// readAll drains an io.Reader to a string. Used by happy-path tests.
func readAll(r io.Reader) string {
	b, _ := io.ReadAll(r)
	return string(b)
}
