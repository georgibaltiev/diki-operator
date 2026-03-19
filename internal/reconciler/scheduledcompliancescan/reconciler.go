// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package reconciler

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configv1alpha1 "github.com/gardener/diki-operator/pkg/apis/config/v1alpha1"
	dikiv1alpha1 "github.com/gardener/diki-operator/pkg/apis/diki/v1alpha1"
)

const (
	// DefaultRunsHistoryLimit is the default number of completed ComplianceScan CRs to retain.
	DefaultRunsHistoryLimit = int32(4)
)

// Reconciler reconciles ScheduledComplianceScans.
type Reconciler struct {
	Client client.Client
	Config configv1alpha1.ScheduledComplianceScanConfig
}

// Reconcile handles reconciliation requests for ScheduledComplianceScan resources.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("name", req.Name)

	scheduled := &dikiv1alpha1.ScheduledComplianceScan{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: req.Name}, scheduled); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Object is gone, stop reconciling")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("error retrieving scheduledComplianceScan: %w", err)
	}

	schedule, err := cron.ParseStandard(scheduled.Spec.Schedule)
	if err != nil {
		log.Error(err, "Invalid cron schedule", "schedule", scheduled.Spec.Schedule)
		// Invalid schedule — nothing we can do without user fixing it.
		return reconcile.Result{}, nil
	}

	now := time.Now()

	// Manage active run.
	if scheduled.Status.Active != nil {
		activeScan := &dikiv1alpha1.ComplianceScan{}
		if err := r.Client.Get(ctx, client.ObjectKey{Name: scheduled.Status.Active.Name}, activeScan); err != nil {
			if !apierrors.IsNotFound(err) {
				return reconcile.Result{}, fmt.Errorf("error retrieving active ComplianceScan: %w", err)
			}
			// Scan was deleted externally — clear active reference.
			log.Info("Active ComplianceScan not found, clearing active reference")
			patch := client.MergeFrom(scheduled.DeepCopy())
			scheduled.Status.Active = nil
			if err := r.Client.Status().Patch(ctx, scheduled, patch); err != nil {
				return reconcile.Result{}, fmt.Errorf("error clearing active reference: %w", err)
			}
		} else if activeScan.Status.Phase == dikiv1alpha1.ComplianceScanCompleted || activeScan.Status.Phase == dikiv1alpha1.ComplianceScanFailed {
			log.Info("Active ComplianceScan finished", "phase", activeScan.Status.Phase)
			patch := client.MergeFrom(scheduled.DeepCopy())
			scheduled.Status.Active = nil
			scheduled.Status.LastCompletionTime = ptr.To(metav1.NewTime(now))
			if err := r.Client.Status().Patch(ctx, scheduled, patch); err != nil {
				return reconcile.Result{}, fmt.Errorf("error updating status after completion: %w", err)
			}
		} else {
			// Still running — requeue after the next schedule time to check again.
			var lastTime time.Time
			if scheduled.Status.LastScheduleTime != nil {
				lastTime = scheduled.Status.LastScheduleTime.Time
			} else {
				lastTime = scheduled.CreationTimestamp.Time
			}
			next := schedule.Next(lastTime)
			requeueAfter := next.Sub(now)
			if requeueAfter < 5*time.Second {
				requeueAfter = 5 * time.Second
			}
			log.Info("Active ComplianceScan still running, requeuing", "requeueAfter", requeueAfter)
			return reconcile.Result{RequeueAfter: requeueAfter}, nil
		}
	}

	// Determine when to start the next scan.
	var lastTime time.Time
	if scheduled.Status.LastScheduleTime != nil {
		lastTime = scheduled.Status.LastScheduleTime.Time
	} else {
		lastTime = scheduled.CreationTimestamp.Time
	}

	nextScheduleTime := schedule.Next(lastTime)
	if now.Before(nextScheduleTime) {
		requeueAfter := nextScheduleTime.Sub(now)
		log.Info("Not yet time for next scan", "nextScheduleTime", nextScheduleTime, "requeueAfter", requeueAfter)
		return reconcile.Result{RequeueAfter: requeueAfter}, nil
	}

	// Time to create a new ComplianceScan.
	scanName := fmt.Sprintf("%s-%d", scheduled.Name, now.Unix())
	newScan := &dikiv1alpha1.ComplianceScan{
		ObjectMeta: metav1.ObjectMeta{
			Name: scanName,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(scheduled, dikiv1alpha1.SchemeGroupVersion.WithKind("ScheduledComplianceScan")),
			},
		},
		Spec: scheduled.Spec.RunTemplate.Spec,
	}

	if err := r.Client.Create(ctx, newScan); err != nil {
		return reconcile.Result{}, fmt.Errorf("error creating ComplianceScan: %w", err)
	}
	log.Info("Created ComplianceScan", "name", scanName)

	patch := client.MergeFrom(scheduled.DeepCopy())
	scheduled.Status.Active = &corev1.ObjectReference{
		APIVersion: dikiv1alpha1.SchemeGroupVersion.String(),
		Kind:       "ComplianceScan",
		Name:       newScan.Name,
		UID:        newScan.UID,
	}
	scheduled.Status.LastScheduleTime = ptr.To(metav1.NewTime(now))
	if err := r.Client.Status().Patch(ctx, scheduled, patch); err != nil {
		return reconcile.Result{}, fmt.Errorf("error updating status after creating scan: %w", err)
	}

	// Cleanup history.
	if err := r.cleanupHistory(ctx, scheduled); err != nil {
		log.Error(err, "Error during history cleanup")
	}

	nextAfter := schedule.Next(now).Sub(now)
	return reconcile.Result{RequeueAfter: nextAfter}, nil
}

func (r *Reconciler) cleanupHistory(ctx context.Context, scheduled *dikiv1alpha1.ScheduledComplianceScan) error {
	scanList := &dikiv1alpha1.ComplianceScanList{}
	if err := r.Client.List(ctx, scanList); err != nil {
		return fmt.Errorf("error listing ComplianceScans: %w", err)
	}

	// Filter to scans owned by this ScheduledComplianceScan that are finished.
	var finished []dikiv1alpha1.ComplianceScan
	for _, scan := range scanList.Items {
		if !isOwnedBy(&scan, scheduled) {
			continue
		}
		if scan.Status.Phase == dikiv1alpha1.ComplianceScanCompleted || scan.Status.Phase == dikiv1alpha1.ComplianceScanFailed {
			finished = append(finished, scan)
		}
	}

	limit := DefaultRunsHistoryLimit
	if scheduled.Spec.RunsHistoryLimit != nil {
		limit = *scheduled.Spec.RunsHistoryLimit
	}

	if int32(len(finished)) <= limit {
		return nil
	}

	// Sort oldest first.
	sort.Slice(finished, func(i, j int) bool {
		return finished[i].CreationTimestamp.Before(&finished[j].CreationTimestamp)
	})

	toDelete := finished[:int32(len(finished))-limit]
	for _, scan := range toDelete {
		scan := scan
		if err := r.Client.Delete(ctx, &scan); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("error deleting ComplianceScan %s: %w", scan.Name, err)
		}
	}
	return nil
}

func isOwnedBy(scan *dikiv1alpha1.ComplianceScan, owner *dikiv1alpha1.ScheduledComplianceScan) bool {
	for _, ref := range scan.OwnerReferences {
		if ref.UID == owner.UID {
			return true
		}
	}
	return false
}
