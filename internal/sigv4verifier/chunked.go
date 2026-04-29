// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package sigv4verifier

import (
	"bufio"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// ChunkVerifyError wraps any failure decoding or verifying a streaming body
// chunk. Errors from the underlying reader are returned verbatim.
type ChunkVerifyError struct {
	Reason string
}

func (e *ChunkVerifyError) Error() string { return "chunk verify: " + e.Reason }

// emptySHA256 is hex(sha256("")) — used as the canonical hash for chunk
// metadata in the per-chunk string-to-sign.
const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// NewChunkedReader returns an io.Reader that yields the decoded body bytes of
// an `aws-chunked` / STREAMING-AWS4-HMAC-SHA256-PAYLOAD stream, verifying
// every chunk signature as it goes. Reads past the terminator chunk return
// io.EOF.
//
// seedSignature is the top-level request's hex SigV4 signature; signingKey
// is the derived SigningKey; region/service/date must match the top-level
// credential scope.
func NewChunkedReader(body io.Reader, signingKey []byte, seedSignature, region, service string, date time.Time) io.Reader {
	return &chunkedReader{
		br:         bufio.NewReader(body),
		signingKey: signingKey,
		prevSig:    seedSignature,
		region:     region,
		service:    service,
		date:       date,
	}
}

type chunkedReader struct {
	br         *bufio.Reader
	signingKey []byte
	prevSig    string
	region     string
	service    string
	date       time.Time

	pending []byte // current chunk's data not yet returned
	done    bool
	err     error
}

func (c *chunkedReader) Read(p []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	for len(c.pending) == 0 && !c.done {
		if err := c.readNextChunk(); err != nil {
			c.err = err
			return 0, err
		}
	}
	if c.done && len(c.pending) == 0 {
		return 0, io.EOF
	}
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}

// readNextChunk parses and verifies one chunk. On a zero-size chunk (the
// stream terminator) it marks the reader done.
func (c *chunkedReader) readNextChunk() error {
	header, err := c.br.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read chunk header: %w", err)
	}
	header = strings.TrimRight(header, "\r\n")
	size, sig, err := parseChunkHeader(header)
	if err != nil {
		return err
	}

	var data []byte
	if size > 0 {
		data = make([]byte, size)
		if _, err := io.ReadFull(c.br, data); err != nil {
			return fmt.Errorf("read chunk data: %w", err)
		}
	}
	// Per spec each chunk is terminated with CRLF.
	crlf := make([]byte, 2)
	if _, err := io.ReadFull(c.br, crlf); err != nil {
		return fmt.Errorf("read chunk terminator: %w", err)
	}
	if crlf[0] != '\r' || crlf[1] != '\n' {
		return &ChunkVerifyError{Reason: "missing chunk CRLF terminator"}
	}

	if err := c.verify(data, sig); err != nil {
		return err
	}
	c.prevSig = sig
	c.pending = data

	if size == 0 {
		c.done = true
	}
	return nil
}

// verify checks the per-chunk signature:
//
//	StringToSign = "AWS4-HMAC-SHA256-PAYLOAD" + "\n" +
//	               <X-Amz-Date> + "\n" +
//	               <CredentialScope> + "\n" +
//	               <prev-signature> + "\n" +
//	               hex(sha256("")) + "\n" +
//	               hex(sha256(chunk-data))
func (c *chunkedReader) verify(data []byte, presented string) error {
	payload := sha256.Sum256(data)
	scope := fmt.Sprintf("%s/%s/%s/aws4_request", c.date.UTC().Format("20060102"), c.region, c.service)
	sts := "AWS4-HMAC-SHA256-PAYLOAD\n" +
		c.date.UTC().Format("20060102T150405Z") + "\n" +
		scope + "\n" +
		c.prevSig + "\n" +
		emptySHA256 + "\n" +
		hex.EncodeToString(payload[:])

	expected := hex.EncodeToString(hmacSHA256(c.signingKey, []byte(sts)))
	if subtle.ConstantTimeCompare([]byte(expected), []byte(presented)) != 1 {
		return &ChunkVerifyError{Reason: "chunk signature mismatch"}
	}
	return nil
}

func parseChunkHeader(line string) (size int64, signature string, err error) {
	// Format: "<size-hex>;chunk-signature=<hex>"
	semi := strings.IndexByte(line, ';')
	if semi < 0 {
		return 0, "", &ChunkVerifyError{Reason: "chunk header missing signature extension"}
	}
	sizeHex := strings.TrimSpace(line[:semi])
	size, err = strconv.ParseInt(sizeHex, 16, 64)
	if err != nil || size < 0 {
		return 0, "", &ChunkVerifyError{Reason: "chunk size not a non-negative hex integer"}
	}
	ext := strings.TrimSpace(line[semi+1:])
	const prefix = "chunk-signature="
	if !strings.HasPrefix(ext, prefix) {
		return 0, "", &ChunkVerifyError{Reason: "chunk extension must be chunk-signature"}
	}
	signature = strings.ToLower(strings.TrimSpace(ext[len(prefix):]))
	if _, err := hex.DecodeString(signature); err != nil {
		return 0, "", &ChunkVerifyError{Reason: "chunk signature not hex"}
	}
	return size, signature, nil
}
