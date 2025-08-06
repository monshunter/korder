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

// StrategyType defines the merge strategy for Order
type StrategyType string

const (
	// OneTimeStrategy creates tickets once
	OneTimeStrategy StrategyType = "OneTime"
	// ScheduledStrategy creates tickets at scheduled time
	ScheduledStrategy StrategyType = "Scheduled"
	// RecurringStrategy creates tickets repeatedly
	RecurringStrategy StrategyType = "Recurring"
)

// RefreshPolicy defines when tickets should be refreshed
type RefreshPolicy string

const (
	// AlwaysRefresh always refresh tickets
	AlwaysRefresh RefreshPolicy = "Always"
	// OnClaimRefresh refresh tickets when claimed
	OnClaimRefresh RefreshPolicy = "OnClaim"
	// NeverRefresh never refresh tickets
	NeverRefresh RefreshPolicy = "Never"
)

// CleanupPolicy defines how expired tickets should be handled
type CleanupPolicy string

const (
	// DeleteCleanup deletes the ticket when expired
	DeleteCleanup CleanupPolicy = "Delete"
	// RetainCleanup retains the ticket when expired
	RetainCleanup CleanupPolicy = "Retain"
)

// TicketLifecycle defines lifecycle policies for tickets
type TicketLifecycle struct {
	// TTLSecondsAfterFinished defines how long to keep tickets after completion
	// +optional
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`

	// CleanupPolicy defines how to handle expired tickets
	// +kubebuilder:validation:Enum=Delete;Retain
	// +kubebuilder:default=Delete
	CleanupPolicy CleanupPolicy `json:"cleanupPolicy,omitempty"`
}

// OrderStrategy defines how tickets are created and managed
type OrderStrategy struct {
	// Type defines the strategy type
	// +kubebuilder:validation:Enum=OneTime;Scheduled;Recurring
	Type StrategyType `json:"type"`

	// Schedule defines cron expression for Scheduled/Recurring strategies
	// +optional
	Schedule *string `json:"schedule,omitempty"`

	// RefreshPolicy defines when to refresh tickets
	// +kubebuilder:validation:Enum=Always;OnClaim;Never
	// +kubebuilder:default=OnClaim
	RefreshPolicy RefreshPolicy `json:"refreshPolicy,omitempty"`
}

// TicketTemplate defines the template for creating tickets
type TicketTemplate struct {
	// Metadata for tickets created from this template
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec defines the ticket specification
	Spec TicketTemplateSpec `json:"spec"`
}

// TicketTemplateSpec defines the specification part of ticket template
type TicketTemplateSpec struct {
	// Lifecycle defines lifecycle policies
	// +optional
	Lifecycle *TicketLifecycle `json:"lifecycle,omitempty"`

	// StartTime defines when the ticket becomes active
	// Format: RFC3339
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// Duration defines how long the ticket is valid
	Duration *metav1.Duration `json:"duration,omitempty"`

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

// OrderSpec defines the desired state of Order
type OrderSpec struct {
	// Paused indicates that the order is paused
	// +optional
	Paused *bool `json:"paused,omitempty"`

	// Replicas defines the number of tickets to create
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=1
	Replicas *int32 `json:"replicas,omitempty"`

	// Strategy defines how tickets are created and managed
	Strategy OrderStrategy `json:"strategy"`

	// Selector for tickets created by this order
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`

	// Template defines the ticket template
	Template TicketTemplate `json:"template"`
}

// OrderConditionType defines the type of order condition
type OrderConditionType string

const (
	// OrderReady indicates the order is ready
	OrderReady OrderConditionType = "Ready"
	// OrderProgressing indicates the order is progressing
	OrderProgressing OrderConditionType = "Progressing"
	// OrderFailure indicates the order has failed
	OrderFailure OrderConditionType = "Failure"
)

// OrderCondition describes the state of an order at a certain point
type OrderCondition struct {
	// Type of order condition
	Type OrderConditionType `json:"type"`

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

// OrderStatus defines the observed state of Order
type OrderStatus struct {
	// Replicas is the number of desired tickets
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// AvailableReplicas is the number of available tickets
	// +optional
	AvailableReplicas *int32 `json:"availableReplicas,omitempty"`

	// UnavailableReplicas is the number of unavailable tickets
	// +optional
	UnavailableReplicas *int32 `json:"unavailableReplicas,omitempty"`

	// TerminalReplicas is the number of terminal tickets
	// +optional
	TerminalReplicas *int32 `json:"terminalReplicas,omitempty"`

	// UpdatedReplicas is the number of updated tickets
	// +optional
	UpdatedReplicas *int32 `json:"updatedReplicas,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed Order
	// +optional
	ObservedGeneration *int64 `json:"observedGeneration,omitempty"`

	// CollisionCount is the count of hash collisions for the Order
	// +optional
	CollisionCount *int32 `json:"collisionCount,omitempty"`

	// Conditions represent the latest available observations of the order's current state
	// +optional
	Conditions []OrderCondition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Order is the Schema for the orders API
type Order struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Order
	// +required
	Spec OrderSpec `json:"spec"`

	// status defines the observed state of Order
	// +optional
	Status OrderStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// OrderList contains a list of Order
type OrderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Order `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Order{}, &OrderList{})
}
