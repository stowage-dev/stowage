// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// S3Credential is one row of the s3_credentials table — a virtual S3 access
// key handed to a tenant. SecretKeyEnc is the sealed envelope produced by
// internal/secrets; callers unseal it briefly when (re)building the proxy's
// in-memory cache. Buckets is the JSON-serialised scope set as it sits in
// the column; use UnmarshalBuckets for the parsed form.
type S3Credential struct {
	AccessKey    string
	SecretKeyEnc []byte
	BackendID    string
	Buckets      string // JSON array; use UnmarshalBuckets to parse
	UserID       sql.NullString
	Description  string
	Enabled      bool
	ExpiresAt    sql.NullTime
	CreatedAt    time.Time
	CreatedBy    sql.NullString
	UpdatedAt    time.Time
	UpdatedBy    sql.NullString
}

// UnmarshalBuckets returns the parsed bucket scope. Errors are surfaced —
// the caller should treat a malformed Buckets column as a hard failure
// rather than silently authorising no buckets.
func (c *S3Credential) UnmarshalBuckets() ([]string, error) {
	var out []string
	if c.Buckets == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(c.Buckets), &out); err != nil {
		return nil, fmt.Errorf("s3_credentials.buckets: %w", err)
	}
	return out, nil
}

// MarshalBuckets sets the Buckets JSON column from a Go slice.
func (c *S3Credential) MarshalBuckets(b []string) error {
	if b == nil {
		b = []string{}
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return fmt.Errorf("marshal buckets: %w", err)
	}
	c.Buckets = string(raw)
	return nil
}

var (
	ErrS3CredentialNotFound = errors.New("s3 credential not found")
	ErrS3AccessKeyTaken     = errors.New("s3 access key already taken")
)

const s3CredCols = `access_key, secret_key_enc, backend_id, buckets, user_id,
 description, enabled, expires_at, created_at, created_by, updated_at, updated_by`

func scanS3Credential(row interface{ Scan(...any) error }) (*S3Credential, error) {
	var c S3Credential
	var enabled int
	if err := row.Scan(
		&c.AccessKey, &c.SecretKeyEnc, &c.BackendID, &c.Buckets, &c.UserID,
		&c.Description, &enabled, &c.ExpiresAt, &c.CreatedAt, &c.CreatedBy,
		&c.UpdatedAt, &c.UpdatedBy,
	); err != nil {
		return nil, err
	}
	c.Enabled = enabled != 0
	return &c, nil
}

func (s *Store) CreateS3Credential(ctx context.Context, c *S3Credential) error {
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO s3_credentials (`+s3CredCols+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.AccessKey, c.SecretKeyEnc, c.BackendID, c.Buckets, c.UserID,
		c.Description, boolToInt(c.Enabled), c.ExpiresAt, c.CreatedAt, c.CreatedBy,
		c.UpdatedAt, c.UpdatedBy,
	)
	if err != nil {
		return translateS3CredErr(err)
	}
	return nil
}

func (s *Store) GetS3Credential(ctx context.Context, accessKey string) (*S3Credential, error) {
	row := s.R.QueryRowContext(ctx,
		`SELECT `+s3CredCols+` FROM s3_credentials WHERE access_key = ?`, accessKey)
	c, err := scanS3Credential(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrS3CredentialNotFound
	}
	return c, err
}

// ListS3Credentials returns every row, ordered by access_key. Callers that
// only want enabled credentials must filter — the proxy's in-memory cache
// drops disabled entries on rebuild.
func (s *Store) ListS3Credentials(ctx context.Context) ([]*S3Credential, error) {
	rows, err := s.R.QueryContext(ctx,
		`SELECT `+s3CredCols+` FROM s3_credentials ORDER BY access_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*S3Credential
	for rows.Next() {
		c, err := scanS3Credential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// S3CredentialPatch carries fields the admin can change after creation. The
// access_key is immutable (it's the primary key). Pointers distinguish "set"
// from "leave alone". Buckets is the marshalled JSON column; the API layer
// is responsible for round-tripping through MarshalBuckets/UnmarshalBuckets
// so callers don't accidentally write a non-array value.
type S3CredentialPatch struct {
	BackendID   *string
	Buckets     *string
	UserID      *sql.NullString
	Description *string
	Enabled     *bool
	ExpiresAt   *sql.NullTime
	UpdatedBy   sql.NullString
	UpdatedAt   time.Time
}

func (s *Store) UpdateS3Credential(ctx context.Context, accessKey string, p S3CredentialPatch) error {
	var sets []string
	var args []any
	if p.BackendID != nil {
		sets = append(sets, "backend_id = ?")
		args = append(args, *p.BackendID)
	}
	if p.Buckets != nil {
		sets = append(sets, "buckets = ?")
		args = append(args, *p.Buckets)
	}
	if p.UserID != nil {
		sets = append(sets, "user_id = ?")
		args = append(args, *p.UserID)
	}
	if p.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *p.Description)
	}
	if p.Enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, boolToInt(*p.Enabled))
	}
	if p.ExpiresAt != nil {
		sets = append(sets, "expires_at = ?")
		args = append(args, *p.ExpiresAt)
	}
	if len(sets) == 0 {
		return nil
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = time.Now().UTC()
	}
	sets = append(sets, "updated_at = ?", "updated_by = ?")
	args = append(args, p.UpdatedAt, p.UpdatedBy, accessKey)

	res, err := s.DB.ExecContext(ctx,
		`UPDATE s3_credentials SET `+strings.Join(sets, ", ")+` WHERE access_key = ?`, args...)
	if err != nil {
		return translateS3CredErr(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrS3CredentialNotFound
	}
	return nil
}

func (s *Store) DeleteS3Credential(ctx context.Context, accessKey string) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM s3_credentials WHERE access_key = ?`, accessKey)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrS3CredentialNotFound
	}
	return nil
}

func translateS3CredErr(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "UNIQUE") && strings.Contains(msg, "s3_credentials.access_key"),
		strings.Contains(msg, "PRIMARY KEY") && strings.Contains(msg, "s3_credentials"):
		return ErrS3AccessKeyTaken
	default:
		return fmt.Errorf("s3_credentials: %w", err)
	}
}
