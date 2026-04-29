// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package credentials resolves and generates credentials for both the admin
// (backend-facing) and virtual (tenant-facing) paths.
package credentials

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Admin holds the resolved backend admin credentials. The AccessKeyID is
// presented to the backend after the storage-space prefix has been applied.
type Admin struct {
	AccessKeyID     string
	SecretAccessKey string
}

// Resolver loads admin credentials from a Secret and applies a QuObjects-style
// "<storageSpace>:<rawAccessKey>" prefix when requested.
type Resolver struct {
	Client client.Client
}

// AdminSecretRef points at a Secret with the admin key material.
type AdminSecretRef struct {
	Name           string
	Namespace      string
	AccessKeyField string
	SecretKeyField string
	StorageSpace   string
}

func (r *Resolver) Resolve(ctx context.Context, ref AdminSecretRef) (Admin, error) {
	if ref.AccessKeyField == "" {
		ref.AccessKeyField = "AWS_ACCESS_KEY_ID"
	}
	if ref.SecretKeyField == "" {
		ref.SecretKeyField = "AWS_SECRET_ACCESS_KEY"
	}

	var sec corev1.Secret
	nsn := types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}
	if err := r.Client.Get(ctx, nsn, &sec); err != nil {
		return Admin{}, fmt.Errorf("get admin secret %s: %w", nsn, err)
	}

	rawAK, ok := sec.Data[ref.AccessKeyField]
	if !ok || len(rawAK) == 0 {
		return Admin{}, fmt.Errorf("admin secret %s missing key %q", nsn, ref.AccessKeyField)
	}
	rawSK, ok := sec.Data[ref.SecretKeyField]
	if !ok || len(rawSK) == 0 {
		return Admin{}, fmt.Errorf("admin secret %s missing key %q", nsn, ref.SecretKeyField)
	}

	ak := strings.TrimSpace(string(rawAK))
	if ref.StorageSpace != "" && !strings.Contains(ak, ":") {
		ak = ref.StorageSpace + ":" + ak
	}

	return Admin{
		AccessKeyID:     ak,
		SecretAccessKey: strings.TrimSpace(string(rawSK)),
	}, nil
}
