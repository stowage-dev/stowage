// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package chart

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// chartDir resolves deploy/chart relative to this test file so the suite
// runs from any working directory.
func chartDir(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed")
	}
	dir := filepath.Dir(here)
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, "deploy", "chart")
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not find deploy/chart from %s", here)
	return ""
}

// requireHelm skips the test if `helm` is not on PATH. CI installs it via
// the helm/setup-helm action; local devs need it for `make chart-test`
// but a missing binary should produce a clear skip rather than a panic.
func requireHelm(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("helm")
	if err != nil {
		t.Skipf("helm not on PATH: %v", err)
	}
	return bin
}

// runHelm invokes helm with the given args and returns combined output.
// On non-zero exit, the test fails with the full output for triage.
func runHelm(t *testing.T, args ...string) string {
	t.Helper()
	helm := requireHelm(t)
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(helm, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("helm %s: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

// TestChartLint asserts the chart passes `helm lint` without errors. INFO
// messages (like "icon is recommended") are acceptable; ERROR lines are
// not.
func TestChartLint(t *testing.T) {
	dir := chartDir(t)
	out := runHelm(t, "lint", dir)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "[ERROR]") {
			t.Fatalf("helm lint reported errors:\n%s", out)
		}
	}
}

// TestChartTemplate_Default asserts the default `helm template` output
// includes every resource a fresh install should produce. This catches
// accidental {{ if … }} regressions that silently drop a manifest.
func TestChartTemplate_Default(t *testing.T) {
	dir := chartDir(t)
	out := runHelm(t, "template", "stowage", dir,
		"--namespace", "stowage-system",
	)

	mustHave := []string{
		"kind: ServiceAccount",
		"kind: ClusterRole",
		"kind: ClusterRoleBinding",
		"kind: Service",
		"kind: Deployment",
		"kind: ConfigMap",
		"kind: PersistentVolumeClaim",
		"kind: ValidatingWebhookConfiguration",
		"kind: Secret",
	}
	for _, want := range mustHave {
		if !strings.Contains(out, want) {
			t.Errorf("default render missing %q", want)
		}
	}
	if t.Failed() {
		t.Logf("rendered output (truncated):\n%s", truncate(out, 4000))
	}

	// Every YAML doc must parse — guards against malformed templates that
	// happen to look fine in a string contains check.
	for i, doc := range splitYAML(out) {
		var any map[string]any
		if err := yaml.Unmarshal([]byte(doc), &any); err != nil {
			t.Errorf("doc %d unparseable: %v\ndoc:\n%s", i, err, truncate(doc, 800))
		}
	}
}

// TestChartTemplate_OperatorDisabled asserts disabling the operator
// removes the webhook + cluster RBAC + operator-specific config — the
// "dashboard-only" deployment shape stowage explicitly supports.
func TestChartTemplate_OperatorDisabled(t *testing.T) {
	dir := chartDir(t)
	out := runHelm(t, "template", "stowage", dir,
		"--namespace", "stowage-system",
		"--set", "operator.enabled=false",
	)

	mustNotHave := []string{
		"kind: ValidatingWebhookConfiguration",
		"kind: ClusterRole\n",
		"kind: ClusterRoleBinding\n",
	}
	for _, want := range mustNotHave {
		if strings.Contains(out, want) {
			t.Errorf("operator-disabled render unexpectedly contains %q", want)
		}
	}
}

// TestChartTemplate_WebhookDisabled asserts disabling just the webhook
// drops the ValidatingWebhookConfiguration and the cert Secret without
// touching the operator's RBAC or Deployment.
func TestChartTemplate_WebhookDisabled(t *testing.T) {
	dir := chartDir(t)
	out := runHelm(t, "template", "stowage", dir,
		"--namespace", "stowage-system",
		"--set", "webhook.enabled=false",
	)
	if strings.Contains(out, "kind: ValidatingWebhookConfiguration") {
		t.Errorf("webhook-disabled render contains ValidatingWebhookConfiguration")
	}
	if strings.Contains(out, "stowage-webhook-cert") {
		t.Errorf("webhook-disabled render references stowage-webhook-cert")
	}
	// Operator deployment + RBAC must still render.
	if !strings.Contains(out, "kind: Deployment") {
		t.Errorf("webhook-disabled render missing Deployment")
	}
	if !strings.Contains(out, "kind: ClusterRole\n") {
		t.Errorf("webhook-disabled render missing ClusterRole")
	}
}

// TestChartTemplate_CertManagerEnabled asserts switching webhook.certManager
// on swaps the self-signed CA path for the cert-manager injection
// annotation.
func TestChartTemplate_CertManagerEnabled(t *testing.T) {
	dir := chartDir(t)
	out := runHelm(t, "template", "stowage", dir,
		"--namespace", "stowage-system",
		"--set", "webhook.certManager.enabled=true",
		"--set", "webhook.selfSigned.enabled=false",
	)
	if !strings.Contains(out, "cert-manager.io/inject-ca-from") {
		t.Errorf("cert-manager render missing inject-ca-from annotation")
	}
	if !strings.Contains(out, "kind: Certificate") {
		t.Errorf("cert-manager render missing Certificate")
	}
}

// splitYAML splits a multi-doc YAML stream into individual documents,
// skipping empty ones (helm template typically separates with "---\n").
func splitYAML(s string) []string {
	parts := strings.Split(s, "\n---\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

// truncate caps s at n runes so test log output stays readable.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n…(truncated)…"
}

