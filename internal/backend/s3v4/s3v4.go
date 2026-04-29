// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package s3v4 is the generic AWS SDK v2 driver. It talks raw S3 API and
// works against anything S3-compatible: AWS S3, MinIO, Garage, SeaweedFS,
// Backblaze B2, Wasabi, Cloudflare R2, etc.
//
// It does not implement the backend.AdminBackend interface — specialised
// admin drivers (minio, garage, seaweedfs) wrap this one and add that.
package s3v4

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/stowage-dev/stowage/internal/backend"
)

// Config is the wire shape that comes out of internal/config.BackendConfig
// once secrets have been resolved from env.
type Config struct {
	ID        string
	Name      string
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
	PathStyle bool
}

type Driver struct {
	cfg       Config
	client    *s3.Client
	presigner *s3.PresignClient
}

var _ backend.Backend = (*Driver)(nil)

// newSDKHTTPClient returns an *http.Client tuned for the proxy's S3 traffic
// pattern: many small concurrent requests (List/Head/Get) against a small
// number of upstreams. The Go standard library default uses
// MaxIdleConnsPerHost=2, which forces dial+TLS on most calls under any real
// concurrency — that's the dominant tail-latency source for the bench's
// /buckets, /objects, and HEAD /object rows.
//
// Numbers below are sized for the spec's 1 CPU / 200 MiB envelope plus
// headroom; they're well below socket exhaustion on any sensible host.
func newSDKHTTPClient() *http.Client {
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          256,
		MaxIdleConnsPerHost:   64,
		MaxConnsPerHost:       64,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	return &http.Client{Transport: tr}
}

func New(ctx context.Context, c Config) (*Driver, error) {
	if c.ID == "" {
		return nil, errors.New("s3v4: id is required")
	}
	if c.AccessKey == "" || c.SecretKey == "" {
		return nil, errors.New("s3v4: access and secret keys are required")
	}
	region := c.Region
	if region == "" {
		region = "us-east-1"
	}

	httpClient := newSDKHTTPClient()
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey, ""),
		),
		awsconfig.WithHTTPClient(httpClient),
		// The SDK's default (WhenSupported) tries to add a CRC32 header on
		// UploadPart/PutObject. With an unseekable streaming body over plain
		// HTTP it has no way to precompute the checksum, so the call fails.
		// WhenRequired keeps integrity for ops that mandate it without
		// breaking streaming uploads to S3-compatibles like MinIO/Garage.
		awsconfig.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
		awsconfig.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired),
	)
	if err != nil {
		return nil, fmt.Errorf("s3v4: load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if c.Endpoint != "" {
			o.BaseEndpoint = aws.String(c.Endpoint)
		}
		o.UsePathStyle = c.PathStyle
	})

	return &Driver{
		cfg:       c,
		client:    client,
		presigner: s3.NewPresignClient(client),
	}, nil
}

func (d *Driver) ID() string          { return d.cfg.ID }
func (d *Driver) DisplayName() string { return d.cfg.Name }

func (d *Driver) Capabilities() backend.Capabilities {
	// Conservative optimistic defaults for a generic S3. Per-target overrides
	// land when dedicated backends (minio, garage, seaweedfs) wrap this.
	return backend.Capabilities{
		Versioning:        true,
		ObjectLock:        true,
		Lifecycle:         true,
		BucketPolicy:      true,
		CORS:              true,
		Tagging:           true,
		ServerSideEncrypt: true,
		AdminAPI:          "",
		MaxMultipartParts: 10000,
		MaxPartSizeBytes:  5 * 1024 * 1024 * 1024, // 5 GiB — S3 per-part ceiling
	}
}

// ---- Bucket ops ---------------------------------------------------------

func (d *Driver) ListBuckets(ctx context.Context) ([]backend.Bucket, error) {
	out, err := d.client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, err
	}
	res := make([]backend.Bucket, 0, len(out.Buckets))
	for _, b := range out.Buckets {
		bk := backend.Bucket{Name: aws.ToString(b.Name)}
		if b.CreationDate != nil {
			bk.CreatedAt = *b.CreationDate
		}
		res = append(res, bk)
	}
	return res, nil
}

func (d *Driver) CreateBucket(ctx context.Context, name, region string) error {
	in := &s3.CreateBucketInput{Bucket: aws.String(name)}
	// us-east-1 is the only region that must NOT carry a LocationConstraint.
	if region != "" && region != "us-east-1" {
		in.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
			LocationConstraint: s3types.BucketLocationConstraint(region),
		}
	}
	_, err := d.client.CreateBucket(ctx, in)
	return err
}

func (d *Driver) DeleteBucket(ctx context.Context, name string) error {
	_, err := d.client.DeleteBucket(ctx, &s3.DeleteBucketInput{Bucket: aws.String(name)})
	return err
}

func (d *Driver) HeadBucket(ctx context.Context, name string) (backend.BucketInfo, error) {
	_, err := d.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(name)})
	if err != nil {
		return backend.BucketInfo{Name: name, Exists: false}, err
	}
	return backend.BucketInfo{Name: name, Exists: true}, nil
}

// ---- Bucket config ------------------------------------------------------

func (d *Driver) GetBucketVersioning(ctx context.Context, bucket string) (bool, error) {
	out, err := d.client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: aws.String(bucket)})
	if err != nil {
		return false, err
	}
	return out.Status == s3types.BucketVersioningStatusEnabled, nil
}

func (d *Driver) SetBucketVersioning(ctx context.Context, bucket string, enabled bool) error {
	status := s3types.BucketVersioningStatusSuspended
	if enabled {
		status = s3types.BucketVersioningStatusEnabled
	}
	_, err := d.client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(bucket),
		VersioningConfiguration: &s3types.VersioningConfiguration{
			Status: status,
		},
	})
	return err
}

func (d *Driver) GetBucketCORS(ctx context.Context, bucket string) ([]backend.CORSRule, error) {
	out, err := d.client.GetBucketCors(ctx, &s3.GetBucketCorsInput{Bucket: aws.String(bucket)})
	if err != nil {
		return nil, err
	}
	rules := make([]backend.CORSRule, 0, len(out.CORSRules))
	for _, r := range out.CORSRules {
		rules = append(rules, backend.CORSRule{
			AllowedOrigins: r.AllowedOrigins,
			AllowedMethods: r.AllowedMethods,
			AllowedHeaders: r.AllowedHeaders,
			ExposeHeaders:  r.ExposeHeaders,
			MaxAgeSeconds:  int(aws.ToInt32(r.MaxAgeSeconds)),
		})
	}
	return rules, nil
}

func (d *Driver) SetBucketCORS(ctx context.Context, bucket string, rules []backend.CORSRule) error {
	cors := make([]s3types.CORSRule, 0, len(rules))
	for _, r := range rules {
		age := int32(r.MaxAgeSeconds)
		cors = append(cors, s3types.CORSRule{
			AllowedOrigins: r.AllowedOrigins,
			AllowedMethods: r.AllowedMethods,
			AllowedHeaders: r.AllowedHeaders,
			ExposeHeaders:  r.ExposeHeaders,
			MaxAgeSeconds:  &age,
		})
	}
	_, err := d.client.PutBucketCors(ctx, &s3.PutBucketCorsInput{
		Bucket:            aws.String(bucket),
		CORSConfiguration: &s3types.CORSConfiguration{CORSRules: cors},
	})
	return err
}

func (d *Driver) GetBucketPolicy(ctx context.Context, bucket string) (string, error) {
	out, err := d.client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(bucket)})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.Policy), nil
}

func (d *Driver) SetBucketPolicy(ctx context.Context, bucket string, policy string) error {
	_, err := d.client.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(bucket),
		Policy: aws.String(policy),
	})
	return err
}

func (d *Driver) DeleteBucketPolicy(ctx context.Context, bucket string) error {
	_, err := d.client.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{Bucket: aws.String(bucket)})
	return err
}

func (d *Driver) GetBucketLifecycle(ctx context.Context, bucket string) ([]backend.LifecycleRule, error) {
	out, err := d.client.GetBucketLifecycleConfiguration(ctx, &s3.GetBucketLifecycleConfigurationInput{Bucket: aws.String(bucket)})
	if err != nil {
		return nil, err
	}
	rules := make([]backend.LifecycleRule, 0, len(out.Rules))
	for _, r := range out.Rules {
		rule := backend.LifecycleRule{
			ID:      aws.ToString(r.ID),
			Enabled: r.Status == s3types.ExpirationStatusEnabled,
		}
		if r.Filter != nil {
			rule.Prefix = aws.ToString(r.Filter.Prefix)
		}
		if r.Expiration != nil {
			rule.ExpirationDays = int(aws.ToInt32(r.Expiration.Days))
		}
		if r.NoncurrentVersionExpiration != nil {
			rule.NoncurrentExpireDays = int(aws.ToInt32(r.NoncurrentVersionExpiration.NoncurrentDays))
		}
		if r.AbortIncompleteMultipartUpload != nil {
			rule.AbortIncompleteDays = int(aws.ToInt32(r.AbortIncompleteMultipartUpload.DaysAfterInitiation))
		}
		if len(r.Transitions) > 0 {
			t := r.Transitions[0]
			rule.TransitionDays = int(aws.ToInt32(t.Days))
			rule.TransitionStorageClass = string(t.StorageClass)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

func (d *Driver) SetBucketLifecycle(ctx context.Context, bucket string, rules []backend.LifecycleRule) error {
	converted := make([]s3types.LifecycleRule, 0, len(rules))
	for _, r := range rules {
		status := s3types.ExpirationStatusDisabled
		if r.Enabled {
			status = s3types.ExpirationStatusEnabled
		}
		rule := s3types.LifecycleRule{
			ID:     aws.String(r.ID),
			Status: status,
			Filter: &s3types.LifecycleRuleFilter{Prefix: aws.String(r.Prefix)},
		}
		if r.ExpirationDays > 0 {
			days := int32(r.ExpirationDays)
			rule.Expiration = &s3types.LifecycleExpiration{Days: &days}
		}
		if r.NoncurrentExpireDays > 0 {
			days := int32(r.NoncurrentExpireDays)
			rule.NoncurrentVersionExpiration = &s3types.NoncurrentVersionExpiration{NoncurrentDays: &days}
		}
		if r.AbortIncompleteDays > 0 {
			days := int32(r.AbortIncompleteDays)
			rule.AbortIncompleteMultipartUpload = &s3types.AbortIncompleteMultipartUpload{DaysAfterInitiation: &days}
		}
		if r.TransitionDays > 0 && r.TransitionStorageClass != "" {
			days := int32(r.TransitionDays)
			rule.Transitions = []s3types.Transition{{
				Days:         &days,
				StorageClass: s3types.TransitionStorageClass(r.TransitionStorageClass),
			}}
		}
		converted = append(converted, rule)
	}
	_, err := d.client.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket:                 aws.String(bucket),
		LifecycleConfiguration: &s3types.BucketLifecycleConfiguration{Rules: converted},
	})
	return err
}

// ---- Object ops ---------------------------------------------------------

func (d *Driver) ListObjects(ctx context.Context, req backend.ListObjectsRequest) (backend.ListObjectsResult, error) {
	in := &s3.ListObjectsV2Input{
		Bucket: aws.String(req.Bucket),
	}
	if req.Prefix != "" {
		in.Prefix = aws.String(req.Prefix)
	}
	if req.Delimiter != "" {
		in.Delimiter = aws.String(req.Delimiter)
	}
	if req.ContinuationToken != "" {
		in.ContinuationToken = aws.String(req.ContinuationToken)
	}
	if req.MaxKeys > 0 {
		n := int32(req.MaxKeys)
		in.MaxKeys = &n
	}
	out, err := d.client.ListObjectsV2(ctx, in)
	if err != nil {
		return backend.ListObjectsResult{}, err
	}
	res := backend.ListObjectsResult{
		IsTruncated:           aws.ToBool(out.IsTruncated),
		NextContinuationToken: aws.ToString(out.NextContinuationToken),
	}
	for _, cp := range out.CommonPrefixes {
		res.CommonPrefixes = append(res.CommonPrefixes, aws.ToString(cp.Prefix))
	}
	for _, o := range out.Contents {
		info := backend.ObjectInfo{
			Key:          aws.ToString(o.Key),
			Size:         aws.ToInt64(o.Size),
			ETag:         aws.ToString(o.ETag),
			StorageClass: string(o.StorageClass),
		}
		if o.LastModified != nil {
			info.LastModified = *o.LastModified
		}
		res.Objects = append(res.Objects, info)
	}
	return res, nil
}

func (d *Driver) GetObject(ctx context.Context, bucket, key, versionID string, rng *backend.Range) (backend.ObjectReader, error) {
	in := &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if versionID != "" {
		in.VersionId = aws.String(versionID)
	}
	if rng != nil {
		in.Range = aws.String(formatRange(*rng))
	}
	out, err := d.client.GetObject(ctx, in)
	if err != nil {
		return nil, err
	}
	info := backend.ObjectInfo{
		Key:          key,
		Size:         aws.ToInt64(out.ContentLength),
		ETag:         aws.ToString(out.ETag),
		ContentType:  aws.ToString(out.ContentType),
		StorageClass: string(out.StorageClass),
		VersionID:    aws.ToString(out.VersionId),
		Metadata:     out.Metadata,
	}
	if out.LastModified != nil {
		info.LastModified = *out.LastModified
	}
	return &objectReader{body: out.Body, info: info}, nil
}

func (d *Driver) HeadObject(ctx context.Context, bucket, key, versionID string) (backend.ObjectInfo, error) {
	in := &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if versionID != "" {
		in.VersionId = aws.String(versionID)
	}
	out, err := d.client.HeadObject(ctx, in)
	if err != nil {
		return backend.ObjectInfo{}, err
	}
	info := backend.ObjectInfo{
		Key:          key,
		Size:         aws.ToInt64(out.ContentLength),
		ETag:         aws.ToString(out.ETag),
		ContentType:  aws.ToString(out.ContentType),
		StorageClass: string(out.StorageClass),
		VersionID:    aws.ToString(out.VersionId),
		Metadata:     out.Metadata,
	}
	if out.LastModified != nil {
		info.LastModified = *out.LastModified
	}
	return info, nil
}

func (d *Driver) PutObject(ctx context.Context, req backend.PutObjectRequest) (backend.ObjectInfo, error) {
	in := &s3.PutObjectInput{
		Bucket: aws.String(req.Bucket),
		Key:    aws.String(req.Key),
		Body:   req.Body,
	}
	if req.Size > 0 {
		in.ContentLength = aws.Int64(req.Size)
	}
	if req.ContentType != "" {
		in.ContentType = aws.String(req.ContentType)
	}
	if len(req.Metadata) > 0 {
		in.Metadata = req.Metadata
	}

	// SigV4 hashes the payload then rewinds the stream, and the retry
	// middleware rewinds again on every attempt > 1. Both require an
	// io.Seeker. A body from an upstream HTTP response (cross-backend
	// transfer) doesn't satisfy that, so spool it to a temp file first
	// and hand the seekable *os.File to the SDK. Memory stays bounded
	// to the io.Copy buffer; the file is unlinked when PutObject returns.
	if _, seekable := req.Body.(io.Seeker); !seekable && req.Body != nil {
		spooled, n, cleanup, err := spoolToTempFile(req.Body)
		if err != nil {
			return backend.ObjectInfo{}, fmt.Errorf("s3v4: spool body: %w", err)
		}
		defer cleanup()
		in.Body = spooled
		in.ContentLength = aws.Int64(n)
		req.Size = n
	}

	out, err := d.client.PutObject(ctx, in)
	if err != nil {
		return backend.ObjectInfo{}, err
	}
	return backend.ObjectInfo{
		Key:         req.Key,
		Size:        req.Size,
		ETag:        aws.ToString(out.ETag),
		ContentType: req.ContentType,
		VersionID:   aws.ToString(out.VersionId),
		Metadata:    req.Metadata,
	}, nil
}

// spoolToTempFile drains r into an unlinked-on-cleanup tempfile and seeks
// back to start so the result satisfies io.ReadSeeker. Used to make the
// SDK's SigV4 + retry path work with bodies that aren't seekable on the
// wire (e.g. an upstream http.Response.Body).
func spoolToTempFile(r io.Reader) (*os.File, int64, func(), error) {
	f, err := os.CreateTemp("", "stowage-put-*")
	if err != nil {
		return nil, 0, nil, err
	}
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}
	n, err := io.Copy(f, r)
	if err != nil {
		cleanup()
		return nil, 0, nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, 0, nil, err
	}
	return f, n, cleanup, nil
}

func (d *Driver) DeleteObject(ctx context.Context, bucket, key, versionID string) error {
	in := &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if versionID != "" {
		in.VersionId = aws.String(versionID)
	}
	_, err := d.client.DeleteObject(ctx, in)
	return err
}

func (d *Driver) DeleteObjects(ctx context.Context, bucket string, keys []backend.ObjectIdentifier) (backend.DeleteObjectsResult, error) {
	const chunk = 1000 // S3 per-request limit
	var res backend.DeleteObjectsResult
	for start := 0; start < len(keys); start += chunk {
		end := start + chunk
		if end > len(keys) {
			end = len(keys)
		}
		batch := keys[start:end]
		ids := make([]s3types.ObjectIdentifier, 0, len(batch))
		for _, k := range batch {
			oid := s3types.ObjectIdentifier{Key: aws.String(k.Key)}
			if k.VersionID != "" {
				oid.VersionId = aws.String(k.VersionID)
			}
			ids = append(ids, oid)
		}
		out, err := d.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &s3types.Delete{Objects: ids, Quiet: aws.Bool(false)},
		})
		if err != nil {
			return res, err
		}
		for _, d := range out.Deleted {
			res.Deleted = append(res.Deleted, backend.ObjectIdentifier{
				Key:       aws.ToString(d.Key),
				VersionID: aws.ToString(d.VersionId),
			})
		}
		for _, e := range out.Errors {
			res.Errors = append(res.Errors, backend.DeleteError{
				Key:       aws.ToString(e.Key),
				VersionID: aws.ToString(e.VersionId),
				Code:      aws.ToString(e.Code),
				Message:   aws.ToString(e.Message),
			})
		}
	}
	return res, nil
}

func (d *Driver) CopyObject(ctx context.Context, src, dst backend.ObjectRef, metadata map[string]string) error {
	source := src.Bucket + "/" + src.Key
	if src.VersionID != "" {
		source += "?versionId=" + src.VersionID
	}
	in := &s3.CopyObjectInput{
		Bucket:     aws.String(dst.Bucket),
		Key:        aws.String(dst.Key),
		CopySource: aws.String(source),
	}
	if len(metadata) > 0 {
		in.Metadata = metadata
		in.MetadataDirective = s3types.MetadataDirectiveReplace
	}
	_, err := d.client.CopyObject(ctx, in)
	return err
}

func (d *Driver) GetObjectTagging(ctx context.Context, bucket, key, versionID string) (map[string]string, error) {
	in := &s3.GetObjectTaggingInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if versionID != "" {
		in.VersionId = aws.String(versionID)
	}
	out, err := d.client.GetObjectTagging(ctx, in)
	if err != nil {
		return nil, err
	}
	tags := make(map[string]string, len(out.TagSet))
	for _, t := range out.TagSet {
		tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return tags, nil
}

func (d *Driver) SetObjectTagging(ctx context.Context, bucket, key, versionID string, tags map[string]string) error {
	if len(tags) == 0 {
		in := &s3.DeleteObjectTaggingInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		}
		if versionID != "" {
			in.VersionId = aws.String(versionID)
		}
		_, err := d.client.DeleteObjectTagging(ctx, in)
		return err
	}
	set := make([]s3types.Tag, 0, len(tags))
	for k, v := range tags {
		set = append(set, s3types.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	in := &s3.PutObjectTaggingInput{
		Bucket:  aws.String(bucket),
		Key:     aws.String(key),
		Tagging: &s3types.Tagging{TagSet: set},
	}
	if versionID != "" {
		in.VersionId = aws.String(versionID)
	}
	_, err := d.client.PutObjectTagging(ctx, in)
	return err
}

func (d *Driver) ListObjectVersions(ctx context.Context, bucket, keyPrefix string) ([]backend.ObjectVersion, error) {
	in := &s3.ListObjectVersionsInput{
		Bucket: aws.String(bucket),
		Prefix: aws.String(keyPrefix),
	}
	var out []backend.ObjectVersion
	for {
		res, err := d.client.ListObjectVersions(ctx, in)
		if err != nil {
			return nil, err
		}
		for _, v := range res.Versions {
			ov := backend.ObjectVersion{
				Key:          aws.ToString(v.Key),
				VersionID:    aws.ToString(v.VersionId),
				IsLatest:     aws.ToBool(v.IsLatest),
				Size:         aws.ToInt64(v.Size),
				ETag:         aws.ToString(v.ETag),
				StorageClass: string(v.StorageClass),
			}
			if v.LastModified != nil {
				ov.LastModified = *v.LastModified
			}
			out = append(out, ov)
		}
		for _, m := range res.DeleteMarkers {
			ov := backend.ObjectVersion{
				Key:            aws.ToString(m.Key),
				VersionID:      aws.ToString(m.VersionId),
				IsLatest:       aws.ToBool(m.IsLatest),
				IsDeleteMarker: true,
			}
			if m.LastModified != nil {
				ov.LastModified = *m.LastModified
			}
			out = append(out, ov)
		}
		if !aws.ToBool(res.IsTruncated) {
			break
		}
		in.KeyMarker = res.NextKeyMarker
		in.VersionIdMarker = res.NextVersionIdMarker
	}
	return out, nil
}

// UpdateObjectMetadata rewrites user metadata via a self-copy with the
// REPLACE directive. S3 has no in-place metadata mutation — this is the
// portable pattern. Creates a new version on versioned buckets.
func (d *Driver) UpdateObjectMetadata(ctx context.Context, bucket, key string, metadata map[string]string) error {
	source := bucket + "/" + key
	in := &s3.CopyObjectInput{
		Bucket:            aws.String(bucket),
		Key:               aws.String(key),
		CopySource:        aws.String(source),
		MetadataDirective: s3types.MetadataDirectiveReplace,
		Metadata:          metadata,
	}
	_, err := d.client.CopyObject(ctx, in)
	return err
}

// ---- Multipart ----------------------------------------------------------

func (d *Driver) CreateMultipart(ctx context.Context, bucket, key, contentType string, metadata map[string]string) (string, error) {
	in := &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if contentType != "" {
		in.ContentType = aws.String(contentType)
	}
	if len(metadata) > 0 {
		in.Metadata = metadata
	}
	out, err := d.client.CreateMultipartUpload(ctx, in)
	if err != nil {
		return "", err
	}
	return aws.ToString(out.UploadId), nil
}

// MaxBufferedPartBytes caps how big a single part the s3v4 driver will hold
// in memory. SigV4 over plain HTTP requires a seekable body so the SDK can
// hash the payload — a streaming io.Reader fails. Buffering one part per
// in-flight upload keeps us well under the spec's 100 MB RAM ceiling for
// reasonable part sizes (default 16 MB → 16 MB per concurrent upload).
const MaxBufferedPartBytes = 64 * 1024 * 1024

func (d *Driver) UploadPart(ctx context.Context, bucket, key, uploadID string, partNum int, r io.Reader, size int64) (string, error) {
	if size <= 0 {
		return "", fmt.Errorf("s3v4: part size required")
	}
	if size > MaxBufferedPartBytes {
		return "", fmt.Errorf("s3v4: part size %d exceeds buffer ceiling %d", size, MaxBufferedPartBytes)
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", fmt.Errorf("s3v4: read part body: %w", err)
	}
	pn := int32(partNum)
	out, err := d.client.UploadPart(ctx, &s3.UploadPartInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		UploadId:      aws.String(uploadID),
		PartNumber:    &pn,
		Body:          bytes.NewReader(buf),
		ContentLength: aws.Int64(size),
	})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.ETag), nil
}

func (d *Driver) CompleteMultipart(ctx context.Context, bucket, key, uploadID string, parts []backend.CompletedPart) (backend.ObjectInfo, error) {
	cp := make([]s3types.CompletedPart, 0, len(parts))
	for _, p := range parts {
		pn := int32(p.PartNumber)
		cp = append(cp, s3types.CompletedPart{PartNumber: &pn, ETag: aws.String(p.ETag)})
	}
	out, err := d.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:          aws.String(bucket),
		Key:             aws.String(key),
		UploadId:        aws.String(uploadID),
		MultipartUpload: &s3types.CompletedMultipartUpload{Parts: cp},
	})
	if err != nil {
		return backend.ObjectInfo{}, err
	}
	return backend.ObjectInfo{
		Key:       key,
		ETag:      aws.ToString(out.ETag),
		VersionID: aws.ToString(out.VersionId),
	}, nil
}

func (d *Driver) AbortMultipart(ctx context.Context, bucket, key, uploadID string) error {
	_, err := d.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	return err
}

func (d *Driver) ListMultipartUploads(ctx context.Context, bucket, prefix string) ([]backend.MultipartUpload, error) {
	in := &s3.ListMultipartUploadsInput{Bucket: aws.String(bucket)}
	if prefix != "" {
		in.Prefix = aws.String(prefix)
	}
	out, err := d.client.ListMultipartUploads(ctx, in)
	if err != nil {
		return nil, err
	}
	res := make([]backend.MultipartUpload, 0, len(out.Uploads))
	for _, u := range out.Uploads {
		mu := backend.MultipartUpload{
			Key:      aws.ToString(u.Key),
			UploadID: aws.ToString(u.UploadId),
		}
		if u.Initiated != nil {
			mu.Initiated = *u.Initiated
		}
		res = append(res, mu)
	}
	return res, nil
}

// ---- Presigned URLs -----------------------------------------------------

func (d *Driver) PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (string, error) {
	req, err := d.presigner.PresignGetObject(ctx,
		&s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)},
		s3.WithPresignExpires(ttl),
	)
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

func (d *Driver) PresignPut(ctx context.Context, bucket, key string, ttl time.Duration, contentType string) (string, error) {
	in := &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)}
	if contentType != "" {
		in.ContentType = aws.String(contentType)
	}
	req, err := d.presigner.PresignPutObject(ctx, in, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

// Admin returns nil — the generic s3v4 driver does not expose backend-native
// admin APIs. Wrap this driver in internal/backend/minio or garage for that.
func (d *Driver) Admin() backend.AdminBackend { return nil }

// ---- helpers ------------------------------------------------------------

type objectReader struct {
	body io.ReadCloser
	info backend.ObjectInfo
}

func (r *objectReader) Read(p []byte) (int, error) { return r.body.Read(p) }
func (r *objectReader) Close() error               { return r.body.Close() }
func (r *objectReader) Info() backend.ObjectInfo   { return r.info }

func formatRange(r backend.Range) string {
	if r.End < 0 {
		return fmt.Sprintf("bytes=%d-", r.Start)
	}
	return fmt.Sprintf("bytes=%d-%d", r.Start, r.End)
}

// IsNotFound reports whether err is an S3 "does not exist" error. Handlers
// use this to distinguish 404 from other errors.
func IsNotFound(err error) bool {
	var nsk *s3types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var nsb *s3types.NoSuchBucket
	if errors.As(err, &nsb) {
		return true
	}
	var nf *s3types.NotFound
	if errors.As(err, &nf) {
		return true
	}
	// S3 surfaces code-specific 404s (NoSuchCORSConfiguration,
	// NoSuchLifecycleConfiguration, NoSuchBucketPolicy, ...) that aren't typed
	// in s3types. Inspect the underlying HTTP response so any 404 counts.
	var re *smithyhttp.ResponseError
	if errors.As(err, &re) && re.HTTPStatusCode() == http.StatusNotFound {
		return true
	}
	return false
}
