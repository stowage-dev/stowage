// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sigv4verifier

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// buildChunk produces the wire format for a single SigV4 chunk and returns
// the signature so the caller can chain it into the next chunk.
func buildChunk(data []byte, signingKey []byte, prevSig, region, service string, date time.Time) ([]byte, string) {
	payloadHash := sha256.Sum256(data)
	scope := fmt.Sprintf("%s/%s/%s/aws4_request", date.UTC().Format("20060102"), region, service)
	sts := "AWS4-HMAC-SHA256-PAYLOAD\n" +
		date.UTC().Format("20060102T150405Z") + "\n" +
		scope + "\n" +
		prevSig + "\n" +
		emptySHA256 + "\n" +
		hex.EncodeToString(payloadHash[:])
	sig := hex.EncodeToString(hmacSHA256(signingKey, []byte(sts)))

	var b bytes.Buffer
	fmt.Fprintf(&b, "%x;chunk-signature=%s\r\n", len(data), sig)
	b.Write(data)
	b.WriteString("\r\n")
	return b.Bytes(), sig
}

func buildChunkedStream(signingKey []byte, seedSig, region, service string, date time.Time, chunks [][]byte) []byte {
	var out bytes.Buffer
	prev := seedSig
	for _, data := range chunks {
		b, sig := buildChunk(data, signingKey, prev, region, service, date)
		out.Write(b)
		prev = sig
	}
	// Terminator
	term, _ := buildChunk(nil, signingKey, prev, region, service, date)
	out.Write(term)
	return out.Bytes()
}

func TestChunkedReader_Happy(t *testing.T) {
	signingKey := deriveSigningKey(testSecret, "20260424", testRegion, testSvc)
	seed := hex.EncodeToString(hmacSHA256(signingKey, []byte("seed")))
	date := time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC)

	chunks := [][]byte{
		[]byte("the quick brown fox "),
		[]byte("jumps over "),
		[]byte("the lazy dog"),
	}
	stream := buildChunkedStream(signingKey, seed, testRegion, testSvc, date, chunks)

	r := NewChunkedReader(bytes.NewReader(stream), signingKey, seed, testRegion, testSvc, date)
	got, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "the quick brown fox jumps over the lazy dog", string(got))
}

func TestChunkedReader_TamperedSignature(t *testing.T) {
	signingKey := deriveSigningKey(testSecret, "20260424", testRegion, testSvc)
	seed := hex.EncodeToString(hmacSHA256(signingKey, []byte("seed")))
	date := time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC)

	stream := buildChunkedStream(signingKey, seed, testRegion, testSvc, date, [][]byte{[]byte("payload")})
	// Flip a byte in the signature field.
	tampered := []byte(strings.Replace(string(stream), "chunk-signature=", "chunk-signature=0", 1))

	r := NewChunkedReader(bytes.NewReader(tampered), signingKey, seed, testRegion, testSvc, date)
	_, err := io.ReadAll(r)
	require.Error(t, err)
}

func TestChunkedReader_TamperedBody(t *testing.T) {
	signingKey := deriveSigningKey(testSecret, "20260424", testRegion, testSvc)
	seed := hex.EncodeToString(hmacSHA256(signingKey, []byte("seed")))
	date := time.Date(2026, 4, 24, 0, 0, 0, 0, time.UTC)

	stream := buildChunkedStream(signingKey, seed, testRegion, testSvc, date, [][]byte{[]byte("payload")})
	// Overwrite the first body byte.
	idx := bytes.Index(stream, []byte("\r\n")) + 2
	stream[idx] = 'X'

	r := NewChunkedReader(bytes.NewReader(stream), signingKey, seed, testRegion, testSvc, date)
	_, err := io.ReadAll(r)
	require.Error(t, err)
}

func TestParseChunkHeader(t *testing.T) {
	_, _, err := parseChunkHeader("not a header")
	require.Error(t, err)

	_, _, err = parseChunkHeader("xxxx;chunk-signature=deadbeef")
	require.Error(t, err)

	_, _, err = parseChunkHeader("10;other=foo")
	require.Error(t, err)

	size, sig, err := parseChunkHeader("400;chunk-signature=0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	require.Equal(t, int64(0x400), size)
	require.Equal(t, "0123456789abcdef0123456789abcdef", sig)
}
