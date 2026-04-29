// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3v4

import (
	"fmt"
	"net/url"

	"github.com/stowage-dev/stowage/internal/backend"
)

var _ backend.ProxyTargetProvider = (*Driver)(nil)

// ProxyTarget surfaces the driver's endpoint + admin credentials so the S3
// proxy can re-sign forwarded requests. The endpoint is parsed lazily; an
// invalid endpoint returns an error rather than panicking the proxy hot
// path.
func (d *Driver) ProxyTarget() (backend.ProxyTarget, error) {
	if d.cfg.Endpoint == "" {
		return backend.ProxyTarget{}, fmt.Errorf("s3v4 %q: empty endpoint", d.cfg.ID)
	}
	u, err := url.Parse(d.cfg.Endpoint)
	if err != nil {
		return backend.ProxyTarget{}, fmt.Errorf("s3v4 %q: parse endpoint: %w", d.cfg.ID, err)
	}
	region := d.cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	return backend.ProxyTarget{
		Endpoint:  u,
		Region:    region,
		PathStyle: d.cfg.PathStyle,
		AccessKey: d.cfg.AccessKey,
		SecretKey: d.cfg.SecretKey,
	}, nil
}
