// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package reconciler_test

import (
	"context"
	"fmt"
	"time"

	"github.com/gardener/gardener/pkg/client/kubernetes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	logzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	scheduledcompliancescan "github.com/gardener/diki-operator/internal/reconciler/scheduledcompliancescan"
	dikiinstall "github.com/gardener/diki-operator/pkg/apis/diki/install"
	dikiv1alpha1 "github.com/gardener/diki-operator/pkg/apis/diki/v1alpha1"
)

var _ = Describe("ScheduledComplianceScan Controller", func() {
	var (
		ctx = logf.IntoContext(context.Background(), logzap.New(logzap.WriteTo(GinkgoWriter)))

		cr         *scheduledcompliancescan.Reconciler
		fakeClient client.Client

		request reconcile.Request

		scheduled *dikiv1alpha1.ScheduledComplianceScan
	)

	BeforeEach(func() {
		scheme := runtime.NewScheme()
		Expect(kubernetes.AddGardenSchemeToScheme(scheme)).To(Succeed())
		Expect(dikiinstall.AddToScheme(scheme)).To(Succeed())

		fakeClient = fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&dikiv1alpha1.ScheduledComplianceScan{}, &dikiv1alpha1.ComplianceScan{}).
			Build()

		cr = &scheduledcompliancescan.Reconciler{
			Client: fakeClient,
		}

		// "* * * * *" fires every minute; with creation 1 year ago the schedule is always due.
		scheduled = &dikiv1alpha1.ScheduledComplianceScan{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "scheduled",
				UID:               types.UID("test-uid"),
				CreationTimestamp: metav1.Time{Time: time.Now().Add(-365 * 24 * time.Hour)},
			},
			Spec: dikiv1alpha1.ScheduledComplianceScanSpec{
				Schedule:         "* * * * *",
				RunsHistoryLimit: ptr.To(int32(2)),
				RunTemplate: dikiv1alpha1.ComplianceScanTemplate{
					Spec: dikiv1alpha1.ComplianceScanSpec{
						Rulesets: []dikiv1alpha1.RulesetConfig{
							{ID: "FAKE", Version: "v1"},
						},
					},
				},
			},
		}

		request = reconcile.Request{NamespacedName: types.NamespacedName{Name: scheduled.Name}}
	})

	It("should return without error if ScheduledComplianceScan is not found", func() {
		res, err := cr.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("should return without error if cron schedule is invalid", func() {
		scheduled.Spec.Schedule = "not-a-cron"
		Expect(fakeClient.Create(ctx, scheduled)).To(Succeed())

		res, err := cr.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("should not create a scan if the schedule has not elapsed", func() {
		// Set lastScheduleTime to just now — next fire is ~1 minute away.
		now := metav1.Now()
		Expect(fakeClient.Create(ctx, scheduled)).To(Succeed())
		scheduled.Status.LastScheduleTime = &now
		Expect(fakeClient.Status().Update(ctx, scheduled)).To(Succeed())

		res, err := cr.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))
		Expect(res.RequeueAfter).To(BeNumerically("<=", time.Minute))

		scanList := &dikiv1alpha1.ComplianceScanList{}
		Expect(fakeClient.List(ctx, scanList)).To(Succeed())
		Expect(scanList.Items).To(BeEmpty())
	})

	It("should create a ComplianceScan child when schedule is due", func() {
		Expect(fakeClient.Create(ctx, scheduled)).To(Succeed())

		res, err := cr.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))

		// Verify scan was created.
		scanList := &dikiv1alpha1.ComplianceScanList{}
		Expect(fakeClient.List(ctx, scanList)).To(Succeed())
		Expect(scanList.Items).To(HaveLen(1))

		scan := scanList.Items[0]
		Expect(scan.Name).To(HavePrefix("scheduled-"))
		Expect(scan.Spec.Rulesets).To(HaveLen(1))
		Expect(scan.Spec.Rulesets[0].ID).To(Equal("FAKE"))
		Expect(scan.OwnerReferences).To(HaveLen(1))
		Expect(scan.OwnerReferences[0].UID).To(Equal(scheduled.UID))

		// Verify status updated.
		Expect(fakeClient.Get(ctx, client.ObjectKey{Name: scheduled.Name}, scheduled)).To(Succeed())
		Expect(scheduled.Status.Active).NotTo(BeNil())
		Expect(scheduled.Status.Active.Name).To(Equal(scan.Name))
		Expect(scheduled.Status.LastScheduleTime).NotTo(BeNil())
	})

	It("should requeue without creating a new scan when active scan is still running", func() {
		Expect(fakeClient.Create(ctx, scheduled)).To(Succeed())

		// First reconcile — creates a scan.
		_, err := cr.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		Expect(fakeClient.Get(ctx, client.ObjectKey{Name: scheduled.Name}, scheduled)).To(Succeed())
		Expect(scheduled.Status.Active).NotTo(BeNil())

		scanList := &dikiv1alpha1.ComplianceScanList{}
		Expect(fakeClient.List(ctx, scanList)).To(Succeed())
		Expect(scanList.Items).To(HaveLen(1))

		// Active scan has empty phase (still pending/running) — second reconcile should NOT create another.
		res, err := cr.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))

		Expect(fakeClient.List(ctx, scanList)).To(Succeed())
		Expect(scanList.Items).To(HaveLen(1))
	})

	It("should clear active and set lastCompletionTime when active scan completes", func() {
		Expect(fakeClient.Create(ctx, scheduled)).To(Succeed())

		// First reconcile creates a scan.
		_, err := cr.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		Expect(fakeClient.Get(ctx, client.ObjectKey{Name: scheduled.Name}, scheduled)).To(Succeed())
		Expect(scheduled.Status.Active).NotTo(BeNil())
		activeName := scheduled.Status.Active.Name

		// Mark the scan as completed.
		scan := &dikiv1alpha1.ComplianceScan{}
		Expect(fakeClient.Get(ctx, client.ObjectKey{Name: activeName}, scan)).To(Succeed())
		scan.Status.Phase = dikiv1alpha1.ComplianceScanCompleted
		Expect(fakeClient.Status().Update(ctx, scan)).To(Succeed())

		// Second reconcile — should detect completion.
		_, err = cr.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		Expect(fakeClient.Get(ctx, client.ObjectKey{Name: scheduled.Name}, scheduled)).To(Succeed())
		Expect(scheduled.Status.Active).To(BeNil())
		Expect(scheduled.Status.LastCompletionTime).NotTo(BeNil())
	})

	It("should delete oldest scans beyond RunsHistoryLimit", func() {
		Expect(fakeClient.Create(ctx, scheduled)).To(Succeed())

		// Create 3 finished scans owned by scheduled.
		for i := 0; i < 3; i++ {
			scan := &dikiv1alpha1.ComplianceScan{
				ObjectMeta: metav1.ObjectMeta{
					Name:              fmt.Sprintf("old-scan-%d", i),
					CreationTimestamp: metav1.Time{Time: time.Now().Add(time.Duration(i) * time.Minute)},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: dikiv1alpha1.SchemeGroupVersion.String(),
							Kind:       "ScheduledComplianceScan",
							Name:       scheduled.Name,
							UID:        scheduled.UID,
							Controller: ptr.To(true),
						},
					},
				},
				Spec: dikiv1alpha1.ComplianceScanSpec{
					Rulesets: []dikiv1alpha1.RulesetConfig{{ID: "FAKE", Version: "v1"}},
				},
			}
			Expect(fakeClient.Create(ctx, scan)).To(Succeed())
			scan.Status.Phase = dikiv1alpha1.ComplianceScanCompleted
			Expect(fakeClient.Status().Update(ctx, scan)).To(Succeed())
		}

		// Reconcile — schedule is due, creates a 4th scan and triggers cleanup.
		_, err := cr.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		// With limit=2, 1 of the 3 old finished scans should be deleted (oldest = old-scan-0).
		scanList := &dikiv1alpha1.ComplianceScanList{}
		Expect(fakeClient.List(ctx, scanList)).To(Succeed())

		names := make([]string, 0, len(scanList.Items))
		for _, s := range scanList.Items {
			names = append(names, s.Name)
		}
		Expect(names).NotTo(ContainElement("old-scan-0"))
		Expect(names).To(ContainElement("old-scan-1"))
		Expect(names).To(ContainElement("old-scan-2"))
	})

	It("should clear active reference if active scan was deleted externally", func() {
		Expect(fakeClient.Create(ctx, scheduled)).To(Succeed())

		// Manually set a dangling active reference pointing to a non-existent scan.
		Expect(fakeClient.Get(ctx, client.ObjectKey{Name: scheduled.Name}, scheduled)).To(Succeed())
		patch := client.MergeFrom(scheduled.DeepCopy())
		now := metav1.Now()
		scheduled.Status.Active = &corev1.ObjectReference{
			APIVersion: dikiv1alpha1.SchemeGroupVersion.String(),
			Kind:       "ComplianceScan",
			Name:       "ghost-scan",
		}
		scheduled.Status.LastScheduleTime = &now
		Expect(fakeClient.Status().Patch(ctx, scheduled, patch)).To(Succeed())

		_, err := cr.Reconcile(ctx, request)
		Expect(err).NotTo(HaveOccurred())

		Expect(fakeClient.Get(ctx, client.ObjectKey{Name: scheduled.Name}, scheduled)).To(Succeed())
		Expect(scheduled.Status.Active).To(BeNil())
	})
})
