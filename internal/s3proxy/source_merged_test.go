// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func mkSource(akid, source string, anon ...*AnonymousBinding) *fakeSource {
	f := &fakeSource{
		byAKID: map[string]*VirtualCredential{},
		byAnon: map[string]*AnonymousBinding{},
	}
	if akid != "" {
		f.byAKID[akid] = &VirtualCredential{AccessKeyID: akid, SecretAccessKey: "s", Source: source}
	}
	for _, a := range anon {
		f.byAnon[strings.ToLower(a.BucketName)] = a
	}
	return f
}

func TestMergedSource_FirstSourceWins(t *testing.T) {
	k8s := mkSource("AKIASHARED", "kubernetes")
	db := mkSource("AKIASHARED", "sqlite")
	merged := NewMergedSource(nil, k8s, db)

	got, ok := merged.Lookup("AKIASHARED")
	require.True(t, ok)
	require.Equal(t, "kubernetes", got.Source, "K8s source must shadow SQLite source")
}

func TestMergedSource_FallsThroughOnMiss(t *testing.T) {
	k8s := mkSource("", "kubernetes")
	db := mkSource("AKIAONLYDB", "sqlite")
	merged := NewMergedSource(nil, k8s, db)

	got, ok := merged.Lookup("AKIAONLYDB")
	require.True(t, ok)
	require.Equal(t, "sqlite", got.Source)
}

func TestMergedSource_NilSourcesSkipped(t *testing.T) {
	merged := NewMergedSource(nil, nil, mkSource("AKIA", "sqlite"))
	got, ok := merged.Lookup("AKIA")
	require.True(t, ok)
	require.Equal(t, "sqlite", got.Source)
}

func TestMergedSource_ShadowingLogsOnce(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, nil))

	k8s := mkSource("AKIADUPE", "kubernetes")
	db := mkSource("AKIADUPE", "sqlite")
	merged := NewMergedSource(logger, k8s, db)

	for i := 0; i < 5; i++ {
		_, _ = merged.Lookup("AKIADUPE")
	}

	got := buf.String()
	require.Contains(t, got, "shadowed")
	// Count occurrences of the warn line — should be exactly one.
	require.Equal(t, 1, strings.Count(got, "shadowed"),
		"shadow warning must be logged at most once per access key")
}

func TestMergedSource_LookupAnonFirstHit(t *testing.T) {
	k8s := mkSource("", "kubernetes",
		&AnonymousBinding{BucketName: "shared", Source: "kubernetes"})
	db := mkSource("", "sqlite",
		&AnonymousBinding{BucketName: "shared", Source: "sqlite"})
	merged := NewMergedSource(nil, k8s, db)

	got, ok := merged.LookupAnon("shared")
	require.True(t, ok)
	require.Equal(t, "kubernetes", got.Source)
}

func TestMergedSource_SizeIsSum(t *testing.T) {
	k8s := mkSource("AKIA1", "kubernetes")
	db := mkSource("AKIA2", "sqlite")
	merged := NewMergedSource(nil, k8s, db)
	require.Equal(t, 2, merged.Size())
}
