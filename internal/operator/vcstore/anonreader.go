// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package vcstore

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// AnonReader is an informer-backed lookup table the proxy uses to resolve an
// inbound bucket name to an anonymous-access binding. Bucket names are stored
// lower-cased so route matching is case-insensitive (matching S3 bucket-name
// rules).
type AnonReader struct {
	cache    ctrlcache.Cache
	ns       string
	mu       sync.RWMutex
	byBucket map[string]*AnonymousBinding
}

// NewAnonReader wires an AnonReader to the given manager's cache.
func NewAnonReader(mgr manager.Manager, namespace string) (*AnonReader, error) {
	return &AnonReader{
		cache:    mgr.GetCache(),
		ns:       namespace,
		byBucket: map[string]*AnonymousBinding{},
	}, nil
}

// Start registers the informer event handler. Must be called from a Runnable
// added to the manager so it runs after the cache has started.
func (r *AnonReader) Start(ctx context.Context) error {
	inf, err := r.cache.GetInformer(ctx, &corev1.Secret{})
	if err != nil {
		return fmt.Errorf("get secret informer: %w", err)
	}
	logger := log.FromContext(ctx).WithName("vcstore.anonreader")
	_, err = inf.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if s, ok := obj.(*corev1.Secret); ok {
				r.upsert(s)
			}
		},
		UpdateFunc: func(_, newObj interface{}) {
			if s, ok := newObj.(*corev1.Secret); ok {
				r.upsert(s)
			}
		},
		DeleteFunc: func(obj interface{}) {
			s, ok := obj.(*corev1.Secret)
			if !ok {
				if t, ok := obj.(cache.DeletedFinalStateUnknown); ok {
					if s2, ok := t.Obj.(*corev1.Secret); ok {
						r.delete(s2)
					}
				}
				return
			}
			r.delete(s)
		},
	})
	if err != nil {
		return fmt.Errorf("add anon handler: %w", err)
	}
	logger.V(1).Info("anonreader started")
	return r.primeFromCache(ctx)
}

func (r *AnonReader) primeFromCache(ctx context.Context) error {
	var list corev1.SecretList
	if err := r.cache.List(ctx, &list,
		client.InNamespace(r.ns),
		client.MatchingLabels{LabelRole: RoleAnonymousBinding},
	); err != nil {
		return fmt.Errorf("prime anonreader cache: %w", err)
	}
	for i := range list.Items {
		r.upsert(&list.Items[i])
	}
	return nil
}

// Lookup returns the binding for a bucket name, or false. Bucket lookups are
// case-insensitive.
func (r *AnonReader) Lookup(bucket string) (*AnonymousBinding, bool) {
	if bucket == "" {
		return nil, false
	}
	key := strings.ToLower(bucket)
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.byBucket[key]
	if !ok {
		return nil, false
	}
	out := *b
	return &out, true
}

// Size returns the number of registered anonymous bindings. Used for metrics.
func (r *AnonReader) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byBucket)
}

func (r *AnonReader) upsert(s *corev1.Secret) {
	if s.Labels[LabelRole] != RoleAnonymousBinding {
		return
	}
	bucket := string(s.Data[DataBucketName])
	backend := string(s.Data[DataBackend])
	mode := string(s.Data[DataAnonMode])
	if bucket == "" || backend == "" || mode == "" {
		return
	}
	rps := int32(0)
	if raw := string(s.Data[DataAnonRPS]); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 32); err == nil {
			rps = int32(n)
		}
	}
	b := &AnonymousBinding{
		BucketName:     bucket,
		BackendName:    backend,
		Mode:           mode,
		PerSourceIPRPS: rps,
		ClaimNamespace: s.Labels[LabelClaimNS],
		ClaimName:      s.Labels[LabelClaimName],
		ClaimUID:       string(s.Data[DataClaimUID]),
	}
	r.mu.Lock()
	r.byBucket[strings.ToLower(bucket)] = b
	r.mu.Unlock()
}

func (r *AnonReader) delete(s *corev1.Secret) {
	bucket := s.Labels[LabelBucketName]
	if bucket == "" {
		bucket = string(s.Data[DataBucketName])
	}
	if bucket == "" {
		return
	}
	r.mu.Lock()
	delete(r.byBucket, strings.ToLower(bucket))
	r.mu.Unlock()
}
