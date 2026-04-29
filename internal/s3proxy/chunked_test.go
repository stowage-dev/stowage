// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/stretchr/testify/require"

	"github.com/stowage-dev/stowage/internal/sigv4verifier"
)

// buildChunkedAt produces an aws-chunked request body using the supplied
// signingKey and seed signature. The returned bytes are what the wire would
// see for STREAMING-AWS4-HMAC-SHA256-PAYLOAD.
func buildChunkedAt(signingKey []byte, seedSig, region, service string, date time.Time, chunks [][]byte) []byte {
	var out bytes.Buffer
	prev := seedSig
	for _, data := range chunks {
		payloadHash := sha256.Sum256(data)
		scope := fmt.Sprintf("%s/%s/%s/aws4_request", date.UTC().Format("20060102"), region, service)
		sts := "AWS4-HMAC-SHA256-PAYLOAD\n" +
			date.UTC().Format("20060102T150405Z") + "\n" +
			scope + "\n" +
			prev + "\n" +
			"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855\n" +
			hex.EncodeToString(payloadHash[:])
		h := hmac256(signingKey, []byte(sts))
		sig := hex.EncodeToString(h)
		fmt.Fprintf(&out, "%x;chunk-signature=%s\r\n", len(data), sig)
		out.Write(data)
		out.WriteString("\r\n")
		prev = sig
	}
	// terminator
	payloadHash := sha256.Sum256(nil)
	scope := fmt.Sprintf("%s/%s/%s/aws4_request", date.UTC().Format("20060102"), region, service)
	sts := "AWS4-HMAC-SHA256-PAYLOAD\n" +
		date.UTC().Format("20060102T150405Z") + "\n" +
		scope + "\n" +
		prev + "\n" +
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855\n" +
		hex.EncodeToString(payloadHash[:])
	sig := hex.EncodeToString(hmac256(signingKey, []byte(sts)))
	fmt.Fprintf(&out, "0;chunk-signature=%s\r\n\r\n", sig)
	return out.Bytes()
}

func hmac256(key, data []byte) []byte {
	m := newHMAC(key)
	m.Write(data)
	return m.Sum(nil)
}

func TestProxy_ChunkedUpload(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()

	vc := &VirtualCredential{
		AccessKeyID:     "AKIACHUNKCANARY00000",
		SecretAccessKey: "chunkedsecretchunkedsecretchunkedsecretab",
		BackendName:     "primary",
		BucketScopes: []BucketScope{
			{BucketName: "chunked-bucket", BackendName: "primary"},
		},
	}
	proxy := newTestServer(t, ups, vc)
	defer proxy.Close()

	proxyURL, _ := url.Parse(proxy.URL)

	decoded := []byte("streaming upload body test payload x")
	chunks := [][]byte{decoded[:10], decoded[10:25], decoded[25:]}

	// Sign a top-level PUT with X-Amz-Content-Sha256 = STREAMING so we can
	// learn the signing key + seed signature that the chunks must chain to.
	putURL := fmt.Sprintf("%s/%s/streaming.bin", proxy.URL, vc.BucketScopes[0].BucketName)
	preReq, _ := http.NewRequest(http.MethodPut, putURL, nil)
	preReq.Host = proxyURL.Host
	preReq.Header.Set("X-Amz-Content-Sha256", sigv4verifier.StreamingPayload)
	preReq.Header.Set("X-Amz-Decoded-Content-Length", strconv.Itoa(len(decoded)))
	signDate := time.Now().UTC()
	signer := v4.NewSigner()
	require.NoError(t, signer.SignHTTP(context.Background(),
		aws.Credentials{AccessKeyID: vc.AccessKeyID, SecretAccessKey: vc.SecretAccessKey},
		preReq, sigv4verifier.StreamingPayload, "s3", "us-east-1", signDate,
	))

	authHdr := preReq.Header.Get("Authorization")
	sigIdx := bytesIndex(authHdr, "Signature=")
	require.True(t, sigIdx >= 0)
	seedSig := authHdr[sigIdx+len("Signature="):]

	signingKey := deriveSigningKeyFor(vc.SecretAccessKey, signDate.Format("20060102"), "us-east-1", "s3")
	body := buildChunkedAt(signingKey, seedSig, "us-east-1", "s3", signDate, chunks)

	req, _ := http.NewRequest(http.MethodPut, putURL, bytes.NewReader(body))
	req.Host = proxyURL.Host
	for k, vs := range preReq.Header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.ContentLength = int64(len(body))

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equalf(t, http.StatusOK, resp.StatusCode, "body: %s", readAll(resp.Body))

	// Read back and verify the upstream saw the decoded body.
	getURL := fmt.Sprintf("%s/%s/streaming.bin", proxy.URL, vc.BucketScopes[0].BucketName)
	getReq, _ := http.NewRequest(http.MethodGet, getURL, nil)
	getReq.Host = proxyURL.Host
	signVirtual(t, getReq, vc.AccessKeyID, vc.SecretAccessKey, nil)
	resp2, err := http.DefaultClient.Do(getReq)
	require.NoError(t, err)
	defer resp2.Body.Close()
	require.Equal(t, http.StatusOK, resp2.StatusCode)
	got, _ := io.ReadAll(resp2.Body)
	require.Equal(t, string(decoded), string(got))
}

// --- small helpers local to the test file ---

func bytesIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// deriveSigningKeyFor recomputes the SigV4 signing key. Exposed locally
// because the verifier package's helper is unexported.
func deriveSigningKeyFor(secret, date, region, service string) []byte {
	k1 := hmac256([]byte("AWS4"+secret), []byte(date))
	k2 := hmac256(k1, []byte(region))
	k3 := hmac256(k2, []byte(service))
	return hmac256(k3, []byte("aws4_request"))
}
