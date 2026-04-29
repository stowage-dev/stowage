// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/stowage-dev/stowage/internal/auth"
	"github.com/stowage-dev/stowage/internal/config"
	"github.com/stowage-dev/stowage/internal/quickstart"
	"github.com/stowage-dev/stowage/internal/server"
	"github.com/stowage-dev/stowage/internal/store/sqlite"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "stowage:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return errors.New("no command given")
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "serve":
		return runServe(rest)
	case "quickstart":
		return runQuickstart(rest)
	case "create-admin":
		return runCreateAdmin(rest)
	case "hash-password":
		return runHashPassword(rest)
	case "-h", "--help", "help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: stowage <command> [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  serve           Run the dashboard server")
	fmt.Fprintln(os.Stderr, "  quickstart      Download MinIO into ./data and run stowage against it")
	fmt.Fprintln(os.Stderr, "  create-admin    Create the first local admin user")
	fmt.Fprintln(os.Stderr, "  hash-password   Print an argon2id hash for a password")
	fmt.Fprintln(os.Stderr, "  help            Show this message")
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to YAML config file (env vars override)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := newLogger(cfg.Log)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv, err := server.New(ctx, cfg, logger)
	if err != nil {
		return fmt.Errorf("init server: %w", err)
	}

	logger.Info("stowage starting",
		"listen", cfg.Server.Listen,
		"auth_modes", cfg.Auth.Modes,
		"backends", len(cfg.Backends),
		"db", cfg.DB.SQLite.Path,
	)
	if err := srv.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Info("stowage stopped")
	return nil
}

func runQuickstart(args []string) error {
	fs := flag.NewFlagSet("quickstart", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "./data", "directory for MinIO binary, MinIO data, and stowage state")
	listen := fs.String("listen", ":8080", "stowage dashboard listen address")
	minioAddr := fs.String("minio-addr", ":9000", "MinIO S3 listen address")
	minioConsoleAddr := fs.String("minio-console-addr", ":9001", "MinIO console listen address")
	adminUsername := fs.String("admin-username", "admin", "username for the bootstrap admin")
	adminPassword := fs.String("admin-password", "", "password for the bootstrap admin (random if empty)")
	baseURL := fs.String("minio-base-url", quickstart.DefaultBaseURL, "base URL for MinIO release downloads")
	if err := fs.Parse(args); err != nil {
		return err
	}

	logger := newLogger(config.LogConfig{Level: "info", Format: "text"})
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return quickstart.Run(ctx, quickstart.Options{
		DataDir:          *dataDir,
		Listen:           *listen,
		MinIOAddr:        *minioAddr,
		MinIOConsoleAddr: *minioConsoleAddr,
		AdminUsername:    *adminUsername,
		AdminPassword:    *adminPassword,
		BaseURL:          *baseURL,
	}, logger)
}

func runCreateAdmin(args []string) error {
	fs := flag.NewFlagSet("create-admin", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to YAML config file (for DB location)")
	username := fs.String("username", "", "username for the new admin")
	password := fs.String("password", "", "password for the new admin")
	email := fs.String("email", "", "optional email")
	mustChange := fs.Bool("must-change-password", false, "force password change on first login")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *username == "" || *password == "" {
		return errors.New("--username and --password are required")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	store, err := sqlite.Open(ctx, cfg.DB.SQLite.Path)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	svc := auth.NewService(cfg.Auth, store, nil)
	u, err := svc.CreateLocalUser(ctx, *username, *email, *password, "admin", "", *mustChange)
	if err != nil {
		return fmt.Errorf("create admin: %w", err)
	}
	fmt.Fprintf(os.Stdout, "created admin user %s (id=%s)\n", u.Username, u.ID)
	return nil
}

func runHashPassword(args []string) error {
	fs := flag.NewFlagSet("hash-password", flag.ContinueOnError)
	password := fs.String("password", "", "password to hash")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *password == "" {
		return errors.New("--password is required")
	}
	hash, err := auth.HashPassword(*password)
	if err != nil {
		return err
	}
	fmt.Println(hash)
	return nil
}

func newLogger(c config.LogConfig) *slog.Logger {
	var level slog.Level
	if err := level.UnmarshalText([]byte(c.Level)); err != nil {
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}

	var h slog.Handler
	switch c.Format {
	case "text":
		h = slog.NewTextHandler(os.Stdout, opts)
	default:
		h = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(h)
}
