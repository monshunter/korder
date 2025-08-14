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

// TicketPhase defines the phase of a ticket
type TicketPhase string

const (
	// TicketPending means the ticket is waiting to be activated
	TicketPending TicketPhase = "Pending"
	// TicketReady means the ticket is ready and guardian pod is running
	TicketReady TicketPhase = "Ready"
	// TicketClaimed means the ticket is claimed by a business pod
	TicketClaimed TicketPhase = "Claimed"
	// TicketExpired means the ticket has expired
	TicketExpired TicketPhase = "Expired"
)

// TicketSpec defines the desired state of Ticket
type TicketSpec struct {
	// Lifecycle defines lifecycle policies
	// +optional
	Lifecycle *TicketLifecycle `json:"lifecycle,omitempty"`

	// Window defines the time window for ticket validity
	// +optional
	Window *WindowSpec `json:"window,omitempty"`

	// SchedulerName defines which scheduler to use
	// +optional
	SchedulerName *string `json:"schedulerName,omitempty"`

	// PriorityClassName for the guardian pods
	// +optional
	PriorityClassName *string `json:"priorityClassName,omitempty"`

	// Resources required for this ticket
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// NodeName assigns the ticket to a specific node
	// +optional
	NodeName *string `json:"nodeName,omitempty"`

	// NodeSelector constrains the ticket to nodes with matching labels
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Affinity defines scheduling constraints
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Tolerations for the guardian pods
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// TopologySpreadConstraints for the guardian pods
	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
}

// TicketConditionType defines the type of ticket condition
type TicketConditionType string

const (
	// TicketReadyCondition indicates the ticket is ready
	TicketReadyCondition TicketConditionType = "Ready"
	// TicketGuardianReady indicates the guardian pod is ready
	TicketGuardianReady TicketConditionType = "GuardianReady"
	// TicketExpiring indicates the ticket is about to expire
	TicketExpiring TicketConditionType = "Expiring"
)

// TicketCondition describes the state of a ticket at a certain point
type TicketCondition struct {
	// Type of ticket condition
	Type TicketConditionType `json:"type"`

	// Status of the condition
	Status corev1.ConditionStatus `json:"status"`

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

// TicketStatus defines the observed state of Ticket
type TicketStatus struct {
	// Phase of the ticket lifecycle
	// +optional
	Phase TicketPhase `json:"phase,omitempty"`

	// ActivationTime when the ticket was activated
	// +optional
	ActivationTime *metav1.Time `json:"activationTime,omitempty"`

	// ExpirationTime when the ticket expires
	// +optional
	ExpirationTime *metav1.Time `json:"expirationTime,omitempty"`

	// ClaimedTime when the ticket was claimed
	// +optional
	ClaimedTime *metav1.Time `json:"claimedTime,omitempty"`

	// UtilizationRate of the reserved resources (as string to avoid float)
	// +optional
	UtilizationRate *string `json:"utilizationRate,omitempty"`

	// NodeName where the guardian pod is running
	// +optional
	NodeName *string `json:"nodeName,omitempty"`

	// GuardianPod refers to the guardian pod in format "namespace/name"
	// +optional
	GuardianPod *string `json:"guardianPod,omitempty"`

	// ClaimedPod refers to the business pod that claimed this ticket in format "namespace/name"
	// +optional
	ClaimedPod *string `json:"claimedPod,omitempty"`

	// Message providing details about the current state
	// +optional
	Message *string `json:"message,omitempty"`

	// Reason for the current state
	// +optional
	Reason *string `json:"reason,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed Ticket
	// +optional
	ObservedGeneration *int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the ticket's current state
	// +optional
	Conditions []TicketCondition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Ticket phase"
// +kubebuilder:printcolumn:name="Node",type="string",JSONPath=".status.nodeName",description="Node where ticket is assigned",priority=1
// +kubebuilder:printcolumn:name="Claimed-By",type="string",JSONPath=".status.claimedPod",description="Pod that claimed the ticket"
// +kubebuilder:printcolumn:name="Guardian-Pod",type="string",JSONPath=".status.guardianPod",description="Guardian pod holding the resources",priority=1
// +kubebuilder:printcolumn:name="Expiration",type="string",JSONPath=".status.expirationTime",description="When the ticket expires",priority=1
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Age of the ticket"

// Ticket is the Schema for the tickets API
type Ticket struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Ticket
	// +required
	Spec TicketSpec `json:"spec"`

	// status defines the observed state of Ticket
	// +optional
	Status TicketStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// TicketList contains a list of Ticket
type TicketList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Ticket `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Ticket{}, &TicketList{})
}
