// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func containsFinalizer(obj metav1.Object, f string) bool {
	for _, x := range obj.GetFinalizers() {
		if x == f {
			return true
		}
	}
	return false
}

func addFinalizer(obj metav1.Object, f string) {
	if containsFinalizer(obj, f) {
		return
	}
	obj.SetFinalizers(append(obj.GetFinalizers(), f))
}

func removeFinalizer(obj metav1.Object, f string) {
	orig := obj.GetFinalizers()
	out := orig[:0]
	for _, x := range orig {
		if x != f {
			out = append(out, x)
		}
	}
	obj.SetFinalizers(out)
}
