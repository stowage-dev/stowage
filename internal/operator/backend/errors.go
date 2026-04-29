// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package backend wraps the outbound S3 client used by the operator and the
// proxy. All traffic here is signed with admin credentials.
package backend

import (
	"errors"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// Classified backend error conditions the reconciler cares about.
var (
	ErrBucketNotFound = errors.New("bucket not found")
	ErrBucketExists   = errors.New("bucket already exists")
	ErrBucketNotEmpty = errors.New("bucket not empty")
	ErrAccessDenied   = errors.New("access denied")
	ErrUnreachable    = errors.New("backend unreachable")
)

// Classify reduces a backend error to one of the errors above or returns the
// original.
func Classify(err error) error {
	if err == nil {
		return nil
	}

	var nsb *types.NoSuchBucket
	if errors.As(err, &nsb) {
		return ErrBucketNotFound
	}
	var nf *types.NotFound
	if errors.As(err, &nf) {
		return ErrBucketNotFound
	}
	var bne *types.BucketAlreadyOwnedByYou
	if errors.As(err, &bne) {
		return ErrBucketExists
	}
	var bao *types.BucketAlreadyExists
	if errors.As(err, &bao) {
		return ErrBucketExists
	}

	var api smithy.APIError
	if errors.As(err, &api) {
		switch api.ErrorCode() {
		case "NoSuchBucket", "NotFound":
			return ErrBucketNotFound
		case "BucketAlreadyOwnedByYou", "BucketAlreadyExists":
			return ErrBucketExists
		case "BucketNotEmpty":
			return ErrBucketNotEmpty
		case "AccessDenied", "AllAccessDisabled", "SignatureDoesNotMatch":
			return ErrAccessDenied
		}
	}

	return err
}

// IsStatus reports whether err carries the given HTTP status from an S3 call.
func IsStatus(err error, code int) bool {
	var re interface {
		HTTPStatusCode() int
	}
	if errors.As(err, &re) {
		return re.HTTPStatusCode() == code
	}
	_ = http.StatusText // keep import
	return false
}
