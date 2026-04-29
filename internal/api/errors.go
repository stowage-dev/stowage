// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package api

import "net/http"

// Error is the JSON wire shape returned for API failures.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

type errorEnvelope struct {
	Error Error `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, message, detail string) {
	writeJSON(w, status, errorEnvelope{Error: Error{Code: code, Message: message, Detail: detail}})
}
