// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package quickstart bootstraps a self-contained stowage instance: it
// downloads a matching MinIO release, starts it as a child process with
// random credentials, then runs the stowage dashboard pointed at it. Used
// as the container's default CMD and as the `stowage quickstart` command.
package quickstart

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/config"
	"github.com/stowage-dev/stowage/internal/server"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

// DefaultBaseURL is the release directory we fetch MinIO binaries from.
// stowage-dev/stowage-minio publishes a SHA256SUMS file alongside the
// per-platform binaries.
const DefaultBaseURL = "https://github.com/stowage-dev/stowage-minio/releases/latest/download"

// Options controls a quickstart run. Zero values pick sane defaults.
type Options struct {
	DataDir          string // default ./data (resolved to abs)
	Listen           string // stowage listen addr, default :8080
	MinIOAddr        string // default :9000
	MinIOConsoleAddr string // default :9001
	AdminUsername    string // default admin
	AdminPassword    string // default: random 24-char
	BackendID        string // default local-minio
	BaseURL          string // default DefaultBaseURL
	HealthTimeout    time.Duration
}

func (o *Options) defaults() {
	if o.DataDir == "" {
		o.DataDir = "./data"
	}
	if o.Listen == "" {
		o.Listen = ":8080"
	}
	if o.MinIOAddr == "" {
		o.MinIOAddr = ":9000"
	}
	if o.MinIOConsoleAddr == "" {
		o.MinIOConsoleAddr = ":9001"
	}
	if o.AdminUsername == "" {
		o.AdminUsername = "admin"
	}
	if o.BackendID == "" {
		o.BackendID = "local-minio"
	}
	if o.BaseURL == "" {
		o.BaseURL = DefaultBaseURL
	}
	if o.HealthTimeout <= 0 {
		o.HealthTimeout = 60 * time.Second
	}
}

// Run executes the full quickstart flow. It blocks until ctx is cancelled
// or stowage exits, then tears down the MinIO subprocess.
func Run(ctx context.Context, opts Options, logger *slog.Logger) error {
	opts.defaults()
	if logger == nil {
		logger = slog.Default()
	}

	asset, err := minioAsset(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}

	dataDir, err := filepath.Abs(opts.DataDir)
	if err != nil {
		return fmt.Errorf("resolve data dir: %w", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	binDir := filepath.Join(dataDir, "bin")
	binPath := filepath.Join(binDir, asset.binaryName())
	minioDataDir := filepath.Join(dataDir, "minio")
	sqlitePath := filepath.Join(dataDir, "stowage.db")

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(minioDataDir, 0o755); err != nil {
		return err
	}

	if !fileExists(binPath) {
		logger.Info("quickstart: downloading minio", "asset", asset.assetName, "url", opts.BaseURL+"/"+asset.assetName)
		if err := downloadMinIO(ctx, opts.BaseURL, asset, binPath); err != nil {
			return fmt.Errorf("download minio: %w", err)
		}
		logger.Info("quickstart: minio downloaded", "path", binPath)
	} else {
		logger.Info("quickstart: minio already present", "path", binPath)
	}

	accessKey, err := randomKey(20)
	if err != nil {
		return err
	}
	secretKey, err := randomKey(40)
	if err != nil {
		return err
	}
	adminPassword := opts.AdminPassword
	if adminPassword == "" {
		adminPassword, err = randomPassword(24)
		if err != nil {
			return err
		}
	}

	// Make creds visible to the backend registry.
	const accessEnv = "STOWAGE_QUICKSTART_MINIO_ACCESS_KEY"
	const secretEnv = "STOWAGE_QUICKSTART_MINIO_SECRET_KEY"
	if err := os.Setenv(accessEnv, accessKey); err != nil {
		return err
	}
	if err := os.Setenv(secretEnv, secretKey); err != nil {
		return err
	}

	minioCtx, cancelMinIO := context.WithCancel(context.Background())
	defer cancelMinIO()

	minioDone, err := startMinIO(minioCtx, binPath, minioDataDir, opts.MinIOAddr, opts.MinIOConsoleAddr, accessKey, secretKey, logger)
	if err != nil {
		return fmt.Errorf("start minio: %w", err)
	}
	// Make sure MinIO is stopped no matter how we exit.
	defer func() {
		cancelMinIO()
		select {
		case <-minioDone:
		case <-time.After(10 * time.Second):
			logger.Warn("quickstart: minio did not exit in 10s")
		}
	}()

	if err := waitForMinIO(ctx, opts.MinIOAddr, opts.HealthTimeout, logger); err != nil {
		return err
	}

	cfg := buildConfig(opts, sqlitePath, accessEnv, secretEnv)

	if err := bootstrapAdmin(ctx, cfg, opts.AdminUsername, adminPassword, logger); err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}

	printBanner(opts, adminPassword, accessKey, secretKey)

	srv, err := server.New(ctx, cfg, logger)
	if err != nil {
		return fmt.Errorf("init stowage: %w", err)
	}
	if err := srv.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// --- platform / asset detection -----------------------------------------

type asset struct {
	assetName string // e.g. minio-linux-amd64
	exe       bool
}

func (a asset) binaryName() string {
	if a.exe {
		return "minio.exe"
	}
	return "minio"
}

func minioAsset(goos, goarch string) (asset, error) {
	switch goos {
	case "linux":
		switch goarch {
		case "amd64", "arm64", "ppc64le", "s390x":
			return asset{assetName: "minio-linux-" + goarch}, nil
		}
	case "windows":
		if goarch == "amd64" {
			return asset{assetName: "minio-windows-amd64.exe", exe: true}, nil
		}
	case "darwin":
		// stowage-dev/stowage-minio does not yet publish darwin builds.
		return asset{}, fmt.Errorf("quickstart: macOS is not yet supported (no minio-darwin-* build is published at %s); install MinIO yourself and point stowage at it via a config file", DefaultBaseURL)
	}
	return asset{}, fmt.Errorf("quickstart: unsupported platform %s/%s", goos, goarch)
}

// --- download + verify --------------------------------------------------

func downloadMinIO(ctx context.Context, baseURL string, a asset, dst string) error {
	binURL := baseURL + "/" + a.assetName
	sumsURL := baseURL + "/SHA256SUMS"

	expected, err := fetchExpectedHash(ctx, sumsURL, a.assetName)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".minio-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, binURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", binURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", binURL, resp.Status)
	}

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hasher), resp.Body); err != nil {
		return fmt.Errorf("download %s: %w", binURL, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if got != expected {
		return fmt.Errorf("checksum mismatch for %s (expected %s, got %s)", a.assetName, expected, got)
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return err
	}
	return nil
}

func fetchExpectedHash(ctx context.Context, sumsURL, asset string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sumsURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", sumsURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", sumsURL, resp.Status)
	}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		// Tolerate the leading '*' that sha256sum emits in binary mode.
		name := strings.TrimPrefix(fields[1], "*")
		if name == asset {
			return strings.ToLower(fields[0]), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s not present in SHA256SUMS at %s", asset, sumsURL)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// --- minio subprocess ----------------------------------------------------

func startMinIO(ctx context.Context, binPath, dataDir, addr, consoleAddr, accessKey, secretKey string, logger *slog.Logger) (<-chan struct{}, error) {
	cmd := exec.CommandContext(ctx, binPath,
		"server", dataDir,
		"--address", addr,
		"--console-address", consoleAddr,
	)
	cmd.Env = append(os.Environ(),
		"MINIO_ROOT_USER="+accessKey,
		"MINIO_ROOT_PASSWORD="+secretKey,
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	go pipeLines(stdout, logger, "minio")
	go pipeLines(stderr, logger, "minio")

	done := make(chan struct{})
	go func() {
		if err := cmd.Wait(); err != nil {
			// ctx cancellation is the expected exit path.
			if ctx.Err() == nil {
				logger.Error("quickstart: minio exited", "err", err.Error())
			}
		}
		close(done)
	}()
	return done, nil
}

func pipeLines(r io.Reader, logger *slog.Logger, src string) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), 1024*1024)
	for sc.Scan() {
		logger.Info(sc.Text(), "src", src)
	}
}

func waitForMinIO(ctx context.Context, addr string, timeout time.Duration, logger *slog.Logger) error {
	host := "127.0.0.1"
	if i := strings.LastIndex(addr, ":"); i > 0 {
		// honour explicit hosts like 0.0.0.0:9000
		h := addr[:i]
		if h != "" && h != "0.0.0.0" {
			host = h
		}
	}
	port := strings.TrimPrefix(addr, addr[:strings.LastIndex(addr, ":")])
	url := "http://" + host + port + "/minio/health/live"

	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				logger.Info("quickstart: minio ready", "url", url)
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("quickstart: minio did not become ready at %s within %s", url, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// --- stowage config + admin ---------------------------------------------

func buildConfig(opts Options, sqlitePath, accessEnv, secretEnv string) config.Config {
	cfg := config.Defaults()
	cfg.Server.Listen = opts.Listen
	cfg.DB.SQLite.Path = sqlitePath
	cfg.Auth.Modes = []string{"local"}
	endpoint := "http://127.0.0.1" + opts.MinIOAddr
	cfg.Backends = []config.BackendConfig{{
		ID:           opts.BackendID,
		Name:         "Local MinIO",
		Type:         "s3v4",
		Endpoint:     endpoint,
		Region:       "us-east-1",
		AccessKeyEnv: accessEnv,
		SecretKeyEnv: secretEnv,
		PathStyle:    true,
	}}
	return cfg
}

func bootstrapAdmin(ctx context.Context, cfg config.Config, username, password string, logger *slog.Logger) error {
	store, err := sqlite.Open(ctx, cfg.DB.SQLite.Path)
	if err != nil {
		return err
	}
	defer store.Close()

	existing, err := store.GetUserByUsername(ctx, username)
	switch {
	case err == nil && existing != nil:
		logger.Info("quickstart: admin already exists, leaving password unchanged", "username", username)
		return nil
	case errors.Is(err, sqlite.ErrUserNotFound):
		// fall through to create
	case err != nil:
		return err
	}

	svc := auth.NewService(cfg.Auth, store, nil)
	if _, err := svc.CreateLocalUser(ctx, username, "", password, "admin", "", false); err != nil {
		return err
	}
	logger.Info("quickstart: admin created", "username", username)
	return nil
}

// --- credentials ---------------------------------------------------------

const passwordAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randomKey(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b)[:n], nil
}

func randomPassword(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i, v := range b {
		out[i] = passwordAlphabet[int(v)%len(passwordAlphabet)]
	}
	return string(out), nil
}

func printBanner(opts Options, adminPassword, accessKey, secretKey string) {
	bar := strings.Repeat("=", 72)
	fmt.Println()
	fmt.Println(bar)
	fmt.Println("  stowage quickstart is ready")
	fmt.Println(bar)
	fmt.Printf("  Dashboard:        http://127.0.0.1%s\n", opts.Listen)
	fmt.Printf("  Admin username:   %s\n", opts.AdminUsername)
	fmt.Printf("  Admin password:   %s\n", adminPassword)
	fmt.Println()
	fmt.Printf("  MinIO API:        http://127.0.0.1%s\n", opts.MinIOAddr)
	fmt.Printf("  MinIO console:    http://127.0.0.1%s\n", opts.MinIOConsoleAddr)
	fmt.Printf("  MinIO access key: %s\n", accessKey)
	fmt.Printf("  MinIO secret key: %s\n", secretKey)
	fmt.Println(bar)
	fmt.Println()
}
