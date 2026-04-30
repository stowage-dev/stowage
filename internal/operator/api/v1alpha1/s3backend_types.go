// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AddressingStyle controls whether S3 requests to the backend use path-style
// (/<bucket>/<key>) or virtual-hosted style (<bucket>.<host>/<key>).
// +kubebuilder:validation:Enum=path;virtual
type AddressingStyle string

const (
	AddressingStylePath    AddressingStyle = "path"
	AddressingStyleVirtual AddressingStyle = "virtual"
)

// AdminCredentialsRef points at a Secret containing the backend admin access
// and secret key.
type AdminCredentialsRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`

	// +kubebuilder:default=AWS_ACCESS_KEY_ID
	AccessKeyField string `json:"accessKeyField,omitempty"`
	// +kubebuilder:default=AWS_SECRET_ACCESS_KEY
	SecretKeyField string `json:"secretKeyField,omitempty"`
}

type CABundleRef struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	Namespace string `json:"namespace"`
	// +kubebuilder:default=ca.crt
	Key string `json:"key,omitempty"`
}

type TLSSpec struct {
	// +kubebuilder:default=false
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
	// +optional
	CABundleSecretRef *CABundleRef `json:"caBundleSecretRef,omitempty"`
}

type S3BackendSpec struct {
	// +kubebuilder:validation:Pattern=`^https?://.+`
	Endpoint string `json:"endpoint"`

	// +kubebuilder:default=us-east-1
	Region string `json:"region,omitempty"`

	// +kubebuilder:default=path
	AddressingStyle AddressingStyle `json:"addressingStyle,omitempty"`

	AdminCredentialsSecretRef AdminCredentialsRef `json:"adminCredentialsSecretRef"`

	// +optional
	TLS *TLSSpec `json:"tls,omitempty"`

	// BucketNameTemplate is a Go text/template rendering the real bucket
	// name for a claim. Variables: .Namespace, .Name, .Hash.
	// +kubebuilder:default=`{{ .Namespace }}-{{ .Name }}`
	BucketNameTemplate string `json:"bucketNameTemplate,omitempty"`
}

type S3BackendStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +optional
	BucketCount int32 `json:"bucketCount,omitempty"`
}

const (
	ConditionReady               = "Ready"
	ReasonEndpointReachable      = "EndpointReachable"
	ReasonEndpointUnreachable    = "EndpointUnreachable"
	ReasonCredentialsInvalid     = "CredentialsInvalid"
	ReasonTemplateInvalid        = "TemplateInvalid"
	ReasonBackendNotReady        = "BackendNotReady"
	ReasonBackendError           = "BackendError"
	ReasonBucketNotEmpty         = "BucketNotEmpty"
	ReasonBound                  = "Bound"
	ReasonCreatedOnBackend       = "CreatedOnBackend"
	ReasonSecretWritten          = "SecretWritten"
	ReasonCreationInconsistent   = "CreationInconsistent"
	ConditionBucketCreated       = "BucketCreated"
	ConditionCredentialsProvided = "CredentialsProvisioned"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=s3b
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.spec.endpoint`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Buckets",type=integer,JSONPath=`.status.bucketCount`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type S3Backend struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   S3BackendSpec   `json:"spec,omitempty"`
	Status S3BackendStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type S3BackendList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []S3Backend `json:"items"`
}
