// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package vcstore

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Reader is an informer-backed lookup table the proxy uses to resolve an
// inbound access_key_id to a virtual credential. Internally keyed by AKID;
// rebuilt from the cache on every Secret add/update/delete.
type Reader struct {
	cache  cache.Cache
	ns     string
	mu     sync.RWMutex
	byAKID map[string]*VirtualCredential
}

// NewReader wires a new Reader to the given manager's cache and starts the
// informer. ctx controls lifetime.
func NewReader(mgr manager.Manager, namespace string) (*Reader, error) {
	r := &Reader{
		cache:  mgr.GetCache(),
		ns:     namespace,
		byAKID: map[string]*VirtualCredential{},
	}
	return r, nil
}

// Start registers the informer event handler. Must be called from a Runnable
// added to the manager so it runs after the cache has started.
func (r *Reader) Start(ctx context.Context) error {
	inf, err := r.cache.GetInformer(ctx, &corev1.Secret{})
	if err != nil {
		return fmt.Errorf("get secret informer: %w", err)
	}
	logger := log.FromContext(ctx).WithName("vcstore.reader")
	_, err = inf.AddEventHandler(newHandler(r, logger))
	if err != nil {
		return fmt.Errorf("add handler: %w", err)
	}
	return r.primeFromCache(ctx)
}

func (r *Reader) primeFromCache(ctx context.Context) error {
	var list corev1.SecretList
	if err := r.cache.List(ctx, &list,
		client.InNamespace(r.ns),
		client.MatchingLabels{LabelRole: RoleVirtualCredential},
	); err != nil {
		return fmt.Errorf("prime vcstore cache: %w", err)
	}
	for i := range list.Items {
		r.upsert(&list.Items[i])
	}
	return nil
}

// Lookup returns the credential bound to an access key id, or false.
func (r *Reader) Lookup(accessKeyID string) (*VirtualCredential, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	vc, ok := r.byAKID[accessKeyID]
	if !ok {
		return nil, false
	}
	if vc.ExpiresAt != nil && time.Now().After(*vc.ExpiresAt) {
		return nil, false
	}
	out := *vc
	return &out, true
}

// Size returns the number of active credentials. Used for metrics.
func (r *Reader) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byAKID)
}

func (r *Reader) upsert(s *corev1.Secret) {
	if s.Labels[LabelRole] != RoleVirtualCredential {
		return
	}
	vc := secretToCredential(s)
	if vc == nil {
		return
	}
	r.mu.Lock()
	r.byAKID[vc.AccessKeyID] = vc
	r.mu.Unlock()
}

func (r *Reader) delete(s *corev1.Secret) {
	akid := s.Labels[LabelAccessKeyID]
	if akid == "" {
		akid = string(s.Data[DataAccessKeyID])
	}
	if akid == "" {
		return
	}
	r.mu.Lock()
	delete(r.byAKID, akid)
	r.mu.Unlock()
}

func secretToCredential(s *corev1.Secret) *VirtualCredential {
	akid := string(s.Data[DataAccessKeyID])
	sk := string(s.Data[DataSecretAccessKey])
	bucket := string(s.Data[DataBucketName])
	if akid == "" || sk == "" || bucket == "" {
		return nil
	}
	vc := &VirtualCredential{
		AccessKeyID:     akid,
		SecretAccessKey: sk,
		BucketName:      bucket,
		ClaimNamespace:  s.Labels[LabelClaimNS],
		ClaimName:       s.Labels[LabelClaimName],
		ClaimUID:        string(s.Data[DataClaimUID]),
		BackendName:     string(s.Data[DataBackend]),
	}
	// If bucket_scopes is present, it is authoritative for the credential's
	// scope set. Otherwise Normalize() below synthesises a single-element
	// []BucketScope from the legacy bucket_name + backend fields so downstream
	// consumers can always do set-membership without special-casing.
	if raw, ok := s.Data[DataBucketScopes]; ok && len(raw) > 0 {
		var scopes []BucketScope
		if err := json.Unmarshal(raw, &scopes); err == nil && len(scopes) > 0 {
			vc.BucketScopes = scopes
		}
	}
	vc.Normalize()
	if exp, ok := s.Annotations[AnnotationExpiresAt]; ok {
		if t, err := time.Parse(time.RFC3339, exp); err == nil {
			vc.ExpiresAt = &t
		}
	}
	return vc
}
