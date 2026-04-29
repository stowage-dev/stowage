// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package v1alpha1 contains API Schema definitions for the broker v1alpha1 API group.
// +kubebuilder:object:generate=true
// +groupName=broker.stowage.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	GroupVersion  = schema.GroupVersion{Group: "broker.stowage.io", Version: "v1alpha1"}
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}
	AddToScheme   = SchemeBuilder.AddToScheme
)

func init() {
	SchemeBuilder.Register(&S3Backend{}, &S3BackendList{}, &BucketClaim{}, &BucketClaimList{})
}
