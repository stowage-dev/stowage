// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"testing/fstest"
)

func TestScanInlineScriptHashes(t *testing.T) {
	body1 := "alert(1);"
	body2 := "\n\t\tconsole.log('multi');\n\t"
	doc := []byte("<!doctype html><html><head>" +
		"<script>" + body1 + "</script>" +
		"<script src=\"/app.js\"></script>" +
		"<SCRIPT type=\"module\">" + body2 + "</SCRIPT>" +
		"<script data-src=\"x\">noop</script>" +
		"</head></html>")

	got := scanInlineScriptHashes(doc)
	want := []string{
		hash(body1),
		hash(body2),
		hash("noop"),
	}
	if len(got) != len(want) {
		t.Fatalf("got %d hashes, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("hash[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestHasSrcAttr(t *testing.T) {
	cases := []struct {
		attrs string
		want  bool
	}{
		{` src="/x.js"`, true},
		{` SRC = '/x.js'`, true},
		{` type="module"`, false},
		{` data-src="x"`, false},
		{` x-src="y"`, false},
		{``, false},
		{` type="module" src="/x.js"`, true},
	}
	for _, tc := range cases {
		if got := hasSrcAttr([]byte(strings.ToLower(tc.attrs))); got != tc.want {
			t.Errorf("hasSrcAttr(%q)=%v, want %v", tc.attrs, got, tc.want)
		}
	}
}

func TestIndexCSPInjectsHashes(t *testing.T) {
	body := "boot();"
	html := "<html><body><script>" + body + "</script></body></html>"
	root := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte(html)}}

	got := indexCSP(root)
	want := "script-src 'self' 'sha256-" + hash(body) + "'"
	if !strings.Contains(got, want) {
		t.Fatalf("indexCSP missing %q\nfull: %s", want, got)
	}
	// Other directives must be preserved verbatim.
	for _, want := range []string{"default-src 'self'", "frame-ancestors 'none'", "form-action 'self'"} {
		if !strings.Contains(got, want) {
			t.Errorf("indexCSP missing directive %q", want)
		}
	}
}

func TestIndexCSPFallsBackWithoutIndex(t *testing.T) {
	if got := indexCSP(fstest.MapFS{}); got != baseCSP {
		t.Errorf("missing index.html should yield baseCSP, got %q", got)
	}
	if got := indexCSP(nil); got != baseCSP {
		t.Errorf("nil FS should yield baseCSP, got %q", got)
	}
	root := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html></html>")}}
	if got := indexCSP(root); got != baseCSP {
		t.Errorf("index without inline scripts should yield baseCSP, got %q", got)
	}
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return base64.StdEncoding.EncodeToString(sum[:])
}
