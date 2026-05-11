// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/go-logr/logr/testr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// postForm is the assembled multipart body and content type a test
// client would send for a POST Object form. The fields slice preserves
// insertion order so the `file` part lands last, as AWS requires.
type postForm struct {
	body        *bytes.Buffer
	contentType string
}

// buildSignedPostForm constructs a POST Object form with a real SigV4
// policy signature. The conditions slice carries raw JSON for each
// condition so callers can mix object and array forms freely. extra
// fields are appended verbatim (lowercased keys recommended).
func buildSignedPostForm(t *testing.T, akid, secret, bucket, key string, conditions []string, extra map[string]string, fileContent []byte) postForm {
	t.Helper()

	now := time.Now().UTC()
	date := now.Format("20060102")
	stamp := now.Format("20060102T150405Z")
	region := "us-east-1"
	service := "s3"
	credential := akid + "/" + date + "/" + region + "/" + service + "/aws4_request"
	expiration := now.Add(1 * time.Hour).Format("2006-01-02T15:04:05.000Z")

	policyJSON := fmt.Sprintf(`{"expiration":"%s","conditions":[%s]}`, expiration, strings.Join(conditions, ","))
	policyB64 := base64.StdEncoding.EncodeToString([]byte(policyJSON))

	signingKey := derivePolicySigningKey(secret, date, region, service)
	mac := hmac.New(sha256.New, signingKey)
	mac.Write([]byte(policyB64))
	signature := hex.EncodeToString(mac.Sum(nil))

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)

	// Order matters: every non-file field first, file last.
	must := func(err error) {
		t.Helper()
		require.NoError(t, err)
	}
	must(mw.WriteField("key", key))
	for k, v := range extra {
		must(mw.WriteField(k, v))
	}
	must(mw.WriteField("x-amz-algorithm", "AWS4-HMAC-SHA256"))
	must(mw.WriteField("x-amz-credential", credential))
	must(mw.WriteField("x-amz-date", stamp))
	must(mw.WriteField("policy", policyB64))
	must(mw.WriteField("x-amz-signature", signature))

	fw, err := mw.CreateFormFile("file", "upload.bin")
	must(err)
	_, err = fw.Write(fileContent)
	must(err)
	must(mw.Close())

	_ = bucket // bucket isn't sent as a form field in our happy-path tests; route carries it
	return postForm{body: body, contentType: mw.FormDataContentType()}
}

// derivePolicySigningKey replicates the SigV4 signing-key chain. We
// don't import the verifier's unexported helper here so this test stays
// honest about how a client would compute the signature.
func derivePolicySigningKey(secret, date, region, service string) []byte {
	mac := func(k, m []byte) []byte {
		h := hmac.New(sha256.New, k)
		h.Write(m)
		return h.Sum(nil)
	}
	kDate := mac([]byte("AWS4"+secret), []byte(date))
	kRegion := mac(kDate, []byte(region))
	kService := mac(kRegion, []byte(service))
	return mac(kService, []byte("aws4_request"))
}

const (
	testPostAKID   = "AKIATESTPOST00000000"
	testPostSecret = "s3cr3ts3cr3ts3cr3ts3cr3ts3cr3ts3cr3ts3cr"
	testPostBucket = "uploads"
	testBackend    = "primary"
)

func newPostCred() *VirtualCredential {
	return newCred(testPostAKID, testPostSecret, testPostBucket, testBackend)
}

func TestPostObject_Happy(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newTestServer(t, ups, newPostCred())
	defer proxy.Close()

	content := []byte("the contents of the uploaded file")
	form := buildSignedPostForm(t, testPostAKID, testPostSecret, testPostBucket,
		"u/eric/photo.png",
		[]string{
			`{"bucket":"` + testPostBucket + `"}`,
			`["starts-with","$key","u/eric/"]`,
			`["content-length-range",0,1048576]`,
			`{"Content-Type":"image/png"}`,
		},
		map[string]string{
			"Content-Type": "image/png",
		},
		content)

	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/"+testPostBucket, form.body)
	req.Header.Set("Content-Type", form.contentType)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusNoContent, resp.StatusCode, "body=%q", string(respBody))

	// Confirm the upstream received the file with the right key and content.
	getURL := ups.URL + "/" + testPostBucket + "/u/eric/photo.png"
	getResp, err := http.Get(getURL) //nolint:gosec // test fixture URL composed from controlled inputs
	require.NoError(t, err)
	defer getResp.Body.Close()
	got, _ := io.ReadAll(getResp.Body)
	require.Equal(t, content, got)
}

func TestPostObject_SuccessActionStatus200(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newTestServer(t, ups, newPostCred())
	defer proxy.Close()

	form := buildSignedPostForm(t, testPostAKID, testPostSecret, testPostBucket,
		"k", []string{
			`{"bucket":"` + testPostBucket + `"}`,
			`{"key":"k"}`,
			`{"success_action_status":"200"}`,
		},
		map[string]string{"success_action_status": "200"},
		[]byte("x"))

	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/"+testPostBucket, form.body)
	req.Header.Set("Content-Type", form.contentType)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestPostObject_SuccessActionStatus201(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newTestServer(t, ups, newPostCred())
	defer proxy.Close()

	form := buildSignedPostForm(t, testPostAKID, testPostSecret, testPostBucket,
		"keyname", []string{
			`{"bucket":"` + testPostBucket + `"}`,
			`{"key":"keyname"}`,
			`{"success_action_status":"201"}`,
		},
		map[string]string{"success_action_status": "201"},
		[]byte("x"))

	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/"+testPostBucket, form.body)
	req.Header.Set("Content-Type", form.contentType)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var pr postObjectResponse
	require.NoError(t, xml.NewDecoder(resp.Body).Decode(&pr))
	require.Equal(t, testPostBucket, pr.Bucket)
	require.Equal(t, "keyname", pr.Key)
	// Location should point back at the proxy (the inbound host),
	// not at the upstream backend.
	proxyHost, _ := url.Parse(proxy.URL)
	require.Equal(t, proxy.URL+"/"+testPostBucket+"/keyname", pr.Location,
		"proxy URL %s; location %s", proxyHost.Host, pr.Location)
}

func TestPostObject_Location_HonorsXForwardedProto(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newTestServer(t, ups, newPostCred())
	defer proxy.Close()

	form := buildSignedPostForm(t, testPostAKID, testPostSecret, testPostBucket,
		"k", []string{
			`{"bucket":"` + testPostBucket + `"}`,
			`{"key":"k"}`,
			`{"success_action_status":"201"}`,
		},
		map[string]string{"success_action_status": "201"},
		[]byte("x"))

	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/"+testPostBucket, form.body)
	req.Header.Set("Content-Type", form.contentType)
	// Simulate a TLS-terminating ingress in front of the proxy: the
	// inbound TCP connection is plain HTTP but the client used HTTPS.
	req.Header.Set("X-Forwarded-Proto", "https")
	// Pretend the client reached us at a public hostname.
	req.Host = "uploads.example.com"
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	var pr postObjectResponse
	require.NoError(t, xml.NewDecoder(resp.Body).Decode(&pr))
	require.Equal(t, "https://uploads.example.com/"+testPostBucket+"/k", pr.Location)
}

func TestPostObject_Location_VirtualHosted(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	// Configure the proxy with a host suffix so b.uploads.test routes
	// as bucket=b virtual-hosted.
	src := &fakeSource{
		byAKID: map[string]*VirtualCredential{testPostAKID: newPostCred()},
		byAnon: map[string]*AnonymousBinding{},
	}
	src.byAKID[testPostAKID].BucketScopes = []BucketScope{{BucketName: "b", BackendName: testBackend}}
	br := NewBackendResolver(&stubBackendLookup{endpointURL: ups.URL})
	srv := NewServer(Config{
		Source:        src,
		Backends:      br,
		Limiter:       NewLimiter(0, 0),
		IPLimiter:     NewIPLimiter(0),
		Metrics:       NewMetrics(prometheus.NewRegistry()),
		Log:           testr.New(t),
		HostSuffixes:  []string{"uploads.test"},
		BucketCreated: time.Now(),
		AdminCredsOverride: func(_ context.Context, _ BackendSpec) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "admin", SecretAccessKey: "secret"}, nil
		},
	})
	proxy := httptest.NewServer(srv)
	defer proxy.Close()

	form := buildSignedPostForm(t, testPostAKID, testPostSecret, "b",
		"file.bin", []string{
			`{"bucket":"b"}`,
			`{"key":"file.bin"}`,
			`{"success_action_status":"201"}`,
		},
		map[string]string{"success_action_status": "201"},
		[]byte("x"))

	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/", form.body)
	req.Header.Set("Content-Type", form.contentType)
	req.Host = "b.uploads.test"
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	var pr postObjectResponse
	require.NoError(t, xml.NewDecoder(resp.Body).Decode(&pr))
	require.Equal(t, "http://b.uploads.test/file.bin", pr.Location,
		"virtual-hosted location should be host-only, no bucket prefix in path")
}

func TestPostObject_SuccessActionRedirect(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newTestServer(t, ups, newPostCred())
	defer proxy.Close()

	form := buildSignedPostForm(t, testPostAKID, testPostSecret, testPostBucket,
		"keyname", []string{
			`{"bucket":"` + testPostBucket + `"}`,
			`{"key":"keyname"}`,
			`{"success_action_redirect":"https://app.example.com/done"}`,
		},
		map[string]string{"success_action_redirect": "https://app.example.com/done"},
		[]byte("x"))

	// http.Client follows redirects by default; suppress for inspection.
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/"+testPostBucket, form.body)
	req.Header.Set("Content-Type", form.contentType)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "app.example.com", loc.Host)
	require.Equal(t, testPostBucket, loc.Query().Get("bucket"))
	require.Equal(t, "keyname", loc.Query().Get("key"))
}

func TestPostObject_RejectsNonHTTPRedirect(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newTestServer(t, ups, newPostCred())
	defer proxy.Close()

	form := buildSignedPostForm(t, testPostAKID, testPostSecret, testPostBucket,
		"k", []string{
			`{"bucket":"` + testPostBucket + `"}`,
			`{"key":"k"}`,
			`{"success_action_redirect":"javascript:alert(1)"}`,
		},
		map[string]string{"success_action_redirect": "javascript:alert(1)"},
		[]byte("x"))

	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/"+testPostBucket, form.body)
	req.Header.Set("Content-Type", form.contentType)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPostObject_StartsWithViolation(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newTestServer(t, ups, newPostCred())
	defer proxy.Close()

	form := buildSignedPostForm(t, testPostAKID, testPostSecret, testPostBucket,
		// key violates the starts-with prefix
		"u/alice/photo.png",
		[]string{
			`{"bucket":"` + testPostBucket + `"}`,
			`["starts-with","$key","u/eric/"]`,
		},
		nil,
		[]byte("x"))

	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/"+testPostBucket, form.body)
	req.Header.Set("Content-Type", form.contentType)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestPostObject_ExpiredPolicy(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newTestServer(t, ups, newPostCred())
	defer proxy.Close()

	now := time.Now().UTC()
	date := now.Format("20060102")
	stamp := now.Format("20060102T150405Z")
	credential := testPostAKID + "/" + date + "/us-east-1/s3/aws4_request"
	// Already-expired policy.
	expired := now.Add(-1 * time.Hour).Format("2006-01-02T15:04:05.000Z")
	policyJSON := fmt.Sprintf(`{"expiration":"%s","conditions":[{"bucket":"%s"},{"key":"k"}]}`, expired, testPostBucket)
	policyB64 := base64.StdEncoding.EncodeToString([]byte(policyJSON))
	sk := derivePolicySigningKey(testPostSecret, date, "us-east-1", "s3")
	mac := hmac.New(sha256.New, sk)
	mac.Write([]byte(policyB64))
	sig := hex.EncodeToString(mac.Sum(nil))

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	_ = mw.WriteField("key", "k")
	_ = mw.WriteField("x-amz-algorithm", "AWS4-HMAC-SHA256")
	_ = mw.WriteField("x-amz-credential", credential)
	_ = mw.WriteField("x-amz-date", stamp)
	_ = mw.WriteField("policy", policyB64)
	_ = mw.WriteField("x-amz-signature", sig)
	fw, _ := mw.CreateFormFile("file", "f")
	_, _ = fw.Write([]byte("x"))
	_ = mw.Close()

	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/"+testPostBucket, body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestPostObject_TamperedSignature(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newTestServer(t, ups, newPostCred())
	defer proxy.Close()

	form := buildSignedPostForm(t, testPostAKID, testPostSecret, testPostBucket,
		"k", []string{
			`{"bucket":"` + testPostBucket + `"}`,
			`{"key":"k"}`,
		},
		nil,
		[]byte("x"))

	// Replace the signature field in the assembled body with garbage.
	// We rebuild the body rather than trying to surgically edit bytes.
	tamperedBody := strings.Replace(form.body.String(),
		"x-amz-signature", "x-amz-signature_DROP", 1)
	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/"+testPostBucket,
		bytes.NewReader([]byte(tamperedBody)))
	req.Header.Set("Content-Type", form.contentType)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestPostObject_UnknownAKID(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newTestServer(t, ups, newPostCred())
	defer proxy.Close()

	form := buildSignedPostForm(t, "AKIANOTREGISTERED000", testPostSecret, testPostBucket,
		"k", []string{
			`{"bucket":"` + testPostBucket + `"}`,
			`{"key":"k"}`,
		},
		nil,
		[]byte("x"))

	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/"+testPostBucket, form.body)
	req.Header.Set("Content-Type", form.contentType)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestPostObject_ScopeViolation(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	// Credential scoped only to "uploads".
	proxy := newTestServer(t, ups, newPostCred())
	defer proxy.Close()

	form := buildSignedPostForm(t, testPostAKID, testPostSecret, "other-bucket",
		"k", []string{
			`{"bucket":"other-bucket"}`,
			`{"key":"k"}`,
		},
		nil,
		[]byte("x"))

	// Direct the POST at the bucket the policy permits, but the credential
	// isn't scoped there.
	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/other-bucket", form.body)
	req.Header.Set("Content-Type", form.contentType)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestPostObject_MissingFileField(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newTestServer(t, ups, newPostCred())
	defer proxy.Close()

	// Build a form with everything except the file part.
	now := time.Now().UTC()
	date := now.Format("20060102")
	stamp := now.Format("20060102T150405Z")
	credential := testPostAKID + "/" + date + "/us-east-1/s3/aws4_request"
	policyJSON := fmt.Sprintf(`{"expiration":"%s","conditions":[{"bucket":"%s"},{"key":"k"}]}`,
		now.Add(time.Hour).Format("2006-01-02T15:04:05.000Z"), testPostBucket)
	policyB64 := base64.StdEncoding.EncodeToString([]byte(policyJSON))
	sk := derivePolicySigningKey(testPostSecret, date, "us-east-1", "s3")
	mac := hmac.New(sha256.New, sk)
	mac.Write([]byte(policyB64))
	sig := hex.EncodeToString(mac.Sum(nil))

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	_ = mw.WriteField("key", "k")
	_ = mw.WriteField("x-amz-algorithm", "AWS4-HMAC-SHA256")
	_ = mw.WriteField("x-amz-credential", credential)
	_ = mw.WriteField("x-amz-date", stamp)
	_ = mw.WriteField("policy", policyB64)
	_ = mw.WriteField("x-amz-signature", sig)
	_ = mw.Close()

	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/"+testPostBucket, body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPostObject_BucketFieldMismatch(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newTestServer(t, ups, newPostCred())
	defer proxy.Close()

	// Form's bucket field says "uploads" (matching the URL), but the
	// caller's URL is "uploads" and form is "different" — should reject.
	form := buildSignedPostForm(t, testPostAKID, testPostSecret, testPostBucket,
		"k", []string{
			`{"bucket":"different"}`,
			`{"key":"k"}`,
		},
		map[string]string{"bucket": "different"},
		[]byte("x"))

	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/"+testPostBucket, form.body)
	req.Header.Set("Content-Type", form.contentType)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestPostObject_FileTooLarge(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newTestServer(t, ups, newPostCred())
	defer proxy.Close()

	// Policy caps file at 8 bytes; send 32.
	form := buildSignedPostForm(t, testPostAKID, testPostSecret, testPostBucket,
		"k", []string{
			`{"bucket":"` + testPostBucket + `"}`,
			`{"key":"k"}`,
			`["content-length-range",0,8]`,
		},
		nil,
		bytes.Repeat([]byte("x"), 32))

	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/"+testPostBucket, form.body)
	req.Header.Set("Content-Type", form.contentType)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
}

func TestPostObject_FileTooSmall(t *testing.T) {
	ups := newUpstream()
	upsSrv := httptest.NewServer(ups)
	defer upsSrv.Close()
	proxy := newTestServer(t, upsSrv, newPostCred())
	defer proxy.Close()

	// Policy requires min 16; send 4.
	form := buildSignedPostForm(t, testPostAKID, testPostSecret, testPostBucket,
		"k", []string{
			`{"bucket":"` + testPostBucket + `"}`,
			`{"key":"k"}`,
			`["content-length-range",16,1024]`,
		},
		nil,
		[]byte("abcd"))

	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/"+testPostBucket, form.body)
	req.Header.Set("Content-Type", form.contentType)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// The orphan should have been deleted upstream after the failure.
	ups.mu.Lock()
	defer ups.mu.Unlock()
	_, exists := ups.objs[testPostBucket+"/k"]
	require.False(t, exists, "undersized object should be cleaned up upstream")
}

func TestPostObject_ContentTypeForwardedUpstream(t *testing.T) {
	var got http.Header
	ups := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			got = r.Header.Clone()
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer ups.Close()
	proxy := newTestServer(t, ups, newPostCred())
	defer proxy.Close()

	form := buildSignedPostForm(t, testPostAKID, testPostSecret, testPostBucket,
		"k", []string{
			`{"bucket":"` + testPostBucket + `"}`,
			`{"key":"k"}`,
			`{"Content-Type":"image/jpeg"}`,
			`["starts-with","$x-amz-meta-owner",""]`,
		},
		map[string]string{
			"Content-Type":     "image/jpeg",
			"x-amz-meta-owner": "alice",
		},
		[]byte("data"))

	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/"+testPostBucket, form.body)
	req.Header.Set("Content-Type", form.contentType)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	require.Equal(t, "image/jpeg", got.Get("Content-Type"))
	require.Equal(t, "alice", got.Get("X-Amz-Meta-Owner"))
}

func TestPostObject_QuotaExceeded(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()

	srv := newTestProxyWithQuota(t, ups, newPostCred(), alwaysFullQuota{})
	defer srv.Close()

	form := buildSignedPostForm(t, testPostAKID, testPostSecret, testPostBucket,
		"k", []string{
			`{"bucket":"` + testPostBucket + `"}`,
			`{"key":"k"}`,
			`["content-length-range",0,1024]`,
		},
		nil,
		[]byte("hi"))

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/"+testPostBucket, form.body)
	req.Header.Set("Content-Type", form.contentType)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusInsufficientStorage, resp.StatusCode)
}

func TestPostObject_FilenamePlaceholder(t *testing.T) {
	ups := httptest.NewServer(newUpstream())
	defer ups.Close()
	proxy := newTestServer(t, ups, newPostCred())
	defer proxy.Close()

	// Policy uses starts-with to allow any key under uploads/, and the
	// form's key field is "uploads/${filename}". The handler should
	// expand the placeholder with the file part's filename.
	form := buildSignedPostForm(t, testPostAKID, testPostSecret, testPostBucket,
		"uploads/${filename}",
		[]string{
			`{"bucket":"` + testPostBucket + `"}`,
			`["starts-with","$key","uploads/"]`,
		},
		nil,
		[]byte("file-bytes"))

	req, _ := http.NewRequest(http.MethodPost, proxy.URL+"/"+testPostBucket, form.body)
	req.Header.Set("Content-Type", form.contentType)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	// upload.bin is the filename buildSignedPostForm uses for the file part.
	getResp, err := http.Get(ups.URL + "/" + testPostBucket + "/uploads/upload.bin") //nolint:gosec
	require.NoError(t, err)
	defer getResp.Body.Close()
	got, _ := io.ReadAll(getResp.Body)
	require.Equal(t, "file-bytes", string(got))
}

// alwaysFullQuota implements QuotaEnforcer; CheckUpload always denies.
type alwaysFullQuota struct{}

func (alwaysFullQuota) CheckUpload(_ context.Context, _, _ string, _ int64) error {
	return fmt.Errorf("quota exceeded for test")
}
func (alwaysFullQuota) Recorded(_, _ string, _ int64) {}
