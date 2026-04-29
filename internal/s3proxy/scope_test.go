// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnforceScope(t *testing.T) {
	one := []BucketScope{
		{BucketName: "uploads", BackendName: "primary"},
	}
	three := []BucketScope{
		{BucketName: "uploads", BackendName: "primary"},
		{BucketName: "logs", BackendName: "primary"},
		{BucketName: "tmp", BackendName: "primary"},
	}

	cases := []struct {
		name    string
		scopes  []BucketScope
		request string
		want    bool
	}{
		{"service-level passes with empty scopes", nil, "", true},
		{"service-level passes with populated scopes", one, "", true},
		{"single-bucket match", one, "uploads", true},
		{"single-bucket case-insensitive match", one, "UpLoAdS", true},
		{"single-bucket miss", one, "other", false},
		{"multi-bucket match first", three, "uploads", true},
		{"multi-bucket match middle", three, "logs", true},
		{"multi-bucket match last", three, "tmp", true},
		{"multi-bucket case-insensitive match", three, "LOGS", true},
		{"multi-bucket miss", three, "unknown", false},
		{"empty scopes with request bucket denies", nil, "uploads", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, EnforceScope(tc.scopes, tc.request))
		})
	}
}
