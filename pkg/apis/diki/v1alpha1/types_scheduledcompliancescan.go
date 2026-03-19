// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope=Cluster,path=scheduledcompliancescans,shortName=scscan,singular=scheduledcompliancescan
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Schedule",type=string,JSONPath=`.spec.schedule`,description="Cron schedule for the compliance scan"
// +kubebuilder:printcolumn:name="Last Schedule",type=date,JSONPath=`.status.lastScheduleTime`,description="Time of the last scheduled compliance scan"
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`,description="Creation timestamp"

// ScheduledComplianceScan describes a scheduled compliance scan.
type ScheduledComplianceScan struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object metadata.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec contains the specification of this scheduled compliance scan.
	Spec ScheduledComplianceScanSpec `json:"spec,omitempty"`
	// Status contains the status of this scheduled compliance scan.
	Status ScheduledComplianceScanStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ScheduledComplianceScanList describes a list of scheduled compliance scans.
type ScheduledComplianceScanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items contains the list of ScheduledComplianceScans.
	Items []ScheduledComplianceScan `json:"items"`
}

// ScheduledComplianceScanSpec is the specification of a ScheduledComplianceScan.
type ScheduledComplianceScanSpec struct {
	// Schedule is a cron expression that defines how often a ComplianceScan should be run.
	// For example, "0 0 * * *" runs a scan every day at midnight.
	Schedule string `json:"schedule"`
	// RunsHistoryLimit defines how many completed ComplianceScan CRs to retain.
	// Defaults to 4.
	// +optional
	RunsHistoryLimit *int32 `json:"runsHistoryLimit,omitempty"`
	// RunTemplate is the template for the ComplianceScan that will be created.
	RunTemplate ComplianceScanTemplate `json:"runTemplate"`
}

// ComplianceScanTemplate contains the spec of a ComplianceScan to be created.
type ComplianceScanTemplate struct {
	// Spec contains the specification of the ComplianceScan.
	Spec ComplianceScanSpec `json:"spec"`
}

// ScheduledComplianceScanStatus contains the status of a ScheduledComplianceScan.
type ScheduledComplianceScanStatus struct {
	// Active is a reference to the currently running ComplianceScan, if any.
	// +optional
	Active *corev1.ObjectReference `json:"active,omitempty"`
	// LastScheduleTime is the time when the last ComplianceScan was scheduled.
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`
	// LastCompletionTime is the time when the last ComplianceScan completed.
	// +optional
	LastCompletionTime *metav1.Time `json:"lastCompletionTime,omitempty"`
}
