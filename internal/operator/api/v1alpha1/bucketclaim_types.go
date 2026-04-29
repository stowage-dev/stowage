// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:validation:Enum=Delete;Retain
type DeletionPolicy string

const (
	DeletionPolicyDelete DeletionPolicy = "Delete"
	DeletionPolicyRetain DeletionPolicy = "Retain"
)

// +kubebuilder:validation:Enum=Manual;TimeBased
type RotationMode string

const (
	RotationModeManual    RotationMode = "Manual"
	RotationModeTimeBased RotationMode = "TimeBased"
)

// +kubebuilder:validation:Enum=None;ReadOnly
type AnonymousMode string

const (
	AnonymousModeNone     AnonymousMode = "None"
	AnonymousModeReadOnly AnonymousMode = "ReadOnly"
)

// AnonymousAccess opens a bucket to unauthenticated callers via the broker
// proxy. The proxy enforces a tight read-only operation allowlist; the
// underlying backend bucket stays private and is only reachable through the
// proxy.
type AnonymousAccess struct {
	// +kubebuilder:default=None
	Mode AnonymousMode `json:"mode,omitempty"`
	// PerSourceIPRPS bounds anonymous requests per second per client IP. Zero
	// means inherit the proxy default configured at install time.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=0
	PerSourceIPRPS int32 `json:"perSourceIPRPS,omitempty"`
}

type BackendRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// BucketQuota declares per-bucket storage limits enforced by the stowage S3
// proxy. Soft is informational (logged + surfaced in the dashboard); Hard
// causes the proxy to refuse uploads with HTTP 507 once exceeded. The
// operator copies these values into the consumer Secret data fields
// `quota_soft_bytes` / `quota_hard_bytes` (decimal byte counts) so the
// proxy's KubernetesLimitSource can read them off the same informer.
//
// Either value may be omitted independently; omitting both is equivalent
// to setting no quota at all (the proxy treats the bucket as unlimited).
// When both are set, Soft must be ≤ Hard or the validating webhook
// rejects the claim.
//
// +kubebuilder:validation:XValidation:rule="!(has(self.soft) && has(self.hard)) || quantity(self.soft).isLessThan(quantity(self.hard)) || quantity(self.soft).isGreaterThan(quantity(self.hard)) == false",message="quota.soft must be less than or equal to quota.hard"
type BucketQuota struct {
	// Soft is the warning threshold. The proxy still allows the upload
	// but emits a warning to the audit log. Use a Kubernetes Quantity
	// (e.g. "10Gi", "500Mi", "2T").
	// +optional
	Soft *resource.Quantity `json:"soft,omitempty"`
	// Hard is the cap. Uploads that would push usage past Hard are
	// rejected with S3 EntityTooLarge (HTTP 507).
	// +optional
	Hard *resource.Quantity `json:"hard,omitempty"`
}

type ConnectionSecretRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="self.mode != 'TimeBased' || self.intervalDays >= 7",message="TimeBased rotation requires intervalDays >= 7"
type RotationPolicy struct {
	// +kubebuilder:default=Manual
	Mode RotationMode `json:"mode,omitempty"`
	// +kubebuilder:default=90
	// +kubebuilder:validation:Minimum=1
	IntervalDays int32 `json:"intervalDays,omitempty"`
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=0
	OverlapSeconds int32 `json:"overlapSeconds,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!self.forceDelete || self.deletionPolicy == 'Delete'",message="forceDelete=true requires deletionPolicy=Delete"
type BucketClaimSpec struct {
	BackendRef BackendRef `json:"backendRef"`

	// BucketName is an explicit override for the real bucket name. When empty,
	// the S3Backend's BucketNameTemplate is rendered instead.
	// +optional
	// +kubebuilder:validation:Pattern=`^$|^[a-z0-9][a-z0-9.-]{2,62}$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="bucketName is immutable"
	BucketName string `json:"bucketName,omitempty"`

	// +kubebuilder:default=Retain
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`

	// +kubebuilder:default=false
	ForceDelete bool `json:"forceDelete,omitempty"`

	// +optional
	WriteConnectionSecretToRef *ConnectionSecretRef `json:"writeConnectionSecretToRef,omitempty"`

	// +optional
	RotationPolicy *RotationPolicy `json:"rotationPolicy,omitempty"`

	// +optional
	AnonymousAccess *AnonymousAccess `json:"anonymousAccess,omitempty"`

	// Quota declares optional per-bucket storage limits enforced by the
	// stowage S3 proxy. When set, the operator copies the values into the
	// consumer Secret so the proxy's KubernetesLimitSource picks them up
	// without an extra informer.
	// +optional
	Quota *BucketQuota `json:"quota,omitempty"`
}

// +kubebuilder:validation:Enum=Pending;Creating;Bound;Failed;Deleting
type BucketClaimPhase string

const (
	PhasePending  BucketClaimPhase = "Pending"
	PhaseCreating BucketClaimPhase = "Creating"
	PhaseBound    BucketClaimPhase = "Bound"
	PhaseFailed   BucketClaimPhase = "Failed"
	PhaseDeleting BucketClaimPhase = "Deleting"
)

type BucketClaimStatus struct {
	// +optional
	Phase BucketClaimPhase `json:"phase,omitempty"`
	// +optional
	BucketName string `json:"bucketName,omitempty"`
	// +optional
	ProxyEndpoint string `json:"proxyEndpoint,omitempty"`
	// +optional
	BoundSecretName string `json:"boundSecretName,omitempty"`
	// +optional
	AccessKeyID string `json:"accessKeyId,omitempty"`
	// +optional
	RotatedAt *metav1.Time `json:"rotatedAt,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=bc
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Bucket",type=string,JSONPath=`.status.bucketName`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type BucketClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BucketClaimSpec   `json:"spec,omitempty"`
	Status BucketClaimStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type BucketClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BucketClaim `json:"items"`
}

// Finalizer owned by the BucketClaim reconciler.
const Finalizer = "broker.stowage.io/bucketclaim-protection"
