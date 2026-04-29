// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package backend

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Config describes how to build an outbound S3 client for an S3Backend.
type Config struct {
	Endpoint        string
	Region          string
	AccessKeyID     string // already prefixed with "<storageSpace>:" when QuObjects applies
	SecretAccessKey string
	UsePathStyle    bool

	InsecureSkipVerify bool
	CABundle           []byte
}

// NewClient returns an aws-sdk-go-v2 S3 client signed with admin credentials.
func NewClient(cfg Config) (*s3.Client, error) {
	tlsCfg := &tls.Config{
		InsecureSkipVerify: cfg.InsecureSkipVerify, //nolint:gosec // opt-in flag exposed via CRD
		MinVersion:         tls.VersionTLS12,
	}
	if len(cfg.CABundle) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(cfg.CABundle) {
			return nil, fmt.Errorf("invalid CA bundle")
		}
		tlsCfg.RootCAs = pool
	}

	transport := &http.Transport{
		TLSClientConfig:       tlsCfg,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	httpClient := &http.Client{Transport: transport, Timeout: 0}

	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	ep := cfg.Endpoint
	return s3.New(s3.Options{
		Region:       region,
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		HTTPClient:   httpClient,
		UsePathStyle: cfg.UsePathStyle,
		BaseEndpoint: aws.String(ep),
	}), nil
}
