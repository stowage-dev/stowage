// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"context"
	"fmt"
	"net/url"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/stowage-dev/stowage/internal/backend"
)

// BackendSpec is the trimmed-down view of a backend the proxy needs at
// signing time. Region is consulted by signOutbound; AccessKey/SecretKey
// is the admin credential the proxy re-signs with.
type BackendSpec struct {
	Endpoint  *url.URL
	Region    string
	PathStyle bool

	AccessKey string
	SecretKey string
}

// BackendLookup is the seam between the proxy and stowage's backend
// registry. The production implementation wraps backend.Registry; tests
// inject in-memory stubs.
type BackendLookup interface {
	ProxyTarget(id string) (backend.ProxyTarget, bool, error)
}

// BackendResolver fronts the registry with a tiny per-call map lookup. The
// registry already holds unsealed admin credentials in memory, so we don't
// need a 5-minute TTL cache here — every call is O(1)
// against an atomic.Pointer snapshot.
type BackendResolver struct {
	Lookup BackendLookup
}

// NewBackendResolver wires a resolver to a backend registry.
func NewBackendResolver(l BackendLookup) *BackendResolver {
	return &BackendResolver{Lookup: l}
}

// Backend returns the parsed endpoint and spec for a named stowage backend.
func (b *BackendResolver) Backend(_ context.Context, name string) (*url.URL, BackendSpec, error) {
	t, found, err := b.Lookup.ProxyTarget(name)
	if err != nil {
		return nil, BackendSpec{}, fmt.Errorf("backend %q: %w", name, err)
	}
	if !found {
		return nil, BackendSpec{}, fmt.Errorf("backend %q: not registered or does not support proxy forwarding", name)
	}
	spec := BackendSpec{
		Endpoint:  t.Endpoint,
		Region:    t.Region,
		PathStyle: t.PathStyle,
		AccessKey: t.AccessKey,
		SecretKey: t.SecretKey,
	}
	return t.Endpoint, spec, nil
}

// AdminCreds returns the admin AWS credential the proxy uses to re-sign
// requests forwarded to a named backend.
func (b *BackendResolver) AdminCreds(ctx context.Context, name string) (aws.Credentials, BackendSpec, error) {
	_, spec, err := b.Backend(ctx, name)
	if err != nil {
		return aws.Credentials{}, BackendSpec{}, err
	}
	return aws.Credentials{
		AccessKeyID:     spec.AccessKey,
		SecretAccessKey: spec.SecretKey,
	}, spec, nil
}
