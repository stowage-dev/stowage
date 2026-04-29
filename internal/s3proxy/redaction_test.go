// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// TestLogs_NoSecrets asserts that nothing in the proxy's log path leaks
// SigV4 signatures or secret keys. The request is constructed with a
// known-secret AKID and the test captures structured log output.
func TestLogs_NoSecrets(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()

	vc := &VirtualCredential{
		AccessKeyID:     "AKIALEAKCANARY00000A",
		SecretAccessKey: "SUPERSECRETCANARYVALUEDONOTLEAK123456ABCD",
		BackendName:     "primary",
		BucketScopes: []BucketScope{
			{BucketName: "leak-canary", BackendName: "primary"},
		},
	}

	// Build a logr.Logger that captures into an in-memory buffer using
	// logr/funcr — no third-party logging deps needed.
	buf := &bytes.Buffer{}
	logger := funcr.New(func(prefix, args string) {
		fmt.Fprintln(buf, prefix, args)
	}, funcr.Options{LogCaller: funcr.None})

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
		Log:           logger,
		HostSuffixes:  nil,
		BucketCreated: time.Now(),
		AdminCredsOverride: func(_ context.Context, _ BackendSpec) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "admin", SecretAccessKey: "secret"}, nil
		},
	})

	ts := httptest.NewServer(srv)
	defer ts.Close()

	u, _ := url.Parse(ts.URL)
	req, _ := http.NewRequest(http.MethodGet,
		fmt.Sprintf("%s/%s/key?X-Amz-Signature=deadbeefSECRETSIGNATURE", ts.URL, vc.BucketScopes[0].BucketName),
		nil)
	req.Host = u.Host
	signVirtual(t, req, vc.AccessKeyID, vc.SecretAccessKey, nil)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()

	logs := buf.String()
	require.NotContains(t, logs, vc.SecretAccessKey, "secret access key leaked into logs")
	require.NotContains(t, logs, "deadbeefSECRETSIGNATURE", "query-string signature leaked into logs")
	require.NotContains(t, logs, "AWS4-HMAC-SHA256 Credential", "Authorization header leaked into logs")
}

var _ logr.Logger = logr.Discard()
