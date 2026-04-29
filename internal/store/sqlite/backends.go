// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Backend is one row of the backends table — an admin-managed S3 endpoint.
// SecretKeyEnc is the sealed envelope produced by internal/secrets. Callers
// hold the cleartext only briefly during create/update or while building a
// driver; it is never read back through the API.
type Backend struct {
	ID           string
	Name         string
	Type         string
	Endpoint     string
	Region       string
	PathStyle    bool
	AccessKey    string
	SecretKeyEnc []byte
	Enabled      bool
	CreatedAt    time.Time
	CreatedBy    sql.NullString
	UpdatedAt    time.Time
	UpdatedBy    sql.NullString
}

var (
	ErrBackendNotFound = errors.New("backend not found")
	ErrBackendIDTaken  = errors.New("backend id already taken")
)

const backendCols = `id, name, type, endpoint, region, path_style, access_key,
 secret_key_enc, enabled, created_at, created_by, updated_at, updated_by`

func scanBackend(row interface {
	Scan(dest ...any) error
}) (*Backend, error) {
	var b Backend
	var pathStyle, enabled int
	if err := row.Scan(
		&b.ID, &b.Name, &b.Type, &b.Endpoint, &b.Region, &pathStyle, &b.AccessKey,
		&b.SecretKeyEnc, &enabled, &b.CreatedAt, &b.CreatedBy, &b.UpdatedAt, &b.UpdatedBy,
	); err != nil {
		return nil, err
	}
	b.PathStyle = pathStyle != 0
	b.Enabled = enabled != 0
	return &b, nil
}

func (s *Store) CreateBackend(ctx context.Context, b *Backend) error {
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO backends (`+backendCols+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, b.Name, b.Type, b.Endpoint, b.Region, boolToInt(b.PathStyle), b.AccessKey,
		b.SecretKeyEnc, boolToInt(b.Enabled), b.CreatedAt, b.CreatedBy, b.UpdatedAt, b.UpdatedBy,
	)
	if err != nil {
		return translateBackendErr(err)
	}
	return nil
}

func (s *Store) GetBackend(ctx context.Context, id string) (*Backend, error) {
	row := s.R.QueryRowContext(ctx,
		`SELECT `+backendCols+` FROM backends WHERE id = ?`, id)
	b, err := scanBackend(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBackendNotFound
	}
	return b, err
}

// ListBackends returns every row, ordered by id. The caller is responsible
// for filtering by Enabled when registering at startup.
func (s *Store) ListBackends(ctx context.Context) ([]*Backend, error) {
	rows, err := s.R.QueryContext(ctx,
		`SELECT `+backendCols+` FROM backends ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Backend
	for rows.Next() {
		b, err := scanBackend(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// BackendPatch carries fields the admin can change after creation. The id is
// immutable (it's the registry key). Pointers distinguish "set" from "leave
// alone"; SecretKeyEnc is a slice with its own "leave alone" sentinel — pass
// a non-nil slice (possibly empty, meaning "no secret") to overwrite, or nil
// to keep the existing value.
type BackendPatch struct {
	Name         *string
	Endpoint     *string
	Region       *string
	PathStyle    *bool
	AccessKey    *string
	SecretKeyEnc []byte // nil = leave alone; non-nil = replace (empty replaces with NULL)
	Enabled      *bool
	UpdatedBy    sql.NullString
	UpdatedAt    time.Time
	// secretSet is set true by callers when SecretKeyEnc should overwrite.
	// We can't tell apart "nil" from "empty" otherwise — empty is a valid
	// "clear the secret" value, while nil should skip the column.
	secretSet bool
}

// SetSecret records a new sealed secret on the patch. Pass nil to clear the
// stored secret; the column will be set to NULL. Use this rather than
// assigning SecretKeyEnc directly so the "leave alone" vs "clear" distinction
// is explicit.
func (p *BackendPatch) SetSecret(enc []byte) {
	p.SecretKeyEnc = enc
	p.secretSet = true
}

// HasSecret reports whether SetSecret was called on this patch.
func (p *BackendPatch) HasSecret() bool { return p.secretSet }

func (s *Store) UpdateBackend(ctx context.Context, id string, p BackendPatch) error {
	var sets []string
	var args []any
	if p.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *p.Name)
	}
	if p.Endpoint != nil {
		sets = append(sets, "endpoint = ?")
		args = append(args, *p.Endpoint)
	}
	if p.Region != nil {
		sets = append(sets, "region = ?")
		args = append(args, *p.Region)
	}
	if p.PathStyle != nil {
		sets = append(sets, "path_style = ?")
		args = append(args, boolToInt(*p.PathStyle))
	}
	if p.AccessKey != nil {
		sets = append(sets, "access_key = ?")
		args = append(args, *p.AccessKey)
	}
	if p.secretSet {
		sets = append(sets, "secret_key_enc = ?")
		if len(p.SecretKeyEnc) == 0 {
			args = append(args, nil)
		} else {
			args = append(args, p.SecretKeyEnc)
		}
	}
	if p.Enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, boolToInt(*p.Enabled))
	}
	if len(sets) == 0 {
		return nil
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = time.Now().UTC()
	}
	sets = append(sets, "updated_at = ?", "updated_by = ?")
	args = append(args, p.UpdatedAt, p.UpdatedBy, id)

	res, err := s.DB.ExecContext(ctx,
		`UPDATE backends SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return translateBackendErr(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrBackendNotFound
	}
	return nil
}

func (s *Store) DeleteBackend(ctx context.Context, id string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM backends WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrBackendNotFound
	}
	return nil
}

func translateBackendErr(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "UNIQUE") && strings.Contains(msg, "backends.id"):
		return ErrBackendIDTaken
	case strings.Contains(msg, "PRIMARY KEY") && strings.Contains(msg, "backends"):
		return ErrBackendIDTaken
	default:
		return fmt.Errorf("backends: %w", err)
	}
}
