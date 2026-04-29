// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/stowage-dev/stowage/internal/s3proxy"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// OperatorSourceSnapshotter is the read-only side of s3proxy.KubernetesSource
// the admin view needs. Kept as an interface so tests don't have to spin up
// a real informer.
type OperatorSourceSnapshotter interface {
	SnapshotCredentials() []*s3proxy.VirtualCredential
	SnapshotAnonymousBindings() []*s3proxy.AnonymousBinding
}

// S3ProxyViewDeps groups the collaborators the admin merged-view handlers
// need. OperatorSource is optional — when nil only SQLite-managed entries
// are returned. The SQLite store is the same one used by the credential
// CRUD handlers; reading directly avoids a second cache to keep in sync.
type S3ProxyViewDeps struct {
	Store          *sqlite.Store
	OperatorSource OperatorSourceSnapshotter
	Logger         *slog.Logger
}

// s3CredViewDTO mirrors s3CredDTO but adds a Source tag and the operator-only
// fields that don't exist in the SQLite row (claim namespace/name). A nil
// SecretKey is intentional — the merged view is read-only and the secret was
// either shown once on creation (sqlite) or only ever leaves the cluster as
// the tenant's wired-up Secret (kubernetes).
type s3CredViewDTO struct {
	AccessKey      string   `json:"access_key"`
	BackendID      string   `json:"backend_id"`
	Buckets        []string `json:"buckets"`
	UserID         string   `json:"user_id,omitempty"`
	Description    string   `json:"description,omitempty"`
	Enabled        bool     `json:"enabled"`
	ExpiresAt      string   `json:"expires_at,omitempty"`
	CreatedAt      string   `json:"created_at,omitempty"`
	CreatedBy      string   `json:"created_by,omitempty"`
	UpdatedAt      string   `json:"updated_at,omitempty"`
	UpdatedBy      string   `json:"updated_by,omitempty"`
	Source         string   `json:"source"` // "sqlite" | "kubernetes"
	ClaimNamespace string   `json:"claim_namespace,omitempty"`
	ClaimName      string   `json:"claim_name,omitempty"`
}

type s3AnonViewDTO struct {
	BackendID      string `json:"backend_id"`
	Bucket         string `json:"bucket"`
	Mode           string `json:"mode"`
	PerSourceIPRPS int    `json:"per_source_ip_rps"`
	CreatedAt      string `json:"created_at,omitempty"`
	CreatedBy      string `json:"created_by,omitempty"`
	Source         string `json:"source"` // "sqlite" | "kubernetes"
}

// handleListCredentials emits every virtual credential the proxy currently
// recognises, both UI-managed (SQLite) and operator-provisioned (K8s). Used
// by the admin S3 proxy dashboard. The merged view never reveals secret
// material; rotations still happen through the per-source mutating endpoints.
func (d *S3ProxyViewDeps) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	out := make([]s3CredViewDTO, 0, 32)

	if d.Store != nil {
		rows, err := d.Store.ListS3Credentials(r.Context())
		if err != nil {
			d.Logger.Warn("merged s3 credentials: list sqlite", "err", err.Error())
			writeError(w, http.StatusInternalServerError, "internal", "could not list credentials", "")
			return
		}
		for _, c := range rows {
			out = append(out, sqliteCredToView(c))
		}
	}
	if d.OperatorSource != nil {
		for _, vc := range d.OperatorSource.SnapshotCredentials() {
			out = append(out, kubeCredToView(vc))
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].AccessKey < out[j].AccessKey
	})
	writeJSON(w, http.StatusOK, map[string]any{"credentials": out})
}

func (d *S3ProxyViewDeps) handleListAnonymous(w http.ResponseWriter, r *http.Request) {
	out := make([]s3AnonViewDTO, 0, 8)

	if d.Store != nil {
		rows, err := d.Store.ListS3AnonymousBindings(r.Context())
		if err != nil {
			d.Logger.Warn("merged s3 anonymous: list sqlite", "err", err.Error())
			writeError(w, http.StatusInternalServerError, "internal", "could not list bindings", "")
			return
		}
		for _, b := range rows {
			out = append(out, sqliteAnonToView(b))
		}
	}
	if d.OperatorSource != nil {
		for _, b := range d.OperatorSource.SnapshotAnonymousBindings() {
			out = append(out, kubeAnonToView(b))
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].BackendID != out[j].BackendID {
			return out[i].BackendID < out[j].BackendID
		}
		return out[i].Bucket < out[j].Bucket
	})
	writeJSON(w, http.StatusOK, map[string]any{"bindings": out})
}

func sqliteCredToView(c *sqlite.S3Credential) s3CredViewDTO {
	out := s3CredViewDTO{
		AccessKey:   c.AccessKey,
		BackendID:   c.BackendID,
		Description: c.Description,
		Enabled:     c.Enabled,
		Source:      "sqlite",
		CreatedAt:   c.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   c.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if c.UserID.Valid {
		out.UserID = c.UserID.String
	}
	if c.CreatedBy.Valid {
		out.CreatedBy = c.CreatedBy.String
	}
	if c.UpdatedBy.Valid {
		out.UpdatedBy = c.UpdatedBy.String
	}
	if c.ExpiresAt.Valid {
		out.ExpiresAt = c.ExpiresAt.Time.UTC().Format(time.RFC3339)
	}
	if buckets, err := c.UnmarshalBuckets(); err == nil {
		out.Buckets = buckets
	}
	return out
}

func kubeCredToView(vc *s3proxy.VirtualCredential) s3CredViewDTO {
	buckets := make([]string, 0, len(vc.BucketScopes))
	for _, s := range vc.BucketScopes {
		buckets = append(buckets, s.BucketName)
	}
	out := s3CredViewDTO{
		AccessKey:      vc.AccessKeyID,
		BackendID:      vc.BackendName,
		Buckets:        buckets,
		Enabled:        true, // K8s entries that miss the cache are dropped on the source side
		Source:         "kubernetes",
		ClaimNamespace: vc.ClaimNamespace,
		ClaimName:      vc.ClaimName,
		UserID:         vc.UserID,
	}
	if vc.ExpiresAt != nil {
		out.ExpiresAt = vc.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return out
}

func sqliteAnonToView(b *sqlite.S3AnonymousBinding) s3AnonViewDTO {
	out := s3AnonViewDTO{
		BackendID:      b.BackendID,
		Bucket:         b.Bucket,
		Mode:           b.Mode,
		PerSourceIPRPS: b.PerSourceIPRPS,
		Source:         "sqlite",
		CreatedAt:      b.CreatedAt.UTC().Format(time.RFC3339),
	}
	if b.CreatedBy.Valid {
		out.CreatedBy = b.CreatedBy.String
	}
	return out
}

func kubeAnonToView(b *s3proxy.AnonymousBinding) s3AnonViewDTO {
	return s3AnonViewDTO{
		BackendID:      b.BackendName,
		Bucket:         b.BucketName,
		Mode:           b.Mode,
		PerSourceIPRPS: int(b.PerSourceIPRPS),
		Source:         "kubernetes",
	}
}
