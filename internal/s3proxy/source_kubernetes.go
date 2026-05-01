// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package s3proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

// Operator label / data field names. These are the wire contract between
// the stowage operator and this proxy. They MUST match
// internal/operator/vcstore/labels.go exactly — updating them in one place
// without the other silently breaks the integration.
const (
	k8sLabelRole           = "broker.stowage.io/role"
	k8sLabelClaimNS        = "broker.stowage.io/claim-namespace"
	k8sLabelClaimName      = "broker.stowage.io/claim-name"
	k8sLabelBackend        = "broker.stowage.io/backend"
	k8sLabelBucket         = "broker.stowage.io/bucket"
	k8sAnnotationExpiresAt = "broker.stowage.io/expires-at"

	k8sRoleVirtualCredential = "virtual-credential"
	k8sRoleAnonymousBinding  = "anonymous-binding"

	k8sDataAccessKeyID     = "access_key_id"
	k8sDataSecretAccessKey = "secret_access_key"
	k8sDataBucketName      = "bucket_name"
	k8sDataBackend         = "backend"
	k8sDataAnonMode        = "anonymous_mode"
	k8sDataAnonRPS         = "anonymous_per_source_ip_rps"
	k8sDataBucketScopes    = "bucket_scopes"
	// k8sDataQuotaSoftBytes / k8sDataQuotaHardBytes are decimal byte counts
	// the operator copies from BucketClaim.spec.quota into the proxy-facing
	// Secret. Read by the source's LimitObserver to populate the quota
	// service's merged limit cache. Must match
	// internal/operator/vcstore/labels.go.
	k8sDataQuotaSoftBytes = "quota_soft_bytes"
	k8sDataQuotaHardBytes = "quota_hard_bytes"
)

// LimitObserver receives quota updates as the K8s informer sees them. The
// wiring layer adapts quotas.KubernetesLimitSource to this interface so
// the s3proxy package doesn't import the quotas package directly.
type LimitObserver interface {
	SetLimit(backendID, bucket string, softBytes, hardBytes int64)
	DeleteLimit(backendID, bucket string)
}

// KubernetesSourceConfig governs the K8s informer source.
type KubernetesSourceConfig struct {
	// Namespace holds the Secrets the operator wrote.
	Namespace string
	// Kubeconfig is the path to a kubeconfig file. When empty the source
	// loads in-cluster configuration; failure is surfaced from Start.
	Kubeconfig string
	// ResyncPeriod is the informer's full-list resync interval. Zero means
	// "no scheduled relist" (rely on watch); 5–10 minutes is a reasonable
	// safety net against missed events on long-running connections.
	ResyncPeriod time.Duration
	// LimitObserver, when non-nil, receives quota updates parsed from the
	// same Secrets that carry credentials. Wiring code passes a
	// quotas.KubernetesLimitSource adapter so K8s-managed quotas land in
	// the merged limit cache without an extra informer.
	LimitObserver LimitObserver
}

// KubernetesSource watches Kubernetes Secrets in a configured namespace and
// surfaces VirtualCredentials (role=virtual-credential) and
// AnonymousBindings (role=anonymous-binding) to the proxy. Maintains an
// in-memory map updated by informer events; the hot path is lock-protected
// reads only.
type KubernetesSource struct {
	cfg    KubernetesSourceConfig
	client kubernetes.Interface
	logger *slog.Logger

	mu       sync.RWMutex
	byAKID   map[string]*VirtualCredential
	byBucket map[string]*AnonymousBinding

	// started is set once Start has been called so callers can poll for
	// readiness via Size() without racing the first cache prime.
	startedOnce sync.Once
}

// NewKubernetesSource builds a Kubernetes-backed source. Returns an error
// if the kubeconfig (file path or in-cluster) is unloadable.
func NewKubernetesSource(cfg KubernetesSourceConfig, logger *slog.Logger) (*KubernetesSource, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Namespace == "" {
		return nil, fmt.Errorf("s3proxy: kubernetes namespace is required")
	}

	restConfig, err := loadKubeConfig(cfg.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("new kubernetes client: %w", err)
	}
	return &KubernetesSource{
		cfg:      cfg,
		client:   client,
		logger:   logger,
		byAKID:   map[string]*VirtualCredential{},
		byBucket: map[string]*AnonymousBinding{},
	}, nil
}

// Start subscribes to Secret events in the configured namespace, filtered
// to the operator role labels, and primes the in-memory cache.
// Returns once the informer's first sync is complete; the goroutine that
// services watch events runs until ctx is cancelled. ctx must outlive the
// source — cancelling it shuts the informer down. The initial sync wait is
// bounded internally so a slow apiserver doesn't hang boot indefinitely.
func (s *KubernetesSource) Start(ctx context.Context) error {
	resync := s.cfg.ResyncPeriod
	if resync <= 0 {
		resync = 0
	}

	roleSelector := fmt.Sprintf("%s in (%s,%s)",
		k8sLabelRole, k8sRoleVirtualCredential, k8sRoleAnonymousBinding)

	factory := informers.NewSharedInformerFactoryWithOptions(s.client, resync,
		informers.WithNamespace(s.cfg.Namespace),
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.LabelSelector = roleSelector
		}),
	)
	informer := factory.Core().V1().Secrets().Informer()

	if _, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj any) { s.upsert(obj) },
		UpdateFunc: func(_, newObj any) { s.upsert(newObj) },
		DeleteFunc: s.handleDelete,
	}); err != nil {
		return fmt.Errorf("add event handler: %w", err)
	}

	factory.Start(ctx.Done())
	syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if !cache.WaitForCacheSync(syncCtx.Done(), informer.HasSynced) {
		return fmt.Errorf("kubernetes informer cache failed to sync within deadline")
	}

	s.startedOnce.Do(func() {
		s.logger.Info("s3proxy: kubernetes source ready",
			"namespace", s.cfg.Namespace,
			"credentials", s.Size(),
			"anonymous_bindings", s.anonSize())
	})
	return nil
}

// Lookup returns a copy of the virtual credential bound to akid, or false.
func (s *KubernetesSource) Lookup(akid string) (*VirtualCredential, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	vc, ok := s.byAKID[akid]
	if !ok {
		return nil, false
	}
	if vc.ExpiresAt != nil && time.Now().After(*vc.ExpiresAt) {
		return nil, false
	}
	out := *vc
	return &out, true
}

// LookupAnon returns a copy of the anonymous binding for bucket, or false.
func (s *KubernetesSource) LookupAnon(bucket string) (*AnonymousBinding, bool) {
	if bucket == "" {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.byBucket[strings.ToLower(bucket)]
	if !ok {
		return nil, false
	}
	out := *b
	return &out, true
}

// Size returns the number of cached virtual credentials.
func (s *KubernetesSource) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byAKID)
}

func (s *KubernetesSource) anonSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.byBucket)
}

// SnapshotCredentials returns a copy of every cached virtual credential.
// SecretAccessKey is redacted on the copies — admin UIs only need shape and
// scope, not the secret. The slice is unordered; callers that want a stable
// view sort it themselves. Used by the read-only admin view that lists
// operator-provisioned credentials.
func (s *KubernetesSource) SnapshotCredentials() []*VirtualCredential {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*VirtualCredential, 0, len(s.byAKID))
	for _, vc := range s.byAKID {
		c := *vc
		c.SecretAccessKey = ""
		out = append(out, &c)
	}
	return out
}

// SnapshotAnonymousBindings returns a copy of every cached anonymous binding.
// Same caller contract as SnapshotCredentials.
func (s *KubernetesSource) SnapshotAnonymousBindings() []*AnonymousBinding {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*AnonymousBinding, 0, len(s.byBucket))
	for _, b := range s.byBucket {
		c := *b
		out = append(out, &c)
	}
	return out
}

func (s *KubernetesSource) upsert(obj any) {
	sec, ok := obj.(*corev1.Secret)
	if !ok {
		return
	}
	switch sec.Labels[k8sLabelRole] {
	case k8sRoleVirtualCredential:
		if vc := secretToVirtualCredential(sec); vc != nil {
			s.mu.Lock()
			s.byAKID[vc.AccessKeyID] = vc
			s.mu.Unlock()
			s.publishQuota(sec, vc)
		}
	case k8sRoleAnonymousBinding:
		if a := secretToAnonymousBinding(sec); a != nil {
			s.mu.Lock()
			s.byBucket[strings.ToLower(a.BucketName)] = a
			s.mu.Unlock()
		}
	}
}

// publishQuota pushes the quota carried on a virtual-credential Secret
// into the configured LimitObserver. Either limit may be zero — the
// observer is responsible for deleting the entry when both are zero,
// since "no quota" must shadow nothing.
func (s *KubernetesSource) publishQuota(sec *corev1.Secret, vc *VirtualCredential) {
	if s.cfg.LimitObserver == nil {
		return
	}
	soft := parseInt64Bytes(sec.Data[k8sDataQuotaSoftBytes])
	hard := parseInt64Bytes(sec.Data[k8sDataQuotaHardBytes])
	for _, scope := range vc.BucketScopes {
		if soft == 0 && hard == 0 {
			s.cfg.LimitObserver.DeleteLimit(scope.BackendName, scope.BucketName)
			continue
		}
		s.cfg.LimitObserver.SetLimit(scope.BackendName, scope.BucketName, soft, hard)
	}
}

func parseInt64Bytes(raw []byte) int64 {
	if len(raw) == 0 {
		return 0
	}
	n, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (s *KubernetesSource) handleDelete(obj any) {
	sec, ok := obj.(*corev1.Secret)
	if !ok {
		// On stale watch reconnects the informer may deliver a tombstone
		// (DeletedFinalStateUnknown) instead of a Secret. Unwrap it.
		if t, isTomb := obj.(cache.DeletedFinalStateUnknown); isTomb {
			if s2, sok := t.Obj.(*corev1.Secret); sok {
				sec = s2
			}
		}
		if sec == nil {
			return
		}
	}
	switch sec.Labels[k8sLabelRole] {
	case k8sRoleVirtualCredential:
		akid := sec.Labels[k8sLabelBucket] // fallback to data if label missing
		if v := sec.Data[k8sDataAccessKeyID]; len(v) > 0 {
			akid = string(v)
		}
		if akid == "" {
			return
		}
		// Capture the scope set BEFORE removing from the AKID map so the
		// limit observer knows which (backend, bucket) entries to delete.
		s.mu.Lock()
		var scopes []BucketScope
		if vc, ok := s.byAKID[akid]; ok {
			scopes = vc.BucketScopes
		}
		delete(s.byAKID, akid)
		s.mu.Unlock()
		if s.cfg.LimitObserver != nil {
			for _, scope := range scopes {
				s.cfg.LimitObserver.DeleteLimit(scope.BackendName, scope.BucketName)
			}
		}
	case k8sRoleAnonymousBinding:
		bucket := sec.Labels[k8sLabelBucket]
		if bucket == "" {
			bucket = string(sec.Data[k8sDataBucketName])
		}
		if bucket == "" {
			return
		}
		s.mu.Lock()
		delete(s.byBucket, strings.ToLower(bucket))
		s.mu.Unlock()
	}
}

// secretToVirtualCredential decodes one operator-written Secret into our
// VirtualCredential shape. Returns nil when required data fields are
// missing or malformed; the source skips those entries entirely so a
// half-written Secret doesn't half-authorise traffic.
func secretToVirtualCredential(sec *corev1.Secret) *VirtualCredential {
	akid := string(sec.Data[k8sDataAccessKeyID])
	sk := string(sec.Data[k8sDataSecretAccessKey])
	primary := string(sec.Data[k8sDataBucketName])
	backend := string(sec.Data[k8sDataBackend])
	if akid == "" || sk == "" || backend == "" {
		return nil
	}

	vc := &VirtualCredential{
		AccessKeyID:     akid,
		SecretAccessKey: sk,
		BackendName:     backend,
		ClaimNamespace:  sec.Labels[k8sLabelClaimNS],
		ClaimName:       sec.Labels[k8sLabelClaimName],
		Source:          "kubernetes",
	}

	// bucket_scopes wins when present (multi-bucket grants); falls back to
	// the legacy 1:1 scope so older operator versions keep working.
	if raw, ok := sec.Data[k8sDataBucketScopes]; ok && len(raw) > 0 {
		var scopes []BucketScope
		if err := json.Unmarshal(raw, &scopes); err == nil && len(scopes) > 0 {
			vc.BucketScopes = scopes
		}
	}
	if len(vc.BucketScopes) == 0 && primary != "" {
		vc.BucketScopes = []BucketScope{{BucketName: primary, BackendName: backend}}
	}

	if exp := sec.Annotations[k8sAnnotationExpiresAt]; exp != "" {
		if t, err := time.Parse(time.RFC3339, exp); err == nil {
			vc.ExpiresAt = &t
		}
	}
	return vc
}

func secretToAnonymousBinding(sec *corev1.Secret) *AnonymousBinding {
	bucket := string(sec.Data[k8sDataBucketName])
	backend := string(sec.Data[k8sDataBackend])
	mode := string(sec.Data[k8sDataAnonMode])
	if bucket == "" || backend == "" || mode == "" {
		return nil
	}
	rps := float64(0)
	if raw := string(sec.Data[k8sDataAnonRPS]); raw != "" {
		if n, err := strconv.ParseFloat(raw, 64); err == nil {
			rps = n
		}
	}
	return &AnonymousBinding{
		BucketName:     bucket,
		BackendName:    backend,
		Mode:           mode,
		PerSourceIPRPS: rps,
		Source:         "kubernetes",
	}
}

// loadKubeConfig prefers an explicit file path, falls back to in-cluster
// config when the path is empty.
func loadKubeConfig(path string) (*rest.Config, error) {
	if path != "" {
		return clientcmd.BuildConfigFromFlags("", path)
	}
	return rest.InClusterConfig()
}
