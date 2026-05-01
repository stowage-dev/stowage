// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/stowage-dev/stowage/internal/backend"
	"github.com/stowage-dev/stowage/internal/backend/s3v4"
	brokerv1a1 "github.com/stowage-dev/stowage/internal/operator/api/v1alpha1"
	"github.com/stowage-dev/stowage/internal/operator/credentials"
)

// RegistryReconciler mirrors S3Backend CRs into the in-process backend
// registry as read-only entries (Source: SourceK8s). It runs alongside
// S3BackendReconciler — the latter sets the CR's Ready status, while
// this one builds the s3v4 driver and updates the registry. They share
// the same cache, so a single S3Backend change triggers both queues
// without a second informer.
//
// Skipped (not registered with the manager) when the runtime did not
// provide a registry — i.e. headless `stowage operator` deployments
// where there is no admin UI to surface CRs into.
type RegistryReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Resolver *credentials.Resolver
	Registry *backend.Registry
}

// +kubebuilder:rbac:groups=broker.stowage.io,resources=s3backends,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

func (r *RegistryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("name", req.Name)

	var b brokerv1a1.S3Backend
	if err := r.Get(ctx, req.NamespacedName, &b); err != nil {
		if apierrors.IsNotFound(err) {
			r.unregisterIfK8sOwned(req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	admin, err := r.Resolver.Resolve(ctx, credentials.AdminSecretRef{
		Name:           b.Spec.AdminCredentialsSecretRef.Name,
		Namespace:      b.Spec.AdminCredentialsSecretRef.Namespace,
		AccessKeyField: b.Spec.AdminCredentialsSecretRef.AccessKeyField,
		SecretKeyField: b.Spec.AdminCredentialsSecretRef.SecretKeyField,
	})
	if err != nil {
		// Drop the entry until creds resolve — a registered backend with
		// stale creds would let admin handlers attempt operations against
		// keys that have already been rotated or revoked.
		r.unregisterIfK8sOwned(req.Name)
		logger.Info("admin credentials unavailable; dropped from registry", "err", err.Error())
		return ctrl.Result{}, err
	}

	driver, err := s3v4.New(ctx, s3v4.Config{
		ID:        b.Name,
		Name:      b.Name,
		Endpoint:  b.Spec.Endpoint,
		Region:    b.Spec.Region,
		AccessKey: admin.AccessKeyID,
		SecretKey: admin.SecretAccessKey,
		PathStyle: b.Spec.AddressingStyle != brokerv1a1.AddressingStyleVirtual,
	})
	if err != nil {
		r.unregisterIfK8sOwned(req.Name)
		return ctrl.Result{}, fmt.Errorf("build s3v4 driver: %w", err)
	}

	src, exists := r.Registry.Source(b.Name)
	switch {
	case !exists:
		if err := r.Registry.RegisterWithSource(driver, backend.SourceK8s); err != nil {
			return ctrl.Result{}, fmt.Errorf("register: %w", err)
		}
	case src == backend.SourceK8s:
		if err := r.Registry.Replace(b.Name, driver); err != nil {
			return ctrl.Result{}, fmt.Errorf("replace: %w", err)
		}
	default:
		// ID owned by config.yaml or the SQLite store. Don't shadow it; the
		// existing entry wins. Loud so the operator notices the conflict.
		logger.Info("S3Backend id collides with non-K8s registry entry; skipping",
			"id", b.Name, "owner", string(src))
	}
	return ctrl.Result{}, nil
}

func (r *RegistryReconciler) unregisterIfK8sOwned(id string) {
	if src, ok := r.Registry.Source(id); ok && src == backend.SourceK8s {
		_ = r.Registry.Unregister(id)
	}
}

// SetupWithManager wires watches. Admin-Secret events trigger reconciles
// of the referencing S3Backends so credential rotation is observed
// without waiting for the next periodic resync.
func (r *RegistryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("s3backend-registry").
		For(&brokerv1a1.S3Backend{}).
		Watches(&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.secretToBackend),
			builder.WithPredicates(),
		).
		Complete(r)
}

func (r *RegistryReconciler) secretToBackend(ctx context.Context, obj client.Object) []ctrl.Request {
	sec, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}
	var list brokerv1a1.S3BackendList
	if err := r.List(ctx, &list); err != nil {
		return nil
	}
	var reqs []ctrl.Request
	for i := range list.Items {
		b := &list.Items[i]
		if b.Spec.AdminCredentialsSecretRef.Namespace == sec.Namespace &&
			b.Spec.AdminCredentialsSecretRef.Name == sec.Name {
			reqs = append(reqs, ctrl.Request{NamespacedName: types.NamespacedName{Name: b.Name}})
		}
	}
	return reqs
}
