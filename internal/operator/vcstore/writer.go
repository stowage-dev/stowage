// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package vcstore

import (
	"context"
	"fmt"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	brokerv1a1 "github.com/stowage-dev/stowage/internal/operator/api/v1alpha1"
)

// BucketScope names one (bucket, backend) tuple a credential is authorised
// for. A legacy 1:1 credential has exactly one BucketScope; a many-to-many
// grant has up to N. The proxy's scope check is set-membership over this slice.
type BucketScope struct {
	BucketName  string `json:"bucket"`
	BackendName string `json:"backend"`
}

// VirtualCredential is the full mapping the proxy needs to authorize and
// forward a tenant request.
//
// BucketScopes is authoritative for scope enforcement. BucketName and
// BackendName remain populated with the *primary* scope (BucketScopes[0]) so
// legacy readers — and code paths that only care about the single-bucket
// case — keep working unchanged. Reader.secretToCredential and Normalize()
// both guarantee that BucketScopes is non-empty when BucketName is set.
type VirtualCredential struct {
	AccessKeyID     string
	SecretAccessKey string
	BucketName      string
	ClaimNamespace  string
	ClaimName       string
	ClaimUID        string
	BackendName     string
	BucketScopes    []BucketScope
	ExpiresAt       *time.Time
	// QuotaSoftBytes / QuotaHardBytes carry the optional per-bucket storage
	// limits the proxy enforces. Zero means "no limit on that side"; the
	// writer omits the corresponding Secret data field when both are zero.
	QuotaSoftBytes int64
	QuotaHardBytes int64
}

// Normalize guarantees BucketScopes is populated. It's called by production
// read paths (secretToCredential) and is safe to call from tests that build
// VirtualCredential literals without an explicit BucketScopes slice.
func (v *VirtualCredential) Normalize() {
	if len(v.BucketScopes) == 0 && v.BucketName != "" {
		v.BucketScopes = []BucketScope{{BucketName: v.BucketName, BackendName: v.BackendName}}
	}
}

// Writer is the operator-side writer of both Secret kinds.
type Writer struct {
	Client    client.Client
	Namespace string // the internal namespace, typically "stowage-system"
}

// WriteInternal creates or updates the proxy-facing Secret for a virtual credential.
// Internal Secrets cannot be cross-namespace owner-refed from the BucketClaim
// (K8s forbids it) so they are cleaned up by the claim's finalizer.
//
// The Secret produced here is byte-identical to pre-BucketScope releases: it
// carries the five legacy data keys only, with no bucket_scopes key. Callers
// that need multi-bucket credentials go through writeInternalSecret directly.
func (w *Writer) WriteInternal(ctx context.Context, vc VirtualCredential) error {
	labels := map[string]string{
		LabelRole:        RoleVirtualCredential,
		LabelClaimNS:     vc.ClaimNamespace,
		LabelClaimName:   vc.ClaimName,
		LabelClaimUID:    vc.ClaimUID,
		LabelAccessKeyID: vc.AccessKeyID,
		LabelBackendName: vc.BackendName,
	}
	data := map[string][]byte{
		DataAccessKeyID:     []byte(vc.AccessKeyID),
		DataSecretAccessKey: []byte(vc.SecretAccessKey),
		DataBucketName:      []byte(vc.BucketName),
		DataClaimUID:        []byte(vc.ClaimUID),
		DataBackend:         []byte(vc.BackendName),
	}
	if vc.QuotaSoftBytes > 0 {
		data[DataQuotaSoftBytes] = []byte(strconv.FormatInt(vc.QuotaSoftBytes, 10))
	}
	if vc.QuotaHardBytes > 0 {
		data[DataQuotaHardBytes] = []byte(strconv.FormatInt(vc.QuotaHardBytes, 10))
	}
	return w.writeInternalSecret(ctx, InternalSecretName(vc.AccessKeyID), labels, data, vc.ExpiresAt)
}

// writeInternalSecret is the shared create-or-update path for internal
// (proxy-facing) credential Secrets. Callers supply the already-built label
// map, data map, and optional expiry; this function does not inject any keys
// of its own, which is what keeps WriteInternal byte-identical to its
// pre-refactor form.
func (w *Writer) writeInternalSecret(ctx context.Context, name string, labels map[string]string, data map[string][]byte, expiresAt *time.Time) error {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: w.Namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}
	if expiresAt != nil {
		sec.Annotations = map[string]string{
			AnnotationExpiresAt: expiresAt.UTC().Format(time.RFC3339),
		}
	}

	var existing corev1.Secret
	err := w.Client.Get(ctx, types.NamespacedName{Namespace: w.Namespace, Name: name}, &existing)
	switch {
	case apierrors.IsNotFound(err):
		return w.Client.Create(ctx, sec)
	case err != nil:
		return fmt.Errorf("get existing internal secret: %w", err)
	}
	sec.ResourceVersion = existing.ResourceVersion
	return w.Client.Update(ctx, sec)
}

// DeleteInternalByClaim removes every internal Secret belonging to the given claim.
func (w *Writer) DeleteInternalByClaim(ctx context.Context, claimNS, claimName string) error {
	var list corev1.SecretList
	if err := w.Client.List(ctx, &list,
		client.InNamespace(w.Namespace),
		client.MatchingLabels{
			LabelRole:      RoleVirtualCredential,
			LabelClaimNS:   claimNS,
			LabelClaimName: claimName,
		},
	); err != nil {
		return fmt.Errorf("list internal secrets: %w", err)
	}
	for i := range list.Items {
		if err := w.Client.Delete(ctx, &list.Items[i]); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete internal secret %s: %w", list.Items[i].Name, err)
		}
	}
	return nil
}

// DeleteInternalByAccessKey removes a specific internal Secret by access key id.
func (w *Writer) DeleteInternalByAccessKey(ctx context.Context, accessKeyID string) error {
	name := InternalSecretName(accessKeyID)
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: w.Namespace}}
	if err := w.Client.Delete(ctx, sec); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// AnonymousBinding is the proxy-facing record describing a publicly-readable
// bucket. The proxy looks it up by inbound bucket name to authorize
// unauthenticated requests.
type AnonymousBinding struct {
	BucketName     string
	BackendName    string
	Mode           string
	PerSourceIPRPS int32
	ClaimNamespace string
	ClaimName      string
	ClaimUID       string
}

// WriteAnonymousBinding creates or updates the proxy-facing Secret describing
// a bucket's anonymous-access policy. Secrets live in the operator namespace
// and are cleaned up on claim delete via DeleteAnonymousBindingByClaim.
func (w *Writer) WriteAnonymousBinding(ctx context.Context, b AnonymousBinding) error {
	if b.BucketName == "" || b.BackendName == "" || b.Mode == "" {
		return fmt.Errorf("anonymous binding requires bucket, backend, and mode")
	}
	labels := map[string]string{
		LabelRole:        RoleAnonymousBinding,
		LabelClaimNS:     b.ClaimNamespace,
		LabelClaimName:   b.ClaimName,
		LabelClaimUID:    b.ClaimUID,
		LabelBackendName: b.BackendName,
		LabelBucketName:  b.BucketName,
	}
	data := map[string][]byte{
		DataBucketName: []byte(b.BucketName),
		DataBackend:    []byte(b.BackendName),
		DataClaimUID:   []byte(b.ClaimUID),
		DataAnonMode:   []byte(b.Mode),
		DataAnonRPS:    []byte(strconv.FormatInt(int64(b.PerSourceIPRPS), 10)),
	}
	name := AnonymousBindingSecretName(b.ClaimNamespace, b.ClaimName)
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: w.Namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}
	var existing corev1.Secret
	err := w.Client.Get(ctx, types.NamespacedName{Namespace: w.Namespace, Name: name}, &existing)
	switch {
	case apierrors.IsNotFound(err):
		return w.Client.Create(ctx, sec)
	case err != nil:
		return fmt.Errorf("get existing anonymous binding: %w", err)
	}
	sec.ResourceVersion = existing.ResourceVersion
	return w.Client.Update(ctx, sec)
}

// DeleteAnonymousBindingByClaim removes the anonymous binding (if any) owned
// by the given claim. Returns nil when no binding exists.
func (w *Writer) DeleteAnonymousBindingByClaim(ctx context.Context, claimNS, claimName string) error {
	name := AnonymousBindingSecretName(claimNS, claimName)
	sec := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: w.Namespace}}
	if err := w.Client.Delete(ctx, sec); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete anonymous binding: %w", err)
	}
	return nil
}

// ConsumerValues is the content of the consumer-facing Secret.
type ConsumerValues struct {
	AccessKeyID     string
	SecretAccessKey string
	Region          string
	EndpointURL     string
	BucketName      string
	AddressingStyle string
}

// WriteConsumer creates or updates the tenant-facing Secret in the claim's
// namespace. It is always owner-refed by the BucketClaim so standard GC
// cleans it up on delete.
func (w *Writer) WriteConsumer(ctx context.Context, claim *brokerv1a1.BucketClaim, secretName string, v ConsumerValues) error {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: claim.Namespace,
			Labels: map[string]string{
				LabelRole:      RoleConsumerSecret,
				LabelClaimName: claim.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         brokerv1a1.GroupVersion.String(),
					Kind:               "BucketClaim",
					Name:               claim.Name,
					UID:                claim.UID,
					Controller:         ptrBool(true),
					BlockOwnerDeletion: ptrBool(true),
				},
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			EnvAccessKeyID:     v.AccessKeyID,
			EnvSecretAccessKey: v.SecretAccessKey,
			EnvRegion:          v.Region,
			EnvEndpointURL:     v.EndpointURL,
			EnvEndpointURLS3:   v.EndpointURL,
			EnvBucketName:      v.BucketName,
			EnvAddressingStyle: v.AddressingStyle,
		},
	}

	var existing corev1.Secret
	err := w.Client.Get(ctx, types.NamespacedName{Namespace: claim.Namespace, Name: secretName}, &existing)
	switch {
	case apierrors.IsNotFound(err):
		return w.Client.Create(ctx, sec)
	case err != nil:
		return fmt.Errorf("get existing consumer secret: %w", err)
	}
	sec.ResourceVersion = existing.ResourceVersion
	return w.Client.Update(ctx, sec)
}

func ptrBool(b bool) *bool { return &b }
