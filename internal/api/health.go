// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
)

func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, statusEnvelope{Status: "ok"})
}

func readyz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, statusEnvelope{Status: "ready"})
}

// statusEnvelope is the trivial {status: "..."} body used by /healthz,
// /readyz, and a handful of mutating endpoints. Encoding a struct with a
// stable shape lets the encoder skip the reflective map walk it'd otherwise
// do for map[string]string on every request.
type statusEnvelope struct {
	Status string `json:"status"`
}

// jsonBufPool keeps reusable byte buffers around for encoding response
// bodies. Marshalling into a buffer (rather than streaming via json.Encoder
// directly into the http.ResponseWriter) means we know Content-Length up
// front and write a single Body slice — no chunked transfer for trivial
// JSON, and no per-call allocations for the encoder's intermediate state.
var jsonBufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// writeJSON serialises v as JSON and writes a 200-class response. The
// encoder is configured exactly like json.Marshal (no HTML escape, no
// trailing newline) so existing test fixtures keep matching.
func writeJSON(w http.ResponseWriter, status int, v any) {
	buf := jsonBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer jsonBufPool.Put(buf)

	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		// Encoding failure mid-response is unrecoverable — bail out with
		// a generic 500 so we don't leak partial JSON onto the wire.
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	// json.Encoder.Encode appends a newline; trim it so the body matches
	// json.Marshal exactly.
	body := buf.Bytes()
	if n := len(body); n > 0 && body[n-1] == '\n' {
		body = body[:n-1]
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
