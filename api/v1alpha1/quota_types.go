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

// QuotaScopeType defines the scope of a quota
type QuotaScopeType string

const (
	// ClusterScope applies quota cluster-wide
	ClusterScope QuotaScopeType = "Cluster"
	// NamespaceSelectorScope applies quota to namespaces matching selector
	NamespaceSelectorScope QuotaScopeType = "NamespaceSelector"
	// NamespaceListScope applies quota to explicitly listed namespaces
	NamespaceListScope QuotaScopeType = "NamespaceList"
	// ObjectSelectorScope applies quota to objects matching selector
	ObjectSelectorScope QuotaScopeType = "ObjectSelector"
)

// QuotaScope defines the scope of a quota
type QuotaScope struct {
	// Type defines the scope type
	// +kubebuilder:validation:Enum=Cluster;NamespaceSelector;NamespaceList;ObjectSelector
	Type QuotaScopeType `json:"type"`

	// NamespaceSelector selects namespaces when type is NamespaceSelector
	// +optional
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	// Namespaces lists explicit namespaces when type is NamespaceList
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`

	// ObjectSelector selects objects when type is ObjectSelector
	// +optional
	ObjectSelector *metav1.LabelSelector `json:"objectSelector,omitempty"`
}

// TimeWindow defines time-based quota limits
type TimeWindow struct {
	// Name of the time window
	Name string `json:"name"`

	// Schedule defines when this window applies (cron format)
	Schedule string `json:"schedule"`

	// Multiplier to apply to base quotas during this window (as string to avoid float)
	Multiplier string `json:"multiplier"`
}

// QuotaAllocation defines resource allocation for specific namespaces
type QuotaAllocation struct {
	// Namespace to allocate resources to
	Namespace string `json:"namespace"`

	// Limits defines the resource limits for this namespace
	Limits corev1.ResourceList `json:"limits"`
}

// QuotaSpec defines the desired state of Quota
type QuotaSpec struct {
	// Scope defines what this quota applies to
	Scope QuotaScope `json:"scope"`

	// Hard defines the maximum resource limits
	Hard corev1.ResourceList `json:"hard"`

	// TimeWindows defines time-based quota adjustments
	// +optional
	TimeWindows []TimeWindow `json:"timeWindows,omitempty"`

	// Allocation defines per-namespace resource allocation
	// +optional
	Allocation []QuotaAllocation `json:"allocation,omitempty"`
}

// QuotaConditionType defines the type of quota condition
type QuotaConditionType string

const (
	// QuotaReady indicates the quota is ready
	QuotaReady QuotaConditionType = "Ready"
	// QuotaEnforced indicates the quota is being enforced
	QuotaEnforced QuotaConditionType = "Enforced"
	// QuotaExceeded indicates the quota has been exceeded
	QuotaExceeded QuotaConditionType = "Exceeded"
)

// QuotaCondition describes the state of a quota at a certain point
type QuotaCondition struct {
	// Type of quota condition
	Type QuotaConditionType `json:"type"`

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

// QuotaStatus defines the observed state of Quota
type QuotaStatus struct {
	// Hard defines the maximum resource limits currently enforced
	// +optional
	Hard corev1.ResourceList `json:"hard,omitempty"`

	// Used defines the current resource usage
	// +optional
	Used corev1.ResourceList `json:"used,omitempty"`

	// ActiveTimeWindow indicates which time window is currently active
	// +optional
	ActiveTimeWindow *string `json:"activeTimeWindow,omitempty"`

	// ObservedGeneration reflects the generation of the most recently observed Quota
	// +optional
	ObservedGeneration *int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the quota's current state
	// +optional
	Conditions []QuotaCondition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// Quota is the Schema for the quotas API
type Quota struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Quota
	// +required
	Spec QuotaSpec `json:"spec"`

	// status defines the observed state of Quota
	// +optional
	Status QuotaStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// QuotaList contains a list of Quota
type QuotaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Quota `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Quota{}, &QuotaList{})
}
