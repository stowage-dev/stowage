// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

func newCred(akid string) *sqlite.S3Credential {
	now := time.Now().UTC()
	c := &sqlite.S3Credential{
		AccessKey:    akid,
		SecretKeyEnc: []byte{0x01, 0x00, 0xde, 0xad, 0xbe, 0xef},
		BackendID:    "minio",
		UserID:       sql.NullString{String: "u-1", Valid: true},
		Description:  "test " + akid,
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
		CreatedBy:    sql.NullString{String: "admin", Valid: true},
		UpdatedBy:    sql.NullString{String: "admin", Valid: true},
	}
	if err := c.MarshalBuckets([]string{"uploads", "archive"}); err != nil {
		panic(err)
	}
	return c
}

func TestS3Credential_CreateGet(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	c := newCred("AKIATEST1")
	if err := store.CreateS3Credential(ctx, c); err != nil {
		t.Fatalf("CreateS3Credential: %v", err)
	}

	got, err := store.GetS3Credential(ctx, "AKIATEST1")
	if err != nil {
		t.Fatalf("GetS3Credential: %v", err)
	}
	if got.BackendID != "minio" || got.Description != "test AKIATEST1" || !got.Enabled {
		t.Fatalf("got %+v", got)
	}
	parsed, err := got.UnmarshalBuckets()
	if err != nil {
		t.Fatalf("UnmarshalBuckets: %v", err)
	}
	if !reflect.DeepEqual(parsed, []string{"uploads", "archive"}) {
		t.Fatalf("buckets mismatch: %v", parsed)
	}
}

func TestS3Credential_DuplicateAccessKey(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	if err := store.CreateS3Credential(ctx, newCred("AKIADUPE")); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	err := store.CreateS3Credential(ctx, newCred("AKIADUPE"))
	if !errors.Is(err, sqlite.ErrS3AccessKeyTaken) {
		t.Fatalf("want ErrS3AccessKeyTaken, got %v", err)
	}
}

func TestS3Credential_NotFound(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()
	_, err := store.GetS3Credential(ctx, "AKIANOPE")
	if !errors.Is(err, sqlite.ErrS3CredentialNotFound) {
		t.Fatalf("want ErrS3CredentialNotFound, got %v", err)
	}
}

func TestS3Credential_UpdateAndList(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if err := store.CreateS3Credential(ctx, newCred("AKIA1")); err != nil {
		t.Fatalf("create AKIA1: %v", err)
	}
	if err := store.CreateS3Credential(ctx, newCred("AKIA2")); err != nil {
		t.Fatalf("create AKIA2: %v", err)
	}

	disabled := false
	desc := "renamed"
	newBuckets := `["only-one"]`
	patch := sqlite.S3CredentialPatch{
		Enabled:     &disabled,
		Description: &desc,
		Buckets:     &newBuckets,
		UpdatedBy:   sql.NullString{String: "admin", Valid: true},
	}
	if err := store.UpdateS3Credential(ctx, "AKIA1", patch); err != nil {
		t.Fatalf("UpdateS3Credential: %v", err)
	}

	got, err := store.GetS3Credential(ctx, "AKIA1")
	if err != nil {
		t.Fatalf("GetS3Credential: %v", err)
	}
	if got.Enabled || got.Description != "renamed" {
		t.Fatalf("patch not applied: %+v", got)
	}
	parsed, err := got.UnmarshalBuckets()
	if err != nil || !reflect.DeepEqual(parsed, []string{"only-one"}) {
		t.Fatalf("buckets not updated: %v err=%v", parsed, err)
	}

	all, err := store.ListS3Credentials(ctx)
	if err != nil {
		t.Fatalf("ListS3Credentials: %v", err)
	}
	if len(all) != 2 || all[0].AccessKey != "AKIA1" || all[1].AccessKey != "AKIA2" {
		t.Fatalf("list ordering wrong: got %d entries: %v",
			len(all), []string{all[0].AccessKey, all[1].AccessKey})
	}
}

func TestS3Credential_Delete(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	if err := store.CreateS3Credential(ctx, newCred("AKIAD")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.DeleteS3Credential(ctx, "AKIAD"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := store.DeleteS3Credential(ctx, "AKIAD"); !errors.Is(err, sqlite.ErrS3CredentialNotFound) {
		t.Fatalf("second delete should miss, got %v", err)
	}
}
