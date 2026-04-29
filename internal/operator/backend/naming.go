// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package backend

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"text/template"
)

var bucketNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{2,62}$`)

// TemplateVars are the values exposed to a BucketNameTemplate.
type TemplateVars struct {
	Namespace string
	Name      string
	Hash      string
}

// RenderBucketName resolves the bucket name from an optional override and a
// template. The output is validated against the S3 bucket-name regex.
func RenderBucketName(override, tmpl, namespace, name string) (string, error) {
	if override != "" {
		if !bucketNameRE.MatchString(override) {
			return "", fmt.Errorf("override %q not a valid bucket name", override)
		}
		return override, nil
	}
	if tmpl == "" {
		tmpl = "{{ .Namespace }}-{{ .Name }}"
	}
	t, err := template.New("bucket").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	h := sha256.Sum256([]byte(namespace + "/" + name))
	vars := TemplateVars{
		Namespace: namespace,
		Name:      name,
		Hash:      hex.EncodeToString(h[:4]), // 8 hex chars
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	out := buf.String()
	if !bucketNameRE.MatchString(out) {
		return "", fmt.Errorf("rendered bucket name %q is not a valid S3 bucket name", out)
	}
	return out, nil
}

// ValidateTemplate ensures a template renders to a valid bucket name against
// representative inputs. Used as an admission-time check.
func ValidateTemplate(tmpl string) error {
	_, err := RenderBucketName("", tmpl, "my-app", "uploads")
	return err
}
