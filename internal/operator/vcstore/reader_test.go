// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package vcstore

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newSec(akid, sk, bucket string, expires *time.Time) *corev1.Secret {
	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      InternalSecretName(akid),
			Namespace: "stowage-system",
			Labels: map[string]string{
				LabelRole:        RoleVirtualCredential,
				LabelAccessKeyID: akid,
				LabelClaimNS:     "ns",
				LabelClaimName:   "n",
			},
		},
		Data: map[string][]byte{
			DataAccessKeyID:     []byte(akid),
			DataSecretAccessKey: []byte(sk),
			DataBucketName:      []byte(bucket),
			DataBackend:         []byte("primary"),
		},
	}
	if expires != nil {
		s.Annotations = map[string]string{AnnotationExpiresAt: expires.UTC().Format(time.RFC3339)}
	}
	return s
}

func TestReader_UpsertAndLookup(t *testing.T) {
	r := &Reader{byAKID: map[string]*VirtualCredential{}}
	r.upsert(newSec("AKIAABC", "s1", "bucket-1", nil))
	r.upsert(newSec("AKIAXYZ", "s2", "bucket-2", nil))

	vc, ok := r.Lookup("AKIAABC")
	require.True(t, ok)
	require.Equal(t, "s1", vc.SecretAccessKey)
	require.Equal(t, "bucket-1", vc.BucketName)
	require.Equal(t,
		[]BucketScope{{BucketName: "bucket-1", BackendName: "primary"}},
		vc.BucketScopes,
		"legacy-shaped Secret must still yield a populated single-element BucketScopes")

	_, ok = r.Lookup("nope")
	require.False(t, ok)
}

func TestReader_BucketScopesFromJSON(t *testing.T) {
	r := &Reader{byAKID: map[string]*VirtualCredential{}}
	s := newSec("AKIAGRANT00000000000", "sk", "primary-bucket", nil)
	s.Data[DataBucketScopes] = []byte(`[
		{"bucket":"primary-bucket","backend":"primary"},
		{"bucket":"secondary-bucket","backend":"primary"},
		{"bucket":"tertiary-bucket","backend":"primary"}
	]`)
	r.upsert(s)

	vc, ok := r.Lookup("AKIAGRANT00000000000")
	require.True(t, ok)
	require.Len(t, vc.BucketScopes, 3)
	require.Equal(t, "primary-bucket", vc.BucketScopes[0].BucketName)
	require.Equal(t, "secondary-bucket", vc.BucketScopes[1].BucketName)
	require.Equal(t, "tertiary-bucket", vc.BucketScopes[2].BucketName)
}

func TestReader_InvalidBucketScopesFallsBackToLegacy(t *testing.T) {
	r := &Reader{byAKID: map[string]*VirtualCredential{}}
	s := newSec("AKIAINVALID000000000", "sk", "only-bucket", nil)
	// Malformed JSON — reader must not drop the credential entirely, just
	// fall back to the singular bucket_name field.
	s.Data[DataBucketScopes] = []byte(`{not-json`)
	r.upsert(s)

	vc, ok := r.Lookup("AKIAINVALID000000000")
	require.True(t, ok)
	require.Equal(t,
		[]BucketScope{{BucketName: "only-bucket", BackendName: "primary"}},
		vc.BucketScopes)
}

func TestReader_Expiry(t *testing.T) {
	r := &Reader{byAKID: map[string]*VirtualCredential{}}
	past := time.Now().Add(-5 * time.Second)
	r.upsert(newSec("AKIAEXP", "s", "bucket", &past))

	_, ok := r.Lookup("AKIAEXP")
	require.False(t, ok, "expired credential should not resolve")
}

func TestReader_Delete(t *testing.T) {
	r := &Reader{byAKID: map[string]*VirtualCredential{}}
	r.upsert(newSec("AKIAXYZ", "s", "bucket", nil))
	r.delete(newSec("AKIAXYZ", "s", "bucket", nil))
	_, ok := r.Lookup("AKIAXYZ")
	require.False(t, ok)
}
