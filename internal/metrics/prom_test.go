// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPromMetricsScrape(t *testing.T) {
	mtx := New()
	mtx.Prom = NewProm("")

	mux := http.NewServeMux()
	mux.Handle("/api/test", mtx.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
	})))
	mux.Handle("/metrics", mtx.Prom.Handler())

	srv := httptest.NewServer(mux)
	defer srv.Close()

	for i := 0; i < 3; i++ {
		resp, err := http.Get(srv.URL + "/api/test")
		if err != nil {
			t.Fatalf("req: %v", err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	if resp.StatusCode != 200 {
		t.Fatalf("scrape status=%d want 200", resp.StatusCode)
	}
	// The counter line must appear and report a 2xx class for our GETs.
	if !strings.Contains(text, `stowage_requests_total{backend="",method="GET",status_class="2xx"} 3`) {
		t.Fatalf("counter not found / wrong value:\n%s", trim(text, 4096))
	}
	// Histogram should expose at least the count metric.
	if !strings.Contains(text, "stowage_request_duration_seconds_count") {
		t.Fatalf("duration histogram missing")
	}
	// Bytes histogram fires when the response wrote >0 bytes.
	if !strings.Contains(text, "stowage_response_bytes_count") {
		t.Fatalf("bytes histogram missing")
	}
}

func TestPromHandlerWhenNil(t *testing.T) {
	var p *Prom
	rr := httptest.NewRecorder()
	p.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/metrics", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want 503", rr.Code)
	}
}

func TestStatusClass(t *testing.T) {
	cases := map[int]string{
		200: "2xx", 201: "2xx", 301: "3xx", 404: "4xx", 500: "5xx", 0: "unknown", 999: "5xx",
	}
	// 999 falls into 9xx in our naive classification — clarify the test.
	for code, want := range cases {
		got := statusClass(code)
		// Only check the cases we actually expect well-defined output for.
		if code < 100 || code >= 600 {
			if got != "unknown" {
				t.Fatalf("statusClass(%d)=%q want unknown", code, got)
			}
			continue
		}
		if got != want {
			t.Fatalf("statusClass(%d)=%q want %q", code, got, want)
		}
	}
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
