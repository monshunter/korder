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
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	corev1alpha1 "github.com/monshunter/korder/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var ticketlog = logf.Log.WithName("ticket-resource")

// SetupTicketWebhookWithManager registers the webhook for Ticket in the manager.
func SetupTicketWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&corev1alpha1.Ticket{}).
		WithValidator(&TicketCustomValidator{Client: mgr.GetClient()}).
		WithDefaulter(&TicketCustomDefaulter{Client: mgr.GetClient()}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-core-korder-dev-v1alpha1-ticket,mutating=true,failurePolicy=fail,sideEffects=None,groups=core.korder.dev,resources=tickets,verbs=create;update,versions=v1alpha1,name=mticket-v1alpha1.kb.io,admissionReviewVersions=v1

// TicketCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Ticket when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type TicketCustomDefaulter struct {
	Client client.Client
}

var _ webhook.CustomDefaulter = &TicketCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Ticket.
func (d *TicketCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	ticket, ok := obj.(*corev1alpha1.Ticket)
	if !ok {
		return fmt.Errorf("expected an Ticket object but got %T", obj)
	}

	ticketlog.Info("Setting defaults for Ticket", "name", ticket.GetName(), "namespace", ticket.GetNamespace())

	// Set default lifecycle policy
	if ticket.Spec.Lifecycle == nil {
		ticket.Spec.Lifecycle = &corev1alpha1.TicketLifecycle{
			CleanupPolicy: corev1alpha1.DeleteCleanup,
		}
	}

	// Set default TTL if cleanup policy is Delete but no TTL specified
	if ticket.Spec.Lifecycle.CleanupPolicy == corev1alpha1.DeleteCleanup && ticket.Spec.Lifecycle.TTLSecondsAfterFinished == nil {
		defaultTTL := int32(3600) // 1 hour
		ticket.Spec.Lifecycle.TTLSecondsAfterFinished = &defaultTTL
	}

	// Set default window duration if not specified
	if ticket.Spec.Window == nil {
		ticket.Spec.Window = &corev1alpha1.WindowSpec{
			Duration: "24h", // 24 hours default
		}
	} else if ticket.Spec.Window.Duration == "" {
		ticket.Spec.Window.Duration = "24h"
	}

	// Set default scheduler name
	if ticket.Spec.SchedulerName == nil {
		defaultScheduler := "default-scheduler"
		ticket.Spec.SchedulerName = &defaultScheduler
	}

	// Set default resource requests if not specified
	if ticket.Spec.Resources == nil {
		ticket.Spec.Resources = &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
		}
	}

	ticketlog.V(1).Info("Applied defaults to Ticket", "name", ticket.Name, "duration", ticket.Spec.Window.Duration)
	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-core-korder-dev-v1alpha1-ticket,mutating=false,failurePolicy=fail,sideEffects=None,groups=core.korder.dev,resources=tickets,verbs=create;update,versions=v1alpha1,name=vticket-v1alpha1.kb.io,admissionReviewVersions=v1

// TicketCustomValidator struct is responsible for validating the Ticket resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type TicketCustomValidator struct {
	Client client.Client
}

var _ webhook.CustomValidator = &TicketCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Ticket.
func (v *TicketCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	ticket, ok := obj.(*corev1alpha1.Ticket)
	if !ok {
		return nil, fmt.Errorf("expected a Ticket object but got %T", obj)
	}

	ticketlog.Info("Validating Ticket creation", "name", ticket.GetName(), "namespace", ticket.GetNamespace())

	// Validate ticket specification
	warnings, err := v.validateTicketSpec(ticket)
	if err != nil {
		return warnings, fmt.Errorf("ticket specification validation failed: %w", err)
	}

	// Validate quota compliance
	if err := v.validateTicketQuotaCompliance(ctx, ticket); err != nil {
		return warnings, fmt.Errorf("quota validation failed: %w", err)
	}

	ticketlog.V(1).Info("Ticket validation passed", "name", ticket.Name)
	return warnings, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Ticket.
func (v *TicketCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	ticket, ok := newObj.(*corev1alpha1.Ticket)
	if !ok {
		return nil, fmt.Errorf("expected a Ticket object for the newObj but got %T", newObj)
	}

	oldTicket, ok := oldObj.(*corev1alpha1.Ticket)
	if !ok {
		return nil, fmt.Errorf("expected a Ticket object for the oldObj but got %T", oldObj)
	}

	ticketlog.V(1).Info("Validating Ticket update", "name", ticket.Name)

	// Validate immutable fields
	warnings, err := v.validateTicketImmutableFields(oldTicket, ticket)
	if err != nil {
		return warnings, fmt.Errorf("immutable field validation failed: %w", err)
	}

	// Validate ticket specification
	specWarnings, err := v.validateTicketSpec(ticket)
	if err != nil {
		return append(warnings, specWarnings...), fmt.Errorf("ticket specification validation failed: %w", err)
	}

	return append(warnings, specWarnings...), nil
}

// validateTicketSpec validates the ticket specification
func (v *TicketCustomValidator) validateTicketSpec(ticket *corev1alpha1.Ticket) (admission.Warnings, error) {
	var warnings admission.Warnings

	// Validate window
	if ticket.Spec.Window != nil {
		// Validate duration format
		if ticket.Spec.Window.Duration != "" {
			if duration, err := time.ParseDuration(ticket.Spec.Window.Duration); err != nil {
				return warnings, fmt.Errorf("invalid window duration format: %w", err)
			} else if duration <= 0 {
				return warnings, fmt.Errorf("window duration must be positive")
			}
		}

		// Validate start time format if specified
		if ticket.Spec.Window.StartTime != nil && *ticket.Spec.Window.StartTime != "" {
			if startTime, err := time.Parse(time.RFC3339, *ticket.Spec.Window.StartTime); err != nil {
				return warnings, fmt.Errorf("invalid start time format, must be RFC3339: %w", err)
			} else if startTime.Before(time.Now()) {
				warnings = append(warnings, "Start time is in the past")
			}
		}
	}

	// Validate lifecycle policy
	if ticket.Spec.Lifecycle != nil {
		if ticket.Spec.Lifecycle.TTLSecondsAfterFinished != nil && *ticket.Spec.Lifecycle.TTLSecondsAfterFinished < 0 {
			return warnings, fmt.Errorf("TTL seconds cannot be negative")
		}
	}

	// Validate resources
	if ticket.Spec.Resources != nil {
		if err := v.validateTicketResources(ticket.Spec.Resources); err != nil {
			return warnings, fmt.Errorf("resource validation failed: %w", err)
		}
	}

	// Validate node selector conflicts
	if ticket.Spec.NodeName != nil && len(ticket.Spec.NodeSelector) > 0 {
		warnings = append(warnings, "Both nodeName and nodeSelector are specified; nodeName takes precedence")
	}

	return warnings, nil
}

// validateTicketResources validates resource requirements
func (v *TicketCustomValidator) validateTicketResources(resources *corev1.ResourceRequirements) error {
	// Validate that requests don't exceed limits
	if resources.Requests != nil && resources.Limits != nil {
		for resourceName, requestQuantity := range resources.Requests {
			if limitQuantity, exists := resources.Limits[resourceName]; exists {
				if requestQuantity.Cmp(limitQuantity) > 0 {
					return fmt.Errorf("resource request for %s exceeds limit", resourceName)
				}
			}
		}
	}

	// Validate resource values are non-negative
	for resourceName, quantity := range resources.Requests {
		if quantity.Sign() < 0 {
			return fmt.Errorf("resource request for %s cannot be negative", resourceName)
		}
	}

	for resourceName, quantity := range resources.Limits {
		if quantity.Sign() < 0 {
			return fmt.Errorf("resource limit for %s cannot be negative", resourceName)
		}
	}

	return nil
}

// validateTicketImmutableFields validates that immutable fields haven't changed
func (v *TicketCustomValidator) validateTicketImmutableFields(oldTicket, newTicket *corev1alpha1.Ticket) (admission.Warnings, error) {
	var warnings admission.Warnings

	// Window duration should be immutable after creation (to avoid confusion with guardian pods)
	if oldTicket.Spec.Window != nil && newTicket.Spec.Window != nil {
		if oldTicket.Spec.Window.Duration != newTicket.Spec.Window.Duration {
			warnings = append(warnings, "Changing window duration may not affect existing guardian pods")
		}
	}

	// Resource requirements should be immutable (to maintain guardian pod consistency)
	if oldTicket.Spec.Resources != nil && newTicket.Spec.Resources != nil {
		if !resourcesEqual(oldTicket.Spec.Resources, newTicket.Spec.Resources) {
			warnings = append(warnings, "Changing resource requirements may not affect existing guardian pods")
		}
	}

	// Node binding should be immutable once set
	if oldTicket.Spec.NodeName != nil && newTicket.Spec.NodeName != nil {
		if *oldTicket.Spec.NodeName != *newTicket.Spec.NodeName {
			return warnings, fmt.Errorf("node name cannot be changed once set")
		}
	}

	return warnings, nil
}

// resourcesEqual compares two resource requirements for equality
func resourcesEqual(old, new *corev1.ResourceRequirements) bool {
	// This is a simplified comparison - in practice, you might want more sophisticated comparison
	if len(old.Requests) != len(new.Requests) || len(old.Limits) != len(new.Limits) {
		return false
	}

	for name, quantity := range old.Requests {
		if newQuantity, exists := new.Requests[name]; !exists || !quantity.Equal(newQuantity) {
			return false
		}
	}

	for name, quantity := range old.Limits {
		if newQuantity, exists := new.Limits[name]; !exists || !quantity.Equal(newQuantity) {
			return false
		}
	}

	return true
}

// validateTicketQuotaCompliance validates that the ticket complies with quotas
func (v *TicketCustomValidator) validateTicketQuotaCompliance(ctx context.Context, ticket *corev1alpha1.Ticket) error {
	// Calculate resource requirements for this ticket
	resourceRequests := v.calculateTicketResourceRequests(ticket)

	// Use the same validation logic as Order webhook
	return v.validateAgainstQuotas(ctx, ticket.Namespace, resourceRequests)
}

// calculateTicketResourceRequests calculates the resource requests for a ticket
func (v *TicketCustomValidator) calculateTicketResourceRequests(ticket *corev1alpha1.Ticket) corev1.ResourceList {
	requests := make(corev1.ResourceList)

	// Add ticket count
	requests[corev1.ResourceName("tickets")] = *resource.NewQuantity(1, resource.DecimalSI)

	// Add reserved resources if specified
	if ticket.Spec.Resources != nil && ticket.Spec.Resources.Requests != nil {
		for resourceName, quantity := range ticket.Spec.Resources.Requests {
			reservedName := corev1.ResourceName("reserved." + string(resourceName))
			requests[reservedName] = quantity
		}
	}

	return requests
}

// validateAgainstQuotas validates resource requests against applicable quotas (simplified version)
func (v *TicketCustomValidator) validateAgainstQuotas(ctx context.Context, namespace string, resourceRequests corev1.ResourceList) error {
	// Get all quotas that might apply
	quotaList := &corev1alpha1.QuotaList{}
	if err := v.Client.List(ctx, quotaList); err != nil {
		return err
	}

	for _, quota := range quotaList.Items {
		if v.doesQuotaApply(quota, namespace) {
			// Simple check: ensure requests don't exceed quota limits
			for resourceName, requestQuantity := range resourceRequests {
				if limitQuantity, exists := quota.Spec.Hard[resourceName]; exists {
					if requestQuantity.Cmp(limitQuantity) > 0 {
						return fmt.Errorf("ticket request for %s (%v) exceeds quota limit (%v)", resourceName, requestQuantity, limitQuantity)
					}
				}
			}
		}
	}

	return nil
}

// doesQuotaApply checks if a quota applies to the given namespace (simplified)
func (v *TicketCustomValidator) doesQuotaApply(quota corev1alpha1.Quota, namespace string) bool {
	switch quota.Spec.Scope.Type {
	case corev1alpha1.ClusterScope:
		return true
	case corev1alpha1.NamespaceListScope:
		for _, ns := range quota.Spec.Scope.Namespaces {
			if ns == namespace {
				return true
			}
		}
	}
	return false
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Ticket.
func (v *TicketCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	ticket, ok := obj.(*corev1alpha1.Ticket)
	if !ok {
		return nil, fmt.Errorf("expected a Ticket object but got %T", obj)
	}

	ticketlog.V(1).Info("Validating Ticket deletion", "name", ticket.Name)

	var warnings admission.Warnings

	// Check if ticket is claimed and warn about potential impact
	if ticket.Status.Phase == corev1alpha1.TicketClaimed {
		warnings = append(warnings, "Deleting a claimed ticket may affect running pods")
	}

	// Check if ticket has a guardian pod that will be deleted
	if ticket.Status.GuardianPod != nil {
		warnings = append(warnings, fmt.Sprintf("Guardian pod %s will be deleted", *ticket.Status.GuardianPod))
	}

	// Allow deletion but provide warnings
	return warnings, nil
}
