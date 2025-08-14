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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DaemonOrderSpec defines the desired state of DaemonOrder
type DaemonOrderSpec struct {
	// Paused indicates that the daemon order is paused
	// +optional
	Paused *bool `json:"paused,omitempty"`

	// RefreshPolicy defines when to refresh tickets
	// +kubebuilder:validation:Enum=Always;OnClaim;Never
	// +kubebuilder:default=OnClaim
	RefreshPolicy RefreshPolicy `json:"refreshPolicy,omitempty"`

	// Selector for tickets created by this daemon order
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// Template defines the ticket template
	Template TicketTemplate `json:"template"`
}

// DaemonOrderConditionType defines the type of daemon order condition
type DaemonOrderConditionType string

const (
	// DaemonOrderReady indicates the daemon order is ready
	DaemonOrderReady DaemonOrderConditionType = "Ready"
	// DaemonOrderProgressing indicates the daemon order is progressing
	DaemonOrderProgressing DaemonOrderConditionType = "Progressing"
	// DaemonOrderFailure indicates the daemon order has failed
	DaemonOrderFailure DaemonOrderConditionType = "Failure"
)

// DaemonOrderCondition describes the state of a daemon order at a certain point
type DaemonOrderCondition struct {
	// Type of daemon order condition
	Type DaemonOrderConditionType `json:"type"`

	// Status of the condition
	Status metav1.ConditionStatus `json:"status"`

	// Last time the condition transitioned
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`

	// Reason for the condition's last transition
	// +optional
	Reason *string `json:"reason,omitempty"`

	// Message providing details about the condition
	// +optional
	Message *string `json:"message,omitempty"`
}

// DaemonOrderStatus defines the observed state of DaemonOrder
type DaemonOrderStatus struct {
	// CurrentNumberScheduled is the number of nodes that are running at least 1 ticket
	// +optional
	CurrentNumberScheduled *int32 `json:"currentNumberScheduled,omitempty"`

	// NumberMisscheduled is the number of nodes that are running tickets but are not supposed to
	// +optional
	NumberMisscheduled *int32 `json:"numberMisscheduled,omitempty"`

	// DesiredNumberScheduled is the total number of nodes that should be running tickets
	// +optional
	DesiredNumberScheduled *int32 `json:"desiredNumberScheduled,omitempty"`

	// NumberReady is the number of nodes that should be running tickets and have one or more ready tickets
	// +optional
	NumberReady *int32 `json:"numberReady,omitempty"`

	// UpdatedNumberScheduled is the total number of nodes that are running updated tickets
	// +optional
	UpdatedNumberScheduled *int32 `json:"updatedNumberScheduled,omitempty"`

	// NumberAvailable is the number of nodes that should be running tickets and have available tickets
	// +optional
	NumberAvailable *int32 `json:"numberAvailable,omitempty"`

	// NumberUnavailable is the number of nodes that should be running tickets but have unavailable tickets
	// +optional
	NumberUnavailable *int32 `json:"numberUnavailable,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed DaemonOrder
	// +optional
	ObservedGeneration *int64 `json:"observedGeneration,omitempty"`

	// CollisionCount is the count of hash collisions for the DaemonOrder
	// +optional
	CollisionCount *int32 `json:"collisionCount,omitempty"`

	// Conditions represent the latest available observations of the daemon order's current state
	// +optional
	Conditions []DaemonOrderCondition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Desired",type="integer",JSONPath=".status.desiredNumberScheduled",description="Number of nodes that should be running tickets"
// +kubebuilder:printcolumn:name="Current",type="integer",JSONPath=".status.currentNumberScheduled",description="Number of nodes that are running tickets"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.numberReady",description="Number of nodes with ready tickets"
// +kubebuilder:printcolumn:name="Up-to-date",type="integer",JSONPath=".status.updatedNumberScheduled",description="Number of nodes with updated tickets"
// +kubebuilder:printcolumn:name="Available",type="integer",JSONPath=".status.numberAvailable",description="Number of nodes with available tickets"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of the daemon order"

// DaemonOrder is the Schema for the daemonorders API
type DaemonOrder struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of DaemonOrder
	// +required
	Spec DaemonOrderSpec `json:"spec"`

	// status defines the observed state of DaemonOrder
	// +optional
	Status DaemonOrderStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// DaemonOrderList contains a list of DaemonOrder
type DaemonOrderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DaemonOrder `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DaemonOrder{}, &DaemonOrderList{})
}
