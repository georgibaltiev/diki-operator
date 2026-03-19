// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package diki

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ScheduledComplianceScan describes a scheduled compliance scan.
type ScheduledComplianceScan struct {
	metav1.TypeMeta
	// Standard object metadata.
	metav1.ObjectMeta

	// Spec contains the specification of this scheduled compliance scan.
	Spec ScheduledComplianceScanSpec
	// Status contains the status of this scheduled compliance scan.
	Status ScheduledComplianceScanStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ScheduledComplianceScanList describes a list of scheduled compliance scans.
type ScheduledComplianceScanList struct {
	metav1.TypeMeta
	metav1.ListMeta

	// Items contains the list of ScheduledComplianceScans.
	Items []ScheduledComplianceScan
}

// ScheduledComplianceScanSpec is the specification of a ScheduledComplianceScan.
type ScheduledComplianceScanSpec struct {
	// Schedule is a cron expression that defines how often a ComplianceScan should be run.
	Schedule string
	// RunsHistoryLimit defines how many completed ComplianceScan CRs to retain.
	// Defaults to 4.
	RunsHistoryLimit *int32
	// RunTemplate is the template for the ComplianceScan that will be created.
	RunTemplate ComplianceScanTemplate
}

// ComplianceScanTemplate contains the spec of a ComplianceScan to be created.
type ComplianceScanTemplate struct {
	// Spec contains the specification of the ComplianceScan.
	Spec ComplianceScanSpec
}

// ScheduledComplianceScanStatus contains the status of a ScheduledComplianceScan.
type ScheduledComplianceScanStatus struct {
	// Active is a reference to the currently running ComplianceScan, if any.
	Active *corev1.ObjectReference
	// LastScheduleTime is the time when the last ComplianceScan was scheduled.
	LastScheduleTime *metav1.Time
	// LastCompletionTime is the time when the last ComplianceScan completed.
	LastCompletionTime *metav1.Time
}
