// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"strings"
)

// indexCSP returns the CSP value to apply when serving the SvelteKit shell.
// SvelteKit's static build emits two inline <script> blocks (a theme
// bootstrap and the module-import shim that pulls in content-hashed entry
// bundles); the second one's body changes every build, so we hash whatever
// shipped in the embedded dist/ at startup and fold the digests into
// script-src. Other routes keep baseCSP unchanged.
//
// Falls back to baseCSP when index.html can't be read or has no inline
// scripts — that's the path tests with an empty FrontendFS exercise.
func indexCSP(root fs.FS) string {
	if root == nil {
		return baseCSP
	}
	hashes, err := indexInlineScriptHashes(root)
	if err != nil || len(hashes) == 0 {
		return baseCSP
	}
	var tokens strings.Builder
	for _, h := range hashes {
		tokens.WriteString(" 'sha256-")
		tokens.WriteString(h)
		tokens.WriteByte('\'')
	}
	return strings.Replace(baseCSP, "script-src 'self'", "script-src 'self'"+tokens.String(), 1)
}

// indexInlineScriptHashes reads index.html out of root and returns base64
// SHA-256 digests of each inline <script> body in document order.
func indexInlineScriptHashes(root fs.FS) ([]string, error) {
	doc, err := fs.ReadFile(root, "index.html")
	if err != nil {
		return nil, err
	}
	return scanInlineScriptHashes(doc), nil
}

// scanInlineScriptHashes walks doc and computes a CSP hash for every inline
// <script>…</script> block — i.e. tags without a src= attribute. The bytes
// hashed are exactly the body bytes between '>' and '</', which is what
// browsers use for the script-src 'sha256-…' check.
func scanInlineScriptHashes(doc []byte) []string {
	var hashes []string
	lower := bytes.ToLower(doc)
	cursor := 0
	for cursor < len(doc) {
		idx := bytes.Index(lower[cursor:], []byte("<script"))
		if idx < 0 {
			break
		}
		tagInner := cursor + idx + len("<script")
		if tagInner >= len(doc) {
			break
		}
		// Reject false matches like <scriptlike> by requiring the next
		// byte to be a tag-name terminator.
		switch doc[tagInner] {
		case ' ', '\t', '\n', '\r', '>', '/':
		default:
			cursor = tagInner
			continue
		}
		gt := bytes.IndexByte(doc[tagInner:], '>')
		if gt < 0 {
			break
		}
		attrs := lower[tagInner : tagInner+gt]
		bodyStart := tagInner + gt + 1
		closeAt := bytes.Index(lower[bodyStart:], []byte("</script>"))
		if closeAt < 0 {
			break
		}
		bodyEnd := bodyStart + closeAt
		cursor = bodyEnd + len("</script>")

		if hasSrcAttr(attrs) {
			continue
		}
		sum := sha256.Sum256(doc[bodyStart:bodyEnd])
		hashes = append(hashes, base64.StdEncoding.EncodeToString(sum[:]))
	}
	return hashes
}

// hasSrcAttr reports whether the lowercased attribute span between
// "<script" and ">" carries a src= attribute. The leading whitespace check
// avoids matching data-src, x-src, etc.
func hasSrcAttr(attrs []byte) bool {
	for i := 0; i+3 < len(attrs); i++ {
		c := attrs[i]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			continue
		}
		if attrs[i+1] != 's' || attrs[i+2] != 'r' || attrs[i+3] != 'c' {
			continue
		}
		j := i + 4
		for j < len(attrs) && (attrs[j] == ' ' || attrs[j] == '\t' || attrs[j] == '\n' || attrs[j] == '\r') {
			j++
		}
		if j < len(attrs) && attrs[j] == '=' {
			return true
		}
	}
	return false
}
