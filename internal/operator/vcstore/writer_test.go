// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package vcstore

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestWriteInternal_ByteIdentity locks in the Secret shape that proxy clients
// (and any external secret-watching tooling) see today. Any refactor of the
// WriteInternal code path must continue to produce exactly these labels and
// exactly these data keys — notably, no `bucket_scopes` key appears in a
// legacy BucketClaim-minted credential.
func TestWriteInternal_ByteIdentity(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	w := &Writer{Client: cli, Namespace: "stowage-system"}

	vc := VirtualCredential{
		AccessKeyID:     "AKIATESTKEY000000000",
		SecretAccessKey: "secret-key-value-here",
		BucketName:      "my-app-uploads",
		ClaimNamespace:  "my-app",
		ClaimName:       "uploads",
		ClaimUID:        "abc-123",
		BackendName:     "primary",
	}
	require.NoError(t, w.WriteInternal(context.Background(), vc))

	var got corev1.Secret
	require.NoError(t, cli.Get(context.Background(),
		types.NamespacedName{Namespace: "stowage-system", Name: InternalSecretName(vc.AccessKeyID)},
		&got))

	require.Equal(t, map[string]string{
		LabelRole:        RoleVirtualCredential,
		LabelClaimNS:     "my-app",
		LabelClaimName:   "uploads",
		LabelClaimUID:    "abc-123",
		LabelAccessKeyID: "AKIATESTKEY000000000",
		LabelBackendName: "primary",
	}, got.Labels, "label set must not drift; proxy label selector depends on it")

	require.Equal(t, map[string][]byte{
		DataAccessKeyID:     []byte("AKIATESTKEY000000000"),
		DataSecretAccessKey: []byte("secret-key-value-here"),
		DataBucketName:      []byte("my-app-uploads"),
		DataClaimUID:        []byte("abc-123"),
		DataBackend:         []byte("primary"),
	}, got.Data, "legacy WriteInternal must not emit bucket_scopes or any new keys")

	require.Empty(t, got.Annotations, "no expiry annotation unless ExpiresAt is set")
	require.Equal(t, corev1.SecretTypeOpaque, got.Type)
}

func TestWriteInternal_ExpiryAnnotation(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	w := &Writer{Client: cli, Namespace: "stowage-system"}

	when := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	vc := VirtualCredential{
		AccessKeyID:     "AKIATESTKEY000000000",
		SecretAccessKey: "secret-key-value-here",
		BucketName:      "my-app-uploads",
		BackendName:     "primary",
		ExpiresAt:       &when,
	}
	require.NoError(t, w.WriteInternal(context.Background(), vc))

	var got corev1.Secret
	require.NoError(t, cli.Get(context.Background(),
		types.NamespacedName{Namespace: "stowage-system", Name: InternalSecretName(vc.AccessKeyID)},
		&got))

	require.Equal(t, "2026-05-01T12:00:00Z", got.Annotations[AnnotationExpiresAt])
}

// TestWriteAnonymousBinding_Roundtrip locks in the Secret shape for the
// anonymous-binding path. The proxy's AnonReader keys off these labels and
// data fields, so any drift is a wire-protocol change.
func TestWriteAnonymousBinding_Roundtrip(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	w := &Writer{Client: cli, Namespace: "stowage-system"}

	b := AnonymousBinding{
		BucketName:     "my-app-uploads",
		BackendName:    "primary",
		Mode:           "ReadOnly",
		PerSourceIPRPS: 25,
		ClaimNamespace: "my-app",
		ClaimName:      "uploads",
		ClaimUID:       "abc-123",
	}
	require.NoError(t, w.WriteAnonymousBinding(context.Background(), b))

	var got corev1.Secret
	require.NoError(t, cli.Get(context.Background(),
		types.NamespacedName{Namespace: "stowage-system", Name: AnonymousBindingSecretName(b.ClaimNamespace, b.ClaimName)},
		&got))

	require.Equal(t, map[string]string{
		LabelRole:        RoleAnonymousBinding,
		LabelClaimNS:     "my-app",
		LabelClaimName:   "uploads",
		LabelClaimUID:    "abc-123",
		LabelBackendName: "primary",
		LabelBucketName:  "my-app-uploads",
	}, got.Labels)

	require.Equal(t, []byte("my-app-uploads"), got.Data[DataBucketName])
	require.Equal(t, []byte("primary"), got.Data[DataBackend])
	require.Equal(t, []byte("ReadOnly"), got.Data[DataAnonMode])
	require.Equal(t, []byte("25"), got.Data[DataAnonRPS])
	require.Equal(t, []byte("abc-123"), got.Data[DataClaimUID])

	// Update path: changing Mode mutates the existing Secret in place.
	b.Mode = "None"
	b.PerSourceIPRPS = 0
	require.NoError(t, w.WriteAnonymousBinding(context.Background(), b))
	require.NoError(t, cli.Get(context.Background(),
		types.NamespacedName{Namespace: "stowage-system", Name: AnonymousBindingSecretName(b.ClaimNamespace, b.ClaimName)},
		&got))
	require.Equal(t, []byte("None"), got.Data[DataAnonMode])
	require.Equal(t, []byte("0"), got.Data[DataAnonRPS])
}

func TestWriteAnonymousBinding_RejectsIncompleteInput(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	w := &Writer{Client: cli, Namespace: "stowage-system"}

	// Bucket missing.
	require.Error(t, w.WriteAnonymousBinding(context.Background(), AnonymousBinding{
		BackendName: "primary", Mode: "ReadOnly",
	}))
	// Backend missing.
	require.Error(t, w.WriteAnonymousBinding(context.Background(), AnonymousBinding{
		BucketName: "b", Mode: "ReadOnly",
	}))
	// Mode missing.
	require.Error(t, w.WriteAnonymousBinding(context.Background(), AnonymousBinding{
		BucketName: "b", BackendName: "primary",
	}))
}

func TestDeleteAnonymousBindingByClaim(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	cli := fake.NewClientBuilder().WithScheme(scheme).Build()
	w := &Writer{Client: cli, Namespace: "stowage-system"}

	b := AnonymousBinding{
		BucketName: "my-app-uploads", BackendName: "primary", Mode: "ReadOnly",
		ClaimNamespace: "my-app", ClaimName: "uploads",
	}
	require.NoError(t, w.WriteAnonymousBinding(context.Background(), b))

	require.NoError(t, w.DeleteAnonymousBindingByClaim(context.Background(), "my-app", "uploads"))

	var got corev1.Secret
	err := cli.Get(context.Background(),
		types.NamespacedName{Namespace: "stowage-system", Name: AnonymousBindingSecretName("my-app", "uploads")},
		&got)
	require.Error(t, err, "binding Secret must be gone")

	// Idempotent: deleting again is a no-op.
	require.NoError(t, w.DeleteAnonymousBindingByClaim(context.Background(), "my-app", "uploads"))
}

func TestVirtualCredential_Normalize(t *testing.T) {
	t.Run("synthesises single scope from legacy fields", func(t *testing.T) {
		vc := &VirtualCredential{BucketName: "b", BackendName: "primary"}
		vc.Normalize()
		require.Equal(t, []BucketScope{{BucketName: "b", BackendName: "primary"}}, vc.BucketScopes)
	})
	t.Run("leaves populated BucketScopes untouched", func(t *testing.T) {
		existing := []BucketScope{
			{BucketName: "a", BackendName: "primary"},
			{BucketName: "b", BackendName: "primary"},
		}
		vc := &VirtualCredential{BucketName: "a", BackendName: "primary", BucketScopes: existing}
		vc.Normalize()
		require.Equal(t, existing, vc.BucketScopes)
	})
	t.Run("no-op when BucketName is empty", func(t *testing.T) {
		vc := &VirtualCredential{}
		vc.Normalize()
		require.Nil(t, vc.BucketScopes)
	})
}
