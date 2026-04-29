// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package backend

import "net/url"

// ProxyTarget is the raw forwarding handle the S3 proxy needs to re-sign
// inbound tenant requests with admin credentials and dispatch them
// upstream. The Backend interface is intentionally high-level (Get/Put/List
// etc.); the proxy bypasses it because it must forward arbitrary S3 verbs,
// including operations the Backend interface does not model.
//
// SecretKey is the unsealed admin secret. It lives in process memory only —
// the registry holds it after sealed-secret hydration at boot.
type ProxyTarget struct {
	Endpoint  *url.URL
	Region    string
	PathStyle bool
	AccessKey string
	SecretKey string
}

// ProxyTargetProvider is implemented by drivers whose backing store can be
// reached via raw S3-API forwarding. The s3v4 driver implements it; in-memory
// test drivers do not.
type ProxyTargetProvider interface {
	ProxyTarget() (ProxyTarget, error)
}

// ProxyTarget returns the forwarding handle for a registered backend, or
// (zero, false) if no backend is registered under id, or if the registered
// backend's driver does not implement ProxyTargetProvider.
//
// This is the only seam between the proxy and the registry. Keeping it
// scoped to a sibling interface (rather than baking ProxyTarget() into
// Backend itself) avoids forcing every driver — including in-memory test
// stubs — to model raw-forward semantics they don't have.
func (r *Registry) ProxyTarget(id string) (ProxyTarget, bool, error) {
	b, ok := r.Get(id)
	if !ok {
		return ProxyTarget{}, false, nil
	}
	p, ok := b.(ProxyTargetProvider)
	if !ok {
		return ProxyTarget{}, false, nil
	}
	t, err := p.ProxyTarget()
	if err != nil {
		return ProxyTarget{}, true, err
	}
	return t, true, nil
}
