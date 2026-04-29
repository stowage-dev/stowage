// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"strings"
	"testing"
)

func TestS3ProxyDefaults(t *testing.T) {
	d := Defaults()
	if d.S3Proxy.Enabled {
		t.Fatalf("s3 proxy should be disabled by default")
	}
	if d.S3Proxy.AnonymousRPS != 20 {
		t.Fatalf("default anonymous rps = %v, want 20", d.S3Proxy.AnonymousRPS)
	}
	if d.S3Proxy.Kubernetes.Namespace != "stowage-system" {
		t.Fatalf("default k8s namespace = %q, want stowage-system", d.S3Proxy.Kubernetes.Namespace)
	}
	if d.S3Proxy.Kubernetes.Enabled {
		t.Fatalf("k8s source should be disabled by default")
	}
}

func TestS3ProxyValidation_RequiresListen(t *testing.T) {
	c := Defaults()
	c.S3Proxy.Enabled = true
	c.S3Proxy.Listen = ""
	err := c.validate()
	if err == nil || !strings.Contains(err.Error(), "s3_proxy.listen") {
		t.Fatalf("want listen-required error, got %v", err)
	}
}

func TestS3ProxyValidation_DifferentPort(t *testing.T) {
	c := Defaults()
	c.S3Proxy.Enabled = true
	c.S3Proxy.Listen = c.Server.Listen
	err := c.validate()
	if err == nil || !strings.Contains(err.Error(), "must differ") {
		t.Fatalf("want must-differ error, got %v", err)
	}
}

func TestS3ProxyValidation_Valid(t *testing.T) {
	c := Defaults()
	c.S3Proxy.Enabled = true
	c.S3Proxy.Listen = ":8090"
	if err := c.validate(); err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
}
