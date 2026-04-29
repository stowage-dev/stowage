// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package s3proxy implements the embedded S3 SigV4 data-plane proxy that
// runs on a separate listener from the dashboard. Inbound virtual SigV4
// requests are verified, scope-checked, rewritten, and re-signed with the
// upstream backend's admin credentials before being forwarded.
package s3proxy

import (
	"encoding/xml"
	"net/http"
)

// S3Error mirrors the XML error body S3 clients expect.
type S3Error struct {
	XMLName   xml.Name `xml:"Error"`
	Code      string   `xml:"Code"`
	Message   string   `xml:"Message"`
	Resource  string   `xml:"Resource,omitempty"`
	RequestID string   `xml:"RequestId,omitempty"`
}

// writeS3Error encodes an XML S3 error with the given HTTP status.
func writeS3Error(w http.ResponseWriter, status int, code, msg, resource, reqID string) {
	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("x-amz-request-id", reqID)
	w.WriteHeader(status)
	_ = xml.NewEncoder(w).Encode(&S3Error{
		Code:      code,
		Message:   msg,
		Resource:  resource,
		RequestID: reqID,
	})
}
