// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSecretToVirtualCredential_Legacy11(t *testing.T) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				k8sLabelRole:      k8sRoleVirtualCredential,
				k8sLabelClaimNS:   "tenant-1",
				k8sLabelClaimName: "uploads",
			},
		},
		Data: map[string][]byte{
			k8sDataAccessKeyID:     []byte("AKIAK8SLEGACY1234567"),
			k8sDataSecretAccessKey: []byte("legacysecretvalue"),
			k8sDataBucketName:      []byte("my-app-uploads"),
			k8sDataBackend:         []byte("primary"),
		},
	}
	got := secretToVirtualCredential(sec)
	require.NotNil(t, got)
	require.Equal(t, "AKIAK8SLEGACY1234567", got.AccessKeyID)
	require.Equal(t, "legacysecretvalue", got.SecretAccessKey)
	require.Equal(t, "primary", got.BackendName)
	require.Equal(t, "kubernetes", got.Source)
	require.Equal(t, "tenant-1", got.ClaimNamespace)
	require.Equal(t, "uploads", got.ClaimName)
	require.Len(t, got.BucketScopes, 1)
	require.Equal(t, "my-app-uploads", got.BucketScopes[0].BucketName)
}

func TestSecretToVirtualCredential_BucketScopesWins(t *testing.T) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{k8sLabelRole: k8sRoleVirtualCredential},
		},
		Data: map[string][]byte{
			k8sDataAccessKeyID:     []byte("AKIASCOPES"),
			k8sDataSecretAccessKey: []byte("secret"),
			k8sDataBucketName:      []byte("primary-bucket"),
			k8sDataBackend:         []byte("primary"),
			k8sDataBucketScopes:    []byte(`[{"bucket":"a","backend":"primary"},{"bucket":"b","backend":"primary"}]`),
		},
	}
	got := secretToVirtualCredential(sec)
	require.NotNil(t, got)
	require.Len(t, got.BucketScopes, 2)
	require.Equal(t, "a", got.BucketScopes[0].BucketName)
	require.Equal(t, "b", got.BucketScopes[1].BucketName)
}

func TestSecretToVirtualCredential_MalformedScopesFallsBack(t *testing.T) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{k8sLabelRole: k8sRoleVirtualCredential},
		},
		Data: map[string][]byte{
			k8sDataAccessKeyID:     []byte("AKIA"),
			k8sDataSecretAccessKey: []byte("secret"),
			k8sDataBucketName:      []byte("primary-bucket"),
			k8sDataBackend:         []byte("primary"),
			k8sDataBucketScopes:    []byte("not-json"),
		},
	}
	got := secretToVirtualCredential(sec)
	require.NotNil(t, got)
	require.Len(t, got.BucketScopes, 1)
	require.Equal(t, "primary-bucket", got.BucketScopes[0].BucketName)
}

func TestSecretToVirtualCredential_MissingFieldsRejected(t *testing.T) {
	cases := []struct {
		name string
		data map[string][]byte
	}{
		{"missing akid", map[string][]byte{
			k8sDataSecretAccessKey: []byte("s"), k8sDataBackend: []byte("p"),
		}},
		{"missing secret", map[string][]byte{
			k8sDataAccessKeyID: []byte("a"), k8sDataBackend: []byte("p"),
		}},
		{"missing backend", map[string][]byte{
			k8sDataAccessKeyID: []byte("a"), k8sDataSecretAccessKey: []byte("s"),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sec := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{k8sLabelRole: k8sRoleVirtualCredential},
				},
				Data: tc.data,
			}
			require.Nil(t, secretToVirtualCredential(sec))
		})
	}
}

func TestSecretToVirtualCredential_ExpiresAtAnnotation(t *testing.T) {
	exp := time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC).Format(time.RFC3339)
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      map[string]string{k8sLabelRole: k8sRoleVirtualCredential},
			Annotations: map[string]string{k8sAnnotationExpiresAt: exp},
		},
		Data: map[string][]byte{
			k8sDataAccessKeyID:     []byte("a"),
			k8sDataSecretAccessKey: []byte("s"),
			k8sDataBucketName:      []byte("b"),
			k8sDataBackend:         []byte("p"),
		},
	}
	got := secretToVirtualCredential(sec)
	require.NotNil(t, got)
	require.NotNil(t, got.ExpiresAt)
	require.Equal(t, 2027, got.ExpiresAt.Year())
}

func TestSecretToAnonymousBinding_Happy(t *testing.T) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{k8sLabelRole: k8sRoleAnonymousBinding},
		},
		Data: map[string][]byte{
			k8sDataBucketName: []byte("public"),
			k8sDataBackend:    []byte("primary"),
			k8sDataAnonMode:   []byte("ReadOnly"),
			k8sDataAnonRPS:    []byte("42"),
		},
	}
	got := secretToAnonymousBinding(sec)
	require.NotNil(t, got)
	require.Equal(t, "public", got.BucketName)
	require.Equal(t, "primary", got.BackendName)
	require.Equal(t, "ReadOnly", got.Mode)
	require.InDelta(t, 42, got.PerSourceIPRPS, 0.001)
	require.Equal(t, "kubernetes", got.Source)
}

func TestSecretToAnonymousBinding_MissingFields(t *testing.T) {
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{k8sLabelRole: k8sRoleAnonymousBinding},
		},
		Data: map[string][]byte{
			k8sDataBucketName: []byte("public"),
			// backend missing
			k8sDataAnonMode: []byte("ReadOnly"),
		},
	}
	require.Nil(t, secretToAnonymousBinding(sec))
}
