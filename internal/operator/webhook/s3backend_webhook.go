// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package webhook holds the admission validators for the operator CRDs.
// These run as part of the operator binary; see cmd/operator/main.go for the
// wiring and deploy/chart/templates/webhook.yaml for the cluster
// registration.
package webhook

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	brokerv1a1 "github.com/stowage-dev/stowage/internal/operator/api/v1alpha1"
	"github.com/stowage-dev/stowage/internal/operator/backend"
)

// S3BackendValidator validates S3Backend create/update operations.
type S3BackendValidator struct {
	// OpsNamespace is the namespace the proxy is scoped to for Secret reads.
	// adminCredentialsSecretRef.namespace must match. When empty, the namespace
	// check is skipped.
	OpsNamespace string
}

// SetupWithManager registers the validator with the manager's webhook server.
func (v *S3BackendValidator) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&brokerv1a1.S3Backend{}).
		WithValidator(v).
		Complete()
}

var _ admission.CustomValidator = &S3BackendValidator{}

func (v *S3BackendValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, v.validate(obj)
}

func (v *S3BackendValidator) ValidateUpdate(_ context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	return nil, v.validate(newObj)
}

func (v *S3BackendValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func (v *S3BackendValidator) validate(obj runtime.Object) error {
	b, ok := obj.(*brokerv1a1.S3Backend)
	if !ok {
		return fmt.Errorf("expected *S3Backend, got %T", obj)
	}
	if err := backend.ValidateTemplate(b.Spec.BucketNameTemplate); err != nil {
		return fmt.Errorf("spec.bucketNameTemplate: %w", err)
	}
	if v.OpsNamespace != "" && b.Spec.AdminCredentialsSecretRef.Namespace != v.OpsNamespace {
		return fmt.Errorf("spec.adminCredentialsSecretRef.namespace must be %q (the operator namespace)", v.OpsNamespace)
	}
	return nil
}

// Compile-time guard that the webhook builder signature is what we expect.
var _ webhook.CustomValidator = &S3BackendValidator{}
