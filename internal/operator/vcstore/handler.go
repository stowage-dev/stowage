// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package vcstore

import (
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
)

func newHandler(r *Reader, log logr.Logger) cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			s, ok := obj.(*corev1.Secret)
			if !ok {
				return
			}
			r.upsert(s)
		},
		UpdateFunc: func(_ interface{}, obj interface{}) {
			s, ok := obj.(*corev1.Secret)
			if !ok {
				return
			}
			r.upsert(s)
		},
		DeleteFunc: func(obj interface{}) {
			s, ok := obj.(*corev1.Secret)
			if !ok {
				if tomb, tok := obj.(cache.DeletedFinalStateUnknown); tok {
					if s2, sok := tomb.Obj.(*corev1.Secret); sok {
						r.delete(s2)
						return
					}
				}
				return
			}
			r.delete(s)
		},
	}
}
