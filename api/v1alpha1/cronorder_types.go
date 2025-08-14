/*
Copyright 2025 monshunter.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConcurrencyPolicy defines how concurrent executions of an Order should be handled
type ConcurrencyPolicy string

const (
	// AllowConcurrent allows concurrent runs; this is the default
	AllowConcurrent ConcurrencyPolicy = "Allow"
	// ForbidConcurrent forbids concurrent runs, skipping next run if previous hasn't finished yet
	ForbidConcurrent ConcurrencyPolicy = "Forbid"
	// ReplaceConcurrent cancels currently running order and replaces it with a new one
	ReplaceConcurrent ConcurrencyPolicy = "Replace"
)

// OrderTemplate describes the Order that will be created when executing a CronOrder
type OrderTemplate struct {
	// Metadata for orders created from this template
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the order specification
	Spec OrderSpec `json:"spec"`
}

// CronOrderSpec defines the desired state of CronOrder
type CronOrderSpec struct {
	// Schedule in Cron format, see https://en.wikipedia.org/wiki/Cron
	Schedule string `json:"schedule"`

	// TimeZone name for the given schedule, see https://en.wikipedia.org/wiki/List_of_tz_database_time_zones
	// If not specified, this will default to the timezone of the kube-controller-manager process
	// +optional
	TimeZone *string `json:"timeZone,omitempty"`

	// Suspend indicates that the CronOrder is suspended
	// +optional
	Suspend *bool `json:"suspend,omitempty"`

	// StartingDeadlineSeconds defines the deadline in seconds for starting the order if it misses scheduled time
	// +optional
	StartingDeadlineSeconds *int64 `json:"startingDeadlineSeconds,omitempty"`

	// ConcurrencyPolicy defines how to treat concurrent executions of an Order
	// +kubebuilder:validation:Enum=Allow;Forbid;Replace
	// +kubebuilder:default=Allow
	ConcurrencyPolicy ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`

	// SuccessfulJobsHistoryLimit defines the number of successful finished Orders to retain
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=3
	SuccessfulJobsHistoryLimit *int32 `json:"successfulJobsHistoryLimit,omitempty"`

	// FailedJobsHistoryLimit defines the number of failed finished Orders to retain
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=1
	FailedJobsHistoryLimit *int32 `json:"failedJobsHistoryLimit,omitempty"`

	// OrderTemplate defines the Order that will be created when executing a CronOrder
	OrderTemplate OrderTemplate `json:"orderTemplate"`
}

// CronOrderStatus defines the observed state of CronOrder
type CronOrderStatus struct {
	// Active holds pointers to currently running Orders
	// +optional
	Active []corev1.ObjectReference `json:"active,omitempty"`

	// LastScheduleTime holds information about when we last scheduled an Order
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`

	// LastSuccessfulTime holds information about when we last successfully completed an Order
	// +optional
	LastSuccessfulTime *metav1.Time `json:"lastSuccessfulTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Schedule",type="string",JSONPath=".spec.schedule",description="Cron schedule for creating orders"
// +kubebuilder:printcolumn:name="Suspend",type="boolean",JSONPath=".spec.suspend",description="Whether the cron order is suspended"
// +kubebuilder:printcolumn:name="Active",type="integer",JSONPath=".status.active",description="Number of active orders"
// +kubebuilder:printcolumn:name="Last Schedule",type="date",JSONPath=".status.lastScheduleTime",description="When the last order was scheduled"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of the cron order"

// CronOrder is the Schema for the cronorders API
type CronOrder struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of CronOrder
	// +required
	Spec CronOrderSpec `json:"spec"`

	// status defines the observed state of CronOrder
	// +optional
	Status CronOrderStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// CronOrderList contains a list of CronOrder
type CronOrderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CronOrder `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CronOrder{}, &CronOrderList{})
}
