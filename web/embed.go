// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package web ships the SvelteKit static build embedded in the Go binary.
// The real frontend lives under web/src/ and is built to web/dist/ by the
// SvelteKit static adapter before `go build` runs.
package web

import "embed"

//go:embed all:dist
var Assets embed.FS
