// Copyright (C) 2026 Damian van der Merwe
// SPDX-License-Identifier: AGPL-3.0-or-later

package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	brokerv1a1 "github.com/stowage-dev/stowage/internal/operator/api/v1alpha1"
	"github.com/stowage-dev/stowage/internal/operator/backend"
	"github.com/stowage-dev/stowage/internal/operator/credentials"
	"github.com/stowage-dev/stowage/internal/operator/vcstore"
)

// BucketClaimReconciler provisions the real bucket and the two Secrets that
// make a BucketClaim usable.
type BucketClaimReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Resolver   *credentials.Resolver
	Writer     *vcstore.Writer
	Recorder   record.EventRecorder
	ProxyURL   string
	OpsNS      string
	MaxWorkers int
}

// +kubebuilder:rbac:groups=broker.stowage.io,resources=bucketclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=broker.stowage.io,resources=bucketclaims/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=broker.stowage.io,resources=bucketclaims/finalizers,verbs=update
// +kubebuilder:rbac:groups=broker.stowage.io,resources=s3backends,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

func (r *BucketClaimReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("claim", req.NamespacedName)

	var claim brokerv1a1.BucketClaim
	if err := r.Get(ctx, req.NamespacedName, &claim); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	var bck brokerv1a1.S3Backend
	if err := r.Get(ctx, types.NamespacedName{Name: claim.Spec.BackendRef.Name}, &bck); err != nil {
		if apierrors.IsNotFound(err) {
			r.setClaimReady(&claim, metav1.ConditionFalse, brokerv1a1.ReasonBackendNotReady, "backend not found")
			return r.patchStatus(ctx, &claim, 30*time.Second)
		}
		return ctrl.Result{}, err
	}
	if !backendReady(&bck) {
		r.setClaimReady(&claim, metav1.ConditionFalse, brokerv1a1.ReasonBackendNotReady, "backend not ready")
		return r.patchStatus(ctx, &claim, 30*time.Second)
	}

	if !claim.DeletionTimestamp.IsZero() {
		return r.handleDelete(ctx, logger, &claim, &bck)
	}

	if !containsFinalizer(&claim, brokerv1a1.Finalizer) {
		addFinalizer(&claim, brokerv1a1.Finalizer)
		if err := r.Update(ctx, &claim); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	bucketName, err := backend.RenderBucketName(claim.Spec.BucketName, bck.Spec.BucketNameTemplate, claim.Namespace, claim.Name)
	if err != nil {
		r.setClaimReady(&claim, metav1.ConditionFalse, brokerv1a1.ReasonTemplateInvalid, err.Error())
		return r.patchStatus(ctx, &claim, 0)
	}

	admin, err := r.resolveAdmin(ctx, &bck)
	if err != nil {
		r.setClaimReady(&claim, metav1.ConditionFalse, brokerv1a1.ReasonCredentialsInvalid, err.Error())
		return r.patchStatus(ctx, &claim, 30*time.Second)
	}

	ops, err := r.buildOps(&bck, admin)
	if err != nil {
		r.setClaimReady(&claim, metav1.ConditionFalse, brokerv1a1.ReasonBackendError, err.Error())
		return r.patchStatus(ctx, &claim, 30*time.Second)
	}

	claim.Status.Phase = brokerv1a1.PhaseCreating
	if err := ops.HeadBucket(ctx, bucketName); err != nil {
		if !errors.Is(err, backend.ErrBucketNotFound) {
			r.setClaimReady(&claim, metav1.ConditionFalse, brokerv1a1.ReasonBackendError, err.Error())
			return r.patchStatus(ctx, &claim, 30*time.Second)
		}
		if err := ops.PutBucket(ctx, bucketName); err != nil {
			r.setClaimReady(&claim, metav1.ConditionFalse, brokerv1a1.ReasonBackendError, fmt.Sprintf("create bucket: %v", err))
			return r.patchStatus(ctx, &claim, 30*time.Second)
		}
		r.Recorder.Eventf(&claim, corev1.EventTypeNormal, "Creating", "bucket %s created on backend", bucketName)
		if err := ops.HeadBucket(ctx, bucketName); err != nil {
			r.setClaimReady(&claim, metav1.ConditionFalse, brokerv1a1.ReasonCreationInconsistent, "bucket still 404 after create")
			return r.patchStatus(ctx, &claim, 15*time.Second)
		}
	}
	claim.Status.Conditions = setCondition(claim.Status.Conditions, metav1.Condition{
		Type:   brokerv1a1.ConditionBucketCreated,
		Status: metav1.ConditionTrue,
		Reason: brokerv1a1.ReasonCreatedOnBackend,
	})

	vc, rotatedAt, err := r.ensureCredential(ctx, &claim, &bck, bucketName)
	if err != nil {
		r.setClaimReady(&claim, metav1.ConditionFalse, brokerv1a1.ReasonBackendError, err.Error())
		return r.patchStatus(ctx, &claim, 30*time.Second)
	}
	if err := r.sweepExpired(ctx, logger, &claim); err != nil {
		logger.Info("sweep expired failed", "err", err)
	}

	secretName := r.consumerSecretName(&claim)
	if err := r.Writer.WriteConsumer(ctx, &claim, secretName, vcstore.ConsumerValues{
		AccessKeyID:     vc.AccessKeyID,
		SecretAccessKey: vc.SecretAccessKey,
		Region:          bck.Spec.Region,
		EndpointURL:     r.ProxyURL,
		BucketName:      bucketName,
		AddressingStyle: string(bck.Spec.AddressingStyle),
	}); err != nil {
		r.setClaimReady(&claim, metav1.ConditionFalse, brokerv1a1.ReasonBackendError, fmt.Sprintf("write consumer secret: %v", err))
		return r.patchStatus(ctx, &claim, 30*time.Second)
	}
	claim.Status.Conditions = setCondition(claim.Status.Conditions, metav1.Condition{
		Type:   brokerv1a1.ConditionCredentialsProvided,
		Status: metav1.ConditionTrue,
		Reason: brokerv1a1.ReasonSecretWritten,
	})

	if err := r.reconcileAnonymousBinding(ctx, &claim, bucketName); err != nil {
		r.setClaimReady(&claim, metav1.ConditionFalse, brokerv1a1.ReasonBackendError, fmt.Sprintf("anonymous binding: %v", err))
		return r.patchStatus(ctx, &claim, 30*time.Second)
	}

	claim.Status.Phase = brokerv1a1.PhaseBound
	claim.Status.BucketName = bucketName
	claim.Status.ProxyEndpoint = r.ProxyURL
	claim.Status.BoundSecretName = secretName
	claim.Status.AccessKeyID = vc.AccessKeyID
	if rotatedAt != nil {
		claim.Status.RotatedAt = rotatedAt
	}
	r.setClaimReady(&claim, metav1.ConditionTrue, brokerv1a1.ReasonBound, "bucket and credentials ready")
	r.Recorder.Event(&claim, corev1.EventTypeNormal, "Bound", "claim bound to backend")

	return r.patchStatus(ctx, &claim, r.nextRotation(&claim))
}

func (r *BucketClaimReconciler) handleDelete(ctx context.Context, logger logr.Logger, claim *brokerv1a1.BucketClaim, bck *brokerv1a1.S3Backend) (ctrl.Result, error) {
	claim.Status.Phase = brokerv1a1.PhaseDeleting
	policy := claim.Spec.DeletionPolicy
	if policy == "" {
		policy = brokerv1a1.DeletionPolicyRetain
	}

	if policy == brokerv1a1.DeletionPolicyDelete {
		admin, err := r.resolveAdmin(ctx, bck)
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		ops, err := r.buildOps(bck, admin)
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		bucketName := claim.Status.BucketName
		if bucketName == "" {
			bucketName, _ = backend.RenderBucketName(claim.Spec.BucketName, bck.Spec.BucketNameTemplate, claim.Namespace, claim.Name)
		}
		if bucketName != "" {
			if err := ops.HeadBucket(ctx, bucketName); err == nil {
				if err := ops.DeleteBucket(ctx, bucketName); err != nil {
					if errors.Is(err, backend.ErrBucketNotEmpty) {
						if !claim.Spec.ForceDelete {
							r.setClaimReady(claim, metav1.ConditionFalse, brokerv1a1.ReasonBucketNotEmpty, "bucket non-empty; set forceDelete=true to purge")
							r.Recorder.Event(claim, corev1.EventTypeWarning, "BucketNotEmpty", "refusing to delete non-empty bucket")
							if _, err := r.patchStatus(ctx, claim, time.Minute); err != nil {
								return ctrl.Result{}, err
							}
							return ctrl.Result{RequeueAfter: time.Minute}, nil
						}
						if err := ops.EmptyBucket(ctx, bucketName); err != nil {
							logger.Info("empty bucket failed", "err", err)
							return ctrl.Result{RequeueAfter: time.Minute}, nil
						}
						if err := ops.DeleteBucket(ctx, bucketName); err != nil {
							logger.Info("delete bucket after empty failed", "err", err)
							return ctrl.Result{RequeueAfter: time.Minute}, nil
						}
					} else if !errors.Is(err, backend.ErrBucketNotFound) {
						logger.Info("delete bucket failed", "err", err)
						return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
					}
				}
			} else if !errors.Is(err, backend.ErrBucketNotFound) {
				logger.Info("head bucket during delete failed", "err", err)
				return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
			}
		}
	}

	if err := r.Writer.DeleteInternalByClaim(ctx, claim.Namespace, claim.Name); err != nil {
		logger.Info("delete internal secrets failed", "err", err)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}
	if err := r.Writer.DeleteAnonymousBindingByClaim(ctx, claim.Namespace, claim.Name); err != nil {
		logger.Info("delete anonymous binding failed", "err", err)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
	}

	removeFinalizer(claim, brokerv1a1.Finalizer)
	if err := r.Update(ctx, claim); err != nil {
		return ctrl.Result{}, err
	}
	r.Recorder.Event(claim, corev1.EventTypeNormal, "Deleted", "claim finalized")
	return ctrl.Result{}, nil
}

// ensureCredential returns the active credential for the claim, performing
// a rotation if the policy calls for one. Returns the credential and, if
// rotation just happened, the timestamp to record in status.
func (r *BucketClaimReconciler) ensureCredential(ctx context.Context, claim *brokerv1a1.BucketClaim, bck *brokerv1a1.S3Backend, bucketName string) (credentials.VirtualKey, *metav1.Time, error) {
	active, _, err := r.listCredentials(ctx, claim)
	if err != nil {
		return credentials.VirtualKey{}, nil, err
	}

	needRotation := r.rotationDue(claim, active)
	if len(active) == 0 || needRotation {
		key, err := credentials.GenerateVirtual()
		if err != nil {
			return credentials.VirtualKey{}, nil, err
		}
		soft, hard := quotaBytes(claim.Spec.Quota)
		if err := r.Writer.WriteInternal(ctx, vcstore.VirtualCredential{
			AccessKeyID:     key.AccessKeyID,
			SecretAccessKey: key.SecretAccessKey,
			BucketName:      bucketName,
			ClaimNamespace:  claim.Namespace,
			ClaimName:       claim.Name,
			ClaimUID:        string(claim.UID),
			BackendName:     bck.Name,
			QuotaSoftBytes:  soft,
			QuotaHardBytes:  hard,
		}); err != nil {
			return credentials.VirtualKey{}, nil, err
		}

		// Mark previous active credentials with an expiry so they remain
		// usable during the overlap window.
		if needRotation {
			overlap := 300 * time.Second
			if claim.Spec.RotationPolicy != nil && claim.Spec.RotationPolicy.OverlapSeconds > 0 {
				overlap = time.Duration(claim.Spec.RotationPolicy.OverlapSeconds) * time.Second
			}
			expires := time.Now().Add(overlap)
			for i := range active {
				r.markExpiring(ctx, &active[i], expires)
			}
			r.Recorder.Eventf(claim, corev1.EventTypeNormal, "Rotated", "virtual credential rotated; overlap %s", overlap)
			now := metav1.Now()
			return key, &now, nil
		}
		return key, nil, nil
	}

	sec := active[0]
	return credentials.VirtualKey{
		AccessKeyID:     string(sec.Data[vcstore.DataAccessKeyID]),
		SecretAccessKey: string(sec.Data[vcstore.DataSecretAccessKey]),
	}, nil, nil
}

func (r *BucketClaimReconciler) rotationDue(claim *brokerv1a1.BucketClaim, active []corev1.Secret) bool {
	if len(active) == 0 {
		return false
	}
	rp := claim.Spec.RotationPolicy
	if rp == nil || rp.Mode != brokerv1a1.RotationModeTimeBased || rp.IntervalDays <= 0 {
		return false
	}
	interval := time.Duration(rp.IntervalDays) * 24 * time.Hour
	if claim.Status.RotatedAt == nil {
		// Treat first reconcile as "just rotated"; actual rotation starts after the interval.
		return false
	}
	return time.Since(claim.Status.RotatedAt.Time) >= interval
}

func (r *BucketClaimReconciler) markExpiring(ctx context.Context, sec *corev1.Secret, at time.Time) {
	if sec.Annotations == nil {
		sec.Annotations = map[string]string{}
	}
	if _, ok := sec.Annotations[vcstore.AnnotationExpiresAt]; ok {
		return
	}
	sec.Annotations[vcstore.AnnotationExpiresAt] = at.UTC().Format(time.RFC3339)
	_ = r.Update(ctx, sec)
}

func (r *BucketClaimReconciler) sweepExpired(ctx context.Context, logger logr.Logger, claim *brokerv1a1.BucketClaim) error {
	_, expired, err := r.listCredentials(ctx, claim)
	if err != nil {
		return err
	}
	now := time.Now()
	for i := range expired {
		exp := expired[i].Annotations[vcstore.AnnotationExpiresAt]
		t, err := time.Parse(time.RFC3339, exp)
		if err != nil || now.Before(t) {
			continue
		}
		logger.Info("sweeping expired credential", "akid", expired[i].Labels[vcstore.LabelAccessKeyID])
		if err := r.Delete(ctx, &expired[i]); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (r *BucketClaimReconciler) listCredentials(ctx context.Context, claim *brokerv1a1.BucketClaim) (active, expiring []corev1.Secret, err error) {
	var list corev1.SecretList
	if err := r.List(ctx, &list,
		client.InNamespace(r.OpsNS),
		client.MatchingLabels{
			vcstore.LabelRole:      vcstore.RoleVirtualCredential,
			vcstore.LabelClaimNS:   claim.Namespace,
			vcstore.LabelClaimName: claim.Name,
		},
	); err != nil {
		return nil, nil, err
	}
	for i := range list.Items {
		if _, ok := list.Items[i].Annotations[vcstore.AnnotationExpiresAt]; ok {
			expiring = append(expiring, list.Items[i])
		} else {
			active = append(active, list.Items[i])
		}
	}
	return active, expiring, nil
}

func (r *BucketClaimReconciler) setClaimReady(claim *brokerv1a1.BucketClaim, status metav1.ConditionStatus, reason, msg string) {
	claim.Status.Conditions = setCondition(claim.Status.Conditions, metav1.Condition{
		Type:               brokerv1a1.ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            msg,
		ObservedGeneration: claim.Generation,
	})
	if status == metav1.ConditionFalse && claim.Status.Phase == brokerv1a1.PhaseBound {
		claim.Status.Phase = brokerv1a1.PhaseFailed
	}
}

func (r *BucketClaimReconciler) patchStatus(ctx context.Context, claim *brokerv1a1.BucketClaim, requeue time.Duration) (ctrl.Result, error) {
	claim.Status.ObservedGeneration = claim.Generation
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var current brokerv1a1.BucketClaim
		if err := r.Get(ctx, client.ObjectKeyFromObject(claim), &current); err != nil {
			return err
		}
		if equality.Semantic.DeepEqual(current.Status, claim.Status) {
			return nil
		}
		current.Status = claim.Status
		return r.Status().Update(ctx, &current)
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	if requeue > 0 {
		return ctrl.Result{RequeueAfter: requeue}, nil
	}
	return ctrl.Result{}, nil
}

// reconcileAnonymousBinding writes or removes the proxy-facing anonymous
// binding Secret depending on the claim's spec. A Mode of None or an
// unset AnonymousAccess block deletes any existing binding.
func (r *BucketClaimReconciler) reconcileAnonymousBinding(ctx context.Context, claim *brokerv1a1.BucketClaim, bucketName string) error {
	a := claim.Spec.AnonymousAccess
	if a == nil || a.Mode == "" || a.Mode == brokerv1a1.AnonymousModeNone {
		return r.Writer.DeleteAnonymousBindingByClaim(ctx, claim.Namespace, claim.Name)
	}
	return r.Writer.WriteAnonymousBinding(ctx, vcstore.AnonymousBinding{
		BucketName:     bucketName,
		BackendName:    claim.Spec.BackendRef.Name,
		Mode:           string(a.Mode),
		PerSourceIPRPS: a.PerSourceIPRPS,
		ClaimNamespace: claim.Namespace,
		ClaimName:      claim.Name,
		ClaimUID:       string(claim.UID),
	})
}

func (r *BucketClaimReconciler) consumerSecretName(claim *brokerv1a1.BucketClaim) string {
	if claim.Spec.WriteConnectionSecretToRef != nil && claim.Spec.WriteConnectionSecretToRef.Name != "" {
		return claim.Spec.WriteConnectionSecretToRef.Name
	}
	return claim.Name + "-s3"
}

func (r *BucketClaimReconciler) resolveAdmin(ctx context.Context, b *brokerv1a1.S3Backend) (credentials.Admin, error) {
	return r.Resolver.Resolve(ctx, credentials.AdminSecretRef{
		Name:           b.Spec.AdminCredentialsSecretRef.Name,
		Namespace:      b.Spec.AdminCredentialsSecretRef.Namespace,
		AccessKeyField: b.Spec.AdminCredentialsSecretRef.AccessKeyField,
		SecretKeyField: b.Spec.AdminCredentialsSecretRef.SecretKeyField,
	})
}

func (r *BucketClaimReconciler) buildOps(b *brokerv1a1.S3Backend, admin credentials.Admin) (*backend.Ops, error) {
	s3c, err := backend.NewClient(backend.Config{
		Endpoint:        b.Spec.Endpoint,
		Region:          b.Spec.Region,
		AccessKeyID:     admin.AccessKeyID,
		SecretAccessKey: admin.SecretAccessKey,
		UsePathStyle:    b.Spec.AddressingStyle != brokerv1a1.AddressingStyleVirtual,
	})
	if err != nil {
		return nil, err
	}
	return &backend.Ops{S3: s3c}, nil
}

func (r *BucketClaimReconciler) nextRotation(claim *brokerv1a1.BucketClaim) time.Duration {
	const defaultRequeue = 5 * time.Minute
	if claim.Spec.RotationPolicy == nil || claim.Spec.RotationPolicy.Mode != brokerv1a1.RotationModeTimeBased {
		return defaultRequeue
	}
	interval := time.Duration(claim.Spec.RotationPolicy.IntervalDays) * 24 * time.Hour
	if claim.Status.RotatedAt == nil {
		return defaultRequeue
	}
	next := claim.Status.RotatedAt.Add(interval)
	d := time.Until(next)
	if d < time.Minute {
		d = time.Minute
	}
	if d > defaultRequeue {
		d = defaultRequeue
	}
	return d
}

func (r *BucketClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.MaxWorkers == 0 {
		r.MaxWorkers = 5
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&brokerv1a1.BucketClaim{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxWorkers}).
		Complete(r)
}

func backendReady(b *brokerv1a1.S3Backend) bool {
	for _, c := range b.Status.Conditions {
		if c.Type == brokerv1a1.ConditionReady {
			return c.Status == metav1.ConditionTrue
		}
	}
	return false
}

// quotaBytes resolves the optional BucketQuota into (soft, hard) decimal
// byte counts for the proxy-facing Secret. Either or both may be zero
// when the claim doesn't set them; the writer omits the corresponding
// Secret data field on zero, so a missing limit on the K8s side surfaces
// as an absent field on the proxy side (i.e. unbounded).
func quotaBytes(q *brokerv1a1.BucketQuota) (soft, hard int64) {
	if q == nil {
		return 0, 0
	}
	if q.Soft != nil {
		soft = q.Soft.Value()
	}
	if q.Hard != nil {
		hard = q.Hard.Value()
	}
	return soft, hard
}
