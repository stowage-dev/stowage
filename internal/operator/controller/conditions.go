// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// setCondition replaces or appends a condition by type. Uses LastTransitionTime
// only when the status flips.
func setCondition(existing []metav1.Condition, c metav1.Condition) []metav1.Condition {
	c.LastTransitionTime = metav1.Now()
	for i := range existing {
		if existing[i].Type == c.Type {
			if existing[i].Status == c.Status {
				c.LastTransitionTime = existing[i].LastTransitionTime
			}
			existing[i] = c
			return existing
		}
	}
	return append(existing, c)
}
