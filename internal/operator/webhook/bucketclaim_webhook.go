// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package webhook

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	brokerv1a1 "github.com/stowage-dev/stowage/internal/operator/api/v1alpha1"
)

// BucketClaimValidator validates BucketClaim create/update operations. It
// enforces cross-field rules that CEL on the CRD would struggle to express
// cleanly, and defends the immutable fields against update.
type BucketClaimValidator struct{}

func (v *BucketClaimValidator) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&brokerv1a1.BucketClaim{}).
		WithValidator(v).
		Complete()
}

var _ admission.CustomValidator = &BucketClaimValidator{}

func (v *BucketClaimValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	c, ok := obj.(*brokerv1a1.BucketClaim)
	if !ok {
		return nil, fmt.Errorf("expected *BucketClaim, got %T", obj)
	}
	return nil, validateCommon(c)
}

func (v *BucketClaimValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldC, ok := oldObj.(*brokerv1a1.BucketClaim)
	if !ok {
		return nil, fmt.Errorf("expected *BucketClaim, got %T", oldObj)
	}
	newC, ok := newObj.(*brokerv1a1.BucketClaim)
	if !ok {
		return nil, fmt.Errorf("expected *BucketClaim, got %T", newObj)
	}

	if oldC.Spec.BucketName != newC.Spec.BucketName {
		return nil, fmt.Errorf("spec.bucketName is immutable")
	}
	if oldC.Spec.BackendRef.Name != newC.Spec.BackendRef.Name {
		return nil, fmt.Errorf("spec.backendRef.name is immutable")
	}
	return nil, validateCommon(newC)
}

func (v *BucketClaimValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

func validateCommon(c *brokerv1a1.BucketClaim) error {
	if c.Spec.ForceDelete && c.Spec.DeletionPolicy != brokerv1a1.DeletionPolicyDelete {
		return fmt.Errorf("spec.forceDelete=true requires spec.deletionPolicy=Delete")
	}
	if rp := c.Spec.RotationPolicy; rp != nil && rp.Mode == brokerv1a1.RotationModeTimeBased && rp.IntervalDays < 7 {
		return fmt.Errorf("spec.rotationPolicy.intervalDays must be >= 7 when mode=TimeBased")
	}
	if a := c.Spec.AnonymousAccess; a != nil {
		switch a.Mode {
		case "", brokerv1a1.AnonymousModeNone, brokerv1a1.AnonymousModeReadOnly:
		default:
			return fmt.Errorf("spec.anonymousAccess.mode %q is not supported", a.Mode)
		}
		if a.PerSourceIPRPS < 0 {
			return fmt.Errorf("spec.anonymousAccess.perSourceIPRPS must be >= 0")
		}
	}
	return nil
}
