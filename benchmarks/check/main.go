// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

// Regression gate for the stowage benchmark.
//
// Reads a current results.json (produced by ../bench.go -json) and
// compares each endpoint against a committed baseline.json. Exits
// non-zero — and renders a markdown report — if any endpoint shows a
// "serious" regression, defined as either of:
//
//   - throughput dropped by more than -tput-tolerance (default 30%)
//   - p99 latency rose by more than -p99-tolerance (default 100%)
//
// Endpoints with very low absolute throughput in the baseline (e.g.
// rate-limited login) are exempted from the throughput rule because
// noise dominates; the p99 rule still applies.
//
// Usage (CI):
//
//	go run ./benchmarks/check \
//	    -current benchmarks/results.json \
//	    -baseline benchmarks/baseline.json \
//	    -summary "$GITHUB_STEP_SUMMARY"
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

type result struct {
	Name          string  `json:"name"`
	Concurrency   int     `json:"concurrency"`
	Ops           int64   `json:"ops"`
	Errs          int64   `json:"errs"`
	DurationMS    float64 `json:"duration_ms"`
	ThroughputRPS float64 `json:"throughput_rps"`
	MeanMS        float64 `json:"mean_ms"`
	P50MS         float64 `json:"p50_ms"`
	P95MS         float64 `json:"p95_ms"`
	P99MS         float64 `json:"p99_ms"`
	MinMS         float64 `json:"min_ms"`
	MaxMS         float64 `json:"max_ms"`
}

type row struct {
	name           string
	baseTput       float64
	curTput        float64
	tputDeltaPct   float64
	baseP99        float64
	curP99         float64
	p99DeltaPct    float64
	failedTput     bool
	failedP99      bool
	exemptTput     bool
	missingFromCur bool
}

func main() {
	currentPath := flag.String("current", "benchmarks/results.json", "path to current bench JSON")
	baselinePath := flag.String("baseline", "benchmarks/baseline.json", "path to committed baseline JSON")
	tputTol := flag.Float64("tput-tolerance", 0.30, "max acceptable throughput drop (0.30 = -30%)")
	p99Tol := flag.Float64("p99-tolerance", 1.00, "max acceptable p99 latency increase (1.00 = +100%, i.e. 2x)")
	tputFloor := flag.Float64("tput-exempt-below", 50.0, "skip throughput rule when baseline req/s below this (e.g. rate-limited login)")
	summaryPath := flag.String("summary", "", "optional path to write markdown summary (use $GITHUB_STEP_SUMMARY in CI)")
	flag.Parse()

	current := mustLoad(*currentPath)
	baseline := mustLoad(*baselinePath)

	curByName := byName(current)

	rows := make([]row, 0, len(baseline))
	for _, b := range baseline {
		r := row{name: b.Name, baseTput: b.ThroughputRPS, baseP99: b.P99MS}
		c, ok := curByName[b.Name]
		if !ok {
			r.missingFromCur = true
			rows = append(rows, r)
			continue
		}
		r.curTput = c.ThroughputRPS
		r.curP99 = c.P99MS
		if b.ThroughputRPS > 0 {
			r.tputDeltaPct = (c.ThroughputRPS - b.ThroughputRPS) / b.ThroughputRPS
		}
		if b.P99MS > 0 {
			r.p99DeltaPct = (c.P99MS - b.P99MS) / b.P99MS
		}
		if b.ThroughputRPS < *tputFloor {
			r.exemptTput = true
		} else if r.tputDeltaPct < -(*tputTol) {
			r.failedTput = true
		}
		if r.p99DeltaPct > *p99Tol {
			r.failedP99 = true
		}
		rows = append(rows, r)
	}

	// Stable, deterministic order.
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })

	report := renderReport(rows, *currentPath, *baselinePath, *tputTol, *p99Tol, *tputFloor)
	fmt.Println(report)
	if *summaryPath != "" {
		if err := appendFile(*summaryPath, report); err != nil {
			fmt.Fprintf(os.Stderr, "warn: could not write summary to %s: %v\n", *summaryPath, err)
		}
	}

	failed := false
	for _, r := range rows {
		if r.missingFromCur || r.failedTput || r.failedP99 {
			failed = true
			break
		}
	}
	if failed {
		fmt.Fprintln(os.Stderr, "REGRESSION DETECTED — see report above.")
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "no serious regression vs baseline")
}

func mustLoad(path string) []result {
	blob, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "check: read %s: %v\n", path, err)
		os.Exit(2)
	}
	var out []result
	if err := json.Unmarshal(blob, &out); err != nil {
		fmt.Fprintf(os.Stderr, "check: parse %s: %v\n", path, err)
		os.Exit(2)
	}
	return out
}

func byName(rs []result) map[string]result {
	m := make(map[string]result, len(rs))
	for _, r := range rs {
		m[r.Name] = r
	}
	return m
}

func renderReport(rows []row, currentPath, baselinePath string, tputTol, p99Tol, tputFloor float64) string {
	var b strings.Builder
	fmt.Fprintln(&b, "## Benchmark regression check")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "- current:  `%s`\n", currentPath)
	fmt.Fprintf(&b, "- baseline: `%s`\n", baselinePath)
	fmt.Fprintf(&b, "- thresholds: throughput must stay within −%.0f%%, p99 within +%.0f%% (exempt below %.0f req/s baseline)\n",
		tputTol*100, p99Tol*100, tputFloor)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Endpoint | Baseline tput | Current tput | Δ tput | Baseline p99 (ms) | Current p99 (ms) | Δ p99 | Status |")
	fmt.Fprintln(&b, "|---|---:|---:|---:|---:|---:|---:|:---|")
	for _, r := range rows {
		status := "OK"
		switch {
		case r.missingFromCur:
			status = "MISSING (endpoint not present in current run)"
		case r.failedTput && r.failedP99:
			status = "FAIL (throughput AND p99 regressed)"
		case r.failedTput:
			status = "FAIL (throughput)"
		case r.failedP99:
			status = "FAIL (p99 latency)"
		case r.exemptTput && r.p99DeltaPct <= p99Tol:
			status = "OK (throughput rule exempt)"
		}
		if r.missingFromCur {
			fmt.Fprintf(&b, "| `%s` | %.1f | — | — | %.2f | — | — | %s |\n",
				r.name, r.baseTput, r.baseP99, status)
			continue
		}
		fmt.Fprintf(&b, "| `%s` | %.1f | %.1f | %+.1f%% | %.2f | %.2f | %+.1f%% | %s |\n",
			r.name,
			r.baseTput, r.curTput, r.tputDeltaPct*100,
			r.baseP99, r.curP99, r.p99DeltaPct*100,
			status,
		)
	}
	return b.String()
}

func appendFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(content + "\n")
	return err
}
