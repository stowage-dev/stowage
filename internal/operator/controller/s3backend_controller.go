// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package controller holds the reconcilers for the operator CRDs.
package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	brokerv1a1 "github.com/stowage-dev/stowage/internal/operator/api/v1alpha1"
	"github.com/stowage-dev/stowage/internal/operator/backend"
	"github.com/stowage-dev/stowage/internal/operator/credentials"
)

// S3BackendReconciler probes the backend for reachability and validates the
// configured bucket name template.
type S3BackendReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Resolver *credentials.Resolver
}

// +kubebuilder:rbac:groups=broker.stowage.io,resources=s3backends,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=broker.stowage.io,resources=s3backends/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

func (r *S3BackendReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var b brokerv1a1.S3Backend
	if err := r.Get(ctx, req.NamespacedName, &b); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if err := backend.ValidateTemplate(b.Spec.BucketNameTemplate); err != nil {
		logger.Info("invalid bucket name template", "err", err)
		return r.setReady(ctx, &b, metav1.ConditionFalse, brokerv1a1.ReasonTemplateInvalid, err.Error(), 0)
	}

	admin, err := r.Resolver.Resolve(ctx, credentials.AdminSecretRef{
		Name:           b.Spec.AdminCredentialsSecretRef.Name,
		Namespace:      b.Spec.AdminCredentialsSecretRef.Namespace,
		AccessKeyField: b.Spec.AdminCredentialsSecretRef.AccessKeyField,
		SecretKeyField: b.Spec.AdminCredentialsSecretRef.SecretKeyField,
		StorageSpace:   b.Spec.QuObjectsStorageSpace,
	})
	if err != nil {
		logger.Info("admin credentials unavailable", "err", err)
		return r.setReady(ctx, &b, metav1.ConditionFalse, brokerv1a1.ReasonCredentialsInvalid, err.Error(), 30*time.Second)
	}

	caBundle, err := r.loadCABundle(ctx, b.Spec.TLS)
	if err != nil {
		return r.setReady(ctx, &b, metav1.ConditionFalse, brokerv1a1.ReasonCredentialsInvalid, err.Error(), 30*time.Second)
	}

	s3c, err := backend.NewClient(backend.Config{
		Endpoint:           b.Spec.Endpoint,
		Region:             b.Spec.Region,
		AccessKeyID:        admin.AccessKeyID,
		SecretAccessKey:    admin.SecretAccessKey,
		UsePathStyle:       b.Spec.AddressingStyle != brokerv1a1.AddressingStyleVirtual,
		InsecureSkipVerify: b.Spec.TLS != nil && b.Spec.TLS.InsecureSkipVerify,
		CABundle:           caBundle,
	})
	if err != nil {
		return r.setReady(ctx, &b, metav1.ConditionFalse, brokerv1a1.ReasonBackendError, err.Error(), 30*time.Second)
	}

	ops := &backend.Ops{S3: s3c}
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	names, err := ops.ListBuckets(probeCtx)
	if err != nil {
		return r.setReady(ctx, &b, metav1.ConditionFalse, brokerv1a1.ReasonEndpointUnreachable, err.Error(), 30*time.Second)
	}

	b.Status.BucketCount = int32(len(names))
	return r.setReady(ctx, &b, metav1.ConditionTrue, brokerv1a1.ReasonEndpointReachable, fmt.Sprintf("%d buckets visible", len(names)), 5*time.Minute)
}

func (r *S3BackendReconciler) loadCABundle(ctx context.Context, tls *brokerv1a1.TLSSpec) ([]byte, error) {
	if tls == nil || tls.CABundleSecretRef == nil {
		return nil, nil
	}
	var sec corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: tls.CABundleSecretRef.Namespace, Name: tls.CABundleSecretRef.Name}, &sec); err != nil {
		return nil, fmt.Errorf("get CA bundle secret: %w", err)
	}
	key := tls.CABundleSecretRef.Key
	if key == "" {
		key = "ca.crt"
	}
	b, ok := sec.Data[key]
	if !ok {
		return nil, fmt.Errorf("CA bundle secret missing key %q", key)
	}
	return b, nil
}

func (r *S3BackendReconciler) setReady(ctx context.Context, b *brokerv1a1.S3Backend, status metav1.ConditionStatus, reason, msg string, requeue time.Duration) (ctrl.Result, error) {
	b.Status.ObservedGeneration = b.Generation
	b.Status.Conditions = setCondition(b.Status.Conditions, metav1.Condition{
		Type:               brokerv1a1.ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: b.Generation,
	})
	if err := r.Status().Update(ctx, b); err != nil {
		return ctrl.Result{}, err
	}
	if requeue == 0 {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{RequeueAfter: requeue}, nil
}

// SetupWithManager wires watches. Admin Secret + CA Secret changes retrigger
// reconcile so credential rotation is observed.
func (r *S3BackendReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&brokerv1a1.S3Backend{}).
		Watches(&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.secretToBackend),
			builder.WithPredicates(),
		).
		Complete(r)
}

func (r *S3BackendReconciler) secretToBackend(ctx context.Context, obj client.Object) []ctrl.Request {
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
		if b.Spec.AdminCredentialsSecretRef.Namespace == sec.Namespace && b.Spec.AdminCredentialsSecretRef.Name == sec.Name {
			reqs = append(reqs, ctrl.Request{NamespacedName: types.NamespacedName{Name: b.Name}})
			continue
		}
		if b.Spec.TLS != nil && b.Spec.TLS.CABundleSecretRef != nil &&
			b.Spec.TLS.CABundleSecretRef.Namespace == sec.Namespace &&
			b.Spec.TLS.CABundleSecretRef.Name == sec.Name {
			reqs = append(reqs, ctrl.Request{NamespacedName: types.NamespacedName{Name: b.Name}})
		}
	}
	return reqs
}
