// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package vcstore

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newAnonReaderForTest() *AnonReader {
	return &AnonReader{byBucket: map[string]*AnonymousBinding{}}
}

func bindingSecret(bucket, backend, mode, rps, ns, name, uid string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      AnonymousBindingSecretName(ns, name),
			Namespace: "stowage-system",
			Labels: map[string]string{
				LabelRole:        RoleAnonymousBinding,
				LabelClaimNS:     ns,
				LabelClaimName:   name,
				LabelClaimUID:    uid,
				LabelBackendName: backend,
				LabelBucketName:  bucket,
			},
		},
		Data: map[string][]byte{
			DataBucketName: []byte(bucket),
			DataBackend:    []byte(backend),
			DataAnonMode:   []byte(mode),
			DataAnonRPS:    []byte(rps),
			DataClaimUID:   []byte(uid),
		},
	}
}

func TestAnonReader_UpsertAndLookup(t *testing.T) {
	r := newAnonReaderForTest()
	r.upsert(bindingSecret("my-bucket", "primary", "ReadOnly", "25", "ns", "claim", "uid-1"))

	got, ok := r.Lookup("my-bucket")
	require.True(t, ok)
	require.Equal(t, "my-bucket", got.BucketName)
	require.Equal(t, "primary", got.BackendName)
	require.Equal(t, "ReadOnly", got.Mode)
	require.Equal(t, int32(25), got.PerSourceIPRPS)
	require.Equal(t, "ns", got.ClaimNamespace)
	require.Equal(t, "claim", got.ClaimName)
	require.Equal(t, "uid-1", got.ClaimUID)
	require.Equal(t, 1, r.Size())
}

func TestAnonReader_LookupCaseInsensitive(t *testing.T) {
	r := newAnonReaderForTest()
	r.upsert(bindingSecret("Mixed-Case-Bucket", "primary", "ReadOnly", "0", "ns", "claim", "uid"))
	_, ok := r.Lookup("mixed-case-bucket")
	require.True(t, ok, "lookup must be case-insensitive")
	_, ok = r.Lookup("MIXED-CASE-BUCKET")
	require.True(t, ok)
}

func TestAnonReader_LookupEmptyBucket(t *testing.T) {
	r := newAnonReaderForTest()
	_, ok := r.Lookup("")
	require.False(t, ok)
}

func TestAnonReader_UpsertReplacesPriorBinding(t *testing.T) {
	r := newAnonReaderForTest()
	r.upsert(bindingSecret("my-bucket", "primary", "ReadOnly", "10", "ns", "claim", "uid-1"))
	r.upsert(bindingSecret("my-bucket", "primary", "ReadOnly", "50", "ns", "claim", "uid-1"))
	got, ok := r.Lookup("my-bucket")
	require.True(t, ok)
	require.Equal(t, int32(50), got.PerSourceIPRPS, "upsert must overwrite, not duplicate")
	require.Equal(t, 1, r.Size())
}

func TestAnonReader_Delete(t *testing.T) {
	r := newAnonReaderForTest()
	sec := bindingSecret("my-bucket", "primary", "ReadOnly", "25", "ns", "claim", "uid-1")
	r.upsert(sec)
	require.Equal(t, 1, r.Size())
	r.delete(sec)
	require.Equal(t, 0, r.Size())
	_, ok := r.Lookup("my-bucket")
	require.False(t, ok)
}

func TestAnonReader_DeleteFallsBackToDataBucketName(t *testing.T) {
	r := newAnonReaderForTest()
	sec := bindingSecret("my-bucket", "primary", "ReadOnly", "25", "ns", "claim", "uid-1")
	r.upsert(sec)
	// Strip the bucket label to force delete() to use Data fallback.
	sec.Labels[LabelBucketName] = ""
	r.delete(sec)
	require.Equal(t, 0, r.Size())
}

func TestAnonReader_UpsertIgnoresWrongRole(t *testing.T) {
	r := newAnonReaderForTest()
	sec := bindingSecret("my-bucket", "primary", "ReadOnly", "25", "ns", "claim", "uid-1")
	sec.Labels[LabelRole] = RoleVirtualCredential
	r.upsert(sec)
	require.Equal(t, 0, r.Size())
}

func TestAnonReader_UpsertIgnoresIncompleteData(t *testing.T) {
	r := newAnonReaderForTest()

	missingMode := bindingSecret("b", "primary", "", "0", "ns", "c", "u")
	r.upsert(missingMode)
	require.Equal(t, 0, r.Size(), "missing mode must not register")

	missingBackend := bindingSecret("b", "", "ReadOnly", "0", "ns", "c", "u")
	r.upsert(missingBackend)
	require.Equal(t, 0, r.Size(), "missing backend must not register")

	missingBucket := bindingSecret("", "primary", "ReadOnly", "0", "ns", "c", "u")
	r.upsert(missingBucket)
	require.Equal(t, 0, r.Size(), "missing bucket must not register")
}

func TestAnonReader_UpsertHandlesBadRPS(t *testing.T) {
	r := newAnonReaderForTest()
	sec := bindingSecret("my-bucket", "primary", "ReadOnly", "not-a-number", "ns", "c", "u")
	r.upsert(sec)
	got, ok := r.Lookup("my-bucket")
	require.True(t, ok, "binding must register even when rps is malformed")
	require.Equal(t, int32(0), got.PerSourceIPRPS, "bad rps falls back to 0")
}

func TestAnonReader_LookupReturnsCopy(t *testing.T) {
	r := newAnonReaderForTest()
	r.upsert(bindingSecret("my-bucket", "primary", "ReadOnly", "25", "ns", "claim", "uid"))
	got, _ := r.Lookup("my-bucket")
	got.Mode = "MUTATED"
	again, _ := r.Lookup("my-bucket")
	require.Equal(t, "ReadOnly", again.Mode, "callers must not mutate the cache via the returned pointer")
}
