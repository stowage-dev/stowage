// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Log       LogConfig       `yaml:"log"`
	DB        DBConfig        `yaml:"db"`
	Auth      AuthConfig      `yaml:"auth"`
	Backends  []BackendConfig `yaml:"backends"`
	Quotas    QuotasConfig    `yaml:"quotas"`
	RateLimit RateLimitConfig `yaml:"ratelimit"`
	S3Proxy   S3ProxyConfig   `yaml:"s3_proxy"`
	Audit     AuditConfig     `yaml:"audit"`
}

// AuditConfig controls what events the audit recorder persists. The
// recorder itself is best-effort by contract (drops on queue overflow,
// Noop is a valid implementation), so sampling here is a continuation
// of that "best-effort" stance rather than a new tradeoff.
type AuditConfig struct {
	Sampling AuditSamplingConfig `yaml:"sampling"`
}

// AuditSamplingConfig governs which events are written.
//
// The proxy hot path generates one audit event per request, including
// a high volume of idempotent read traffic (HeadObject / GetObject /
// ListObjects). At ~1k rps that's 1k audit rows per second, which
// pprof showed consuming ~28 % of the proxy's available CPU even
// after the BatchRecorder fix landed. ProxySuccessReadRate is a
// dial that lets operators trade audit fidelity on those events for
// throughput without affecting any of the security-relevant rows
// (writes, deletes, denied / errored requests, dashboard actions).
type AuditSamplingConfig struct {
	// ProxySuccessReadRate is the fraction of successful read-shaped
	// proxy events (GET / HEAD with a 2xx / 3xx response) recorded
	// to the audit log. 0.0 (the default) skips every successful
	// proxy read; 1.0 records every event. Writes, deletes, and any
	// non-2xx response are always recorded regardless of this knob.
	//
	// Defaults to 0 because the per-request access trail for reads
	// is rarely useful for forensics versus the cost it imposes on
	// a 1-CPU-cgroup deployment. Bump to 1.0 in compliance-sensitive
	// deployments where every successful read must be attributable.
	ProxySuccessReadRate float64 `yaml:"proxy_success_read_rate"`
}

// S3ProxyConfig governs the embedded SigV4 proxy. When Enabled is false the
// listener is never bound and stowage continues to operate as a
// dashboard-only binary (no extra ports, no extra goroutines).
type S3ProxyConfig struct {
	Enabled bool `yaml:"enabled"`
	// Listen is the bind address for the proxy. Defaults to ":8090" when
	// no explicit value is given. Keep this on a separate port from
	// server.listen so the dashboard and SDK traffic can be terminated
	// by independent reverse-proxy entries.
	Listen string `yaml:"listen"`
	// HostSuffixes is the set of virtual-hosted style host suffixes the
	// router strips when classifying requests, e.g. "s3.example.com" so a
	// request to "uploads.s3.example.com" is treated as bucket=uploads.
	// Path-style requests do not need this set.
	HostSuffixes []string `yaml:"host_suffixes"`
	// GlobalRPS caps total requests-per-second across every credential.
	// 0 = unlimited. PerKeyRPS caps requests-per-second per access-key.
	// Both are applied additively (a request must pass global *and* per-key).
	GlobalRPS float64 `yaml:"global_rps"`
	PerKeyRPS float64 `yaml:"per_key_rps"`
	// AnonymousEnabled is the cluster-wide kill switch for unauthenticated
	// reads. When false, the anonymous fast-path is never entered, even
	// for buckets with an active s3_anonymous_bindings row.
	AnonymousEnabled bool `yaml:"anonymous_enabled"`
	// AnonymousRPS is the per-source-IP RPS default for anonymous requests
	// when the binding does not specify a per-binding override.
	AnonymousRPS float64           `yaml:"anonymous_rps"`
	Kubernetes   S3ProxyKubernetes `yaml:"kubernetes"`
}

// S3ProxyKubernetes opts the proxy into reading virtual credentials and
// anonymous bindings from a Kubernetes Secret informer (the same Secrets
// that the stowage operator writes). Disabled by default — explicit
// opt-in protects laptops with stale kubeconfigs from accidentally
// attaching to a remote cluster.
type S3ProxyKubernetes struct {
	Enabled bool `yaml:"enabled"`
	// Namespace holds the operator-written Secrets. Defaults to
	// "stowage-system" when empty.
	Namespace string `yaml:"namespace"`
	// Kubeconfig is an optional path to a kubeconfig file. Empty means
	// in-cluster configuration (the default for Pods running with a
	// service-account token).
	Kubeconfig string `yaml:"kubeconfig"`
}

type QuotasConfig struct {
	// ScanInterval is how often the scanner re-counts every quota-configured
	// bucket. Defaults to 30m. Set to a negative duration to disable the
	// scheduled scan (admins can still trigger ad-hoc recomputes).
	ScanInterval time.Duration `yaml:"scan_interval"`
}

type RateLimitConfig struct {
	// APIPerMinute caps requests-per-minute per session against /api/*.
	// 0 disables the limiter entirely. Default 600 = 10 req/s sustained
	// with bursts up to 600 in a minute. Multipart uploaders that need
	// more parallelism should raise this.
	APIPerMinute int `yaml:"api_per_minute"`
}

type DBConfig struct {
	Driver string       `yaml:"driver"` // "sqlite" (default) | "postgres"
	SQLite SQLiteConfig `yaml:"sqlite"`
}

type SQLiteConfig struct {
	Path string `yaml:"path"`
}

type ServerConfig struct {
	Listen          string        `yaml:"listen"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	PublicURL       string        `yaml:"public_url"`
	// TrustedProxies is a list of CIDRs (or bare IPs) whose X-Forwarded-For
	// / X-Real-IP / X-Forwarded-Proto headers stowage will honour. The
	// empty default trusts every immediate peer, which suits the typical
	// "behind a proxy" deployment. Setting a non-empty list locks the
	// gate down to those CIDRs only.
	TrustedProxies []string `yaml:"trusted_proxies"`
	// SecretKeyFile is an optional path to a file containing the AES-256
	// root key used to seal endpoint secrets at rest. If unset, only the
	// STOWAGE_SECRET_KEY env var can supply the key. If set and the file
	// is missing, stowage generates a fresh key and writes it (hex,
	// mode 0600) on first boot — convenient for self-hosters at the cost
	// of co-locating key and ciphertext on the same disk.
	SecretKeyFile string `yaml:"secret_key_file"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

type AuthConfig struct {
	Modes   []string      `yaml:"modes"`
	Session SessionConfig `yaml:"session"`
	Local   LocalConfig   `yaml:"local"`
	OIDC    OIDCConfig    `yaml:"oidc"`
	Static  StaticConfig  `yaml:"static"`
}

type SessionConfig struct {
	Lifetime    time.Duration `yaml:"lifetime"`
	IdleTimeout time.Duration `yaml:"idle_timeout"`
}

type LocalConfig struct {
	AllowSelfRegistration bool           `yaml:"allow_self_registration"`
	RequireAdminApproval  bool           `yaml:"require_admin_approval"`
	Password              PasswordPolicy `yaml:"password"`
	Lockout               LockoutPolicy  `yaml:"lockout"`
	ResetEmail            ResetEmailCfg  `yaml:"reset_email"`
}

type PasswordPolicy struct {
	MinLength    int  `yaml:"min_length"`
	PreventReuse bool `yaml:"prevent_reuse"`
}

type LockoutPolicy struct {
	MaxAttempts int           `yaml:"max_attempts"`
	Window      time.Duration `yaml:"window"`
}

type ResetEmailCfg struct {
	Enabled bool `yaml:"enabled"`
}

type OIDCConfig struct {
	Issuer          string              `yaml:"issuer"`
	ClientID        string              `yaml:"client_id"`
	ClientSecretEnv string              `yaml:"client_secret_env"`
	Scopes          []string            `yaml:"scopes"`
	RoleClaim       string              `yaml:"role_claim"`
	RoleMapping     map[string][]string `yaml:"role_mapping"`
}

type StaticConfig struct {
	Enabled         bool   `yaml:"enabled"`
	Username        string `yaml:"username"`
	PasswordHashEnv string `yaml:"password_hash_env"`
}

type BackendConfig struct {
	ID           string `yaml:"id"`
	Name         string `yaml:"name"`
	Type         string `yaml:"type"`
	Endpoint     string `yaml:"endpoint"`
	Region       string `yaml:"region"`
	AccessKeyEnv string `yaml:"access_key_env"`
	SecretKeyEnv string `yaml:"secret_key_env"`
	PathStyle    bool   `yaml:"path_style"`
}

func Load(path string) (Config, error) {
	cfg := Defaults()
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read %s: %w", path, err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	applyEnv(&cfg)
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Defaults() Config {
	return Config{
		Server: ServerConfig{
			Listen:          ":8080",
			ShutdownTimeout: 10 * time.Second,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
		DB: DBConfig{
			Driver: "sqlite",
			SQLite: SQLiteConfig{Path: "./stowage.db"},
		},
		Quotas: QuotasConfig{
			ScanInterval: 30 * time.Minute,
		},
		RateLimit: RateLimitConfig{
			APIPerMinute: 600,
		},
		Auth: AuthConfig{
			Modes: []string{"local"},
			Session: SessionConfig{
				Lifetime:    8 * time.Hour,
				IdleTimeout: 1 * time.Hour,
			},
			Local: LocalConfig{
				Password: PasswordPolicy{
					MinLength:    12,
					PreventReuse: true,
				},
				Lockout: LockoutPolicy{
					MaxAttempts: 5,
					Window:      15 * time.Minute,
				},
			},
		},
		S3Proxy: S3ProxyConfig{
			Listen:       ":8090",
			AnonymousRPS: 20,
			Kubernetes: S3ProxyKubernetes{
				Namespace: "stowage-system",
			},
		},
		Audit: AuditConfig{
			Sampling: AuditSamplingConfig{
				ProxySuccessReadRate: 0.0, // skip successful proxy reads by default
			},
		},
	}
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("STOWAGE_LISTEN"); v != "" {
		cfg.Server.Listen = v
	}
	if v := os.Getenv("STOWAGE_PUBLIC_URL"); v != "" {
		cfg.Server.PublicURL = v
	}
	if v := os.Getenv("STOWAGE_LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}
	if v := os.Getenv("STOWAGE_LOG_FORMAT"); v != "" {
		cfg.Log.Format = v
	}
	if v := os.Getenv("STOWAGE_SQLITE_PATH"); v != "" {
		cfg.DB.SQLite.Path = v
	}
	if v := os.Getenv("STOWAGE_SECRET_KEY_FILE"); v != "" {
		cfg.Server.SecretKeyFile = v
	}
}

func (c Config) validate() error {
	if strings.TrimSpace(c.Server.Listen) == "" {
		return fmt.Errorf("server.listen is required")
	}
	switch c.Log.Format {
	case "", "json", "text":
	default:
		return fmt.Errorf("log.format must be \"json\" or \"text\"")
	}
	switch strings.ToLower(c.Log.Level) {
	case "", "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log.level must be debug|info|warn|error")
	}
	switch c.DB.Driver {
	case "", "sqlite":
		if c.DB.SQLite.Path == "" {
			return fmt.Errorf("db.sqlite.path is required when driver is sqlite")
		}
	case "postgres":
		return fmt.Errorf("db.driver %q not yet implemented", c.DB.Driver)
	default:
		return fmt.Errorf("db.driver %q unknown", c.DB.Driver)
	}
	if len(c.Auth.Modes) == 0 {
		return fmt.Errorf("auth.modes must list at least one of: local, oidc, static")
	}
	for _, m := range c.Auth.Modes {
		switch m {
		case "local", "oidc", "static":
		default:
			return fmt.Errorf("auth.modes contains unknown mode %q", m)
		}
	}
	seen := map[string]bool{}
	for i, b := range c.Backends {
		if b.ID == "" {
			return fmt.Errorf("backends[%d].id is required", i)
		}
		if seen[b.ID] {
			return fmt.Errorf("duplicate backend id %q", b.ID)
		}
		seen[b.ID] = true
		if b.Type == "" {
			return fmt.Errorf("backends[%d].type is required", i)
		}
	}
	if c.S3Proxy.Enabled {
		if strings.TrimSpace(c.S3Proxy.Listen) == "" {
			return fmt.Errorf("s3_proxy.listen is required when s3_proxy.enabled is true")
		}
		if c.S3Proxy.Listen == c.Server.Listen {
			return fmt.Errorf("s3_proxy.listen must differ from server.listen (both set to %q)", c.S3Proxy.Listen)
		}
	}
	return nil
}
