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
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	corev1alpha1 "github.com/monshunter/korder/api/v1alpha1"
)

var daemonorderlog = logf.Log.WithName("daemonorder-resource")

const (
	defaultWindowDurationDaemon = "24h"
	defaultSchedulerNameDaemon  = "default-scheduler"
)

// SetupDaemonOrderWebhookWithManager registers the webhook for DaemonOrder in the manager.
func SetupDaemonOrderWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&corev1alpha1.DaemonOrder{}).
		WithValidator(&DaemonOrderCustomValidator{Client: mgr.GetClient()}).
		WithDefaulter(&DaemonOrderCustomDefaulter{Client: mgr.GetClient()}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-core-korder-dev-v1alpha1-daemonorder,mutating=true,failurePolicy=fail,sideEffects=None,groups=core.korder.dev,resources=daemonorders,verbs=create;update,versions=v1alpha1,name=mdaemonorder-v1alpha1.kb.io,admissionReviewVersions=v1

// DaemonOrderCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind DaemonOrder when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type DaemonOrderCustomDefaulter struct {
	Client client.Client
}

var _ webhook.CustomDefaulter = &DaemonOrderCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind DaemonOrder.
func (d *DaemonOrderCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	daemonOrder, ok := obj.(*corev1alpha1.DaemonOrder)
	if !ok {
		return fmt.Errorf("expected a DaemonOrder object but got %T", obj)
	}

	daemonorderlog.Info("Setting defaults for DaemonOrder", "name", daemonOrder.GetName(), "namespace", daemonOrder.GetNamespace())

	// Set default paused state
	if daemonOrder.Spec.Paused == nil {
		defaultPaused := false
		daemonOrder.Spec.Paused = &defaultPaused
	}

	// Set default refresh policy
	if daemonOrder.Spec.RefreshPolicy == "" {
		daemonOrder.Spec.RefreshPolicy = corev1alpha1.OnClaimRefresh
	}

	// Set default ticket template values
	d.setTicketTemplateDefaults(&daemonOrder.Spec.Template)

	// Set default labels for tickets
	if daemonOrder.Spec.Template.Labels == nil {
		daemonOrder.Spec.Template.Labels = make(map[string]string)
	}
	daemonOrder.Spec.Template.Labels["korder.dev/daemonorder"] = daemonOrder.Name
	daemonOrder.Spec.Template.Labels["korder.dev/daemonorder-uid"] = string(daemonOrder.UID)

	daemonorderlog.V(1).Info("Applied defaults to DaemonOrder", "name", daemonOrder.Name, "paused", *daemonOrder.Spec.Paused)
	return nil
}

// setTicketTemplateDefaults sets default values for ticket template
func (d *DaemonOrderCustomDefaulter) setTicketTemplateDefaults(template *corev1alpha1.TicketTemplate) {
	// Set default window duration if not specified
	if template.Spec.Window == nil {
		template.Spec.Window = &corev1alpha1.WindowSpec{
			Duration: defaultWindowDurationDaemon, // 24 hours default
		}
	} else if template.Spec.Window.Duration == "" {
		template.Spec.Window.Duration = defaultWindowDurationDaemon
	}

	// Set default lifecycle policy
	if template.Spec.Lifecycle == nil {
		template.Spec.Lifecycle = &corev1alpha1.TicketLifecycle{
			CleanupPolicy: corev1alpha1.DeleteCleanup,
		}
	}

	// Set default TTL if cleanup policy is Delete but no TTL specified
	if template.Spec.Lifecycle.CleanupPolicy == corev1alpha1.DeleteCleanup && template.Spec.Lifecycle.TTLSecondsAfterFinished == nil {
		defaultTTL := int32(3600) // 1 hour
		template.Spec.Lifecycle.TTLSecondsAfterFinished = &defaultTTL
	}

	// Set default scheduler name
	if template.Spec.SchedulerName == nil {
		schedulerName := defaultSchedulerNameDaemon
		template.Spec.SchedulerName = &schedulerName
	}
}

// +kubebuilder:webhook:path=/validate-core-korder-dev-v1alpha1-daemonorder,mutating=false,failurePolicy=fail,sideEffects=None,groups=core.korder.dev,resources=daemonorders,verbs=create;update,versions=v1alpha1,name=vdaemonorder-v1alpha1.kb.io,admissionReviewVersions=v1

// DaemonOrderCustomValidator struct is responsible for validating the DaemonOrder resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type DaemonOrderCustomValidator struct {
	Client client.Client
}

var _ webhook.CustomValidator = &DaemonOrderCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type DaemonOrder.
func (v *DaemonOrderCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	daemonOrder, ok := obj.(*corev1alpha1.DaemonOrder)
	if !ok {
		return nil, fmt.Errorf("expected a DaemonOrder object but got %T", obj)
	}

	daemonorderlog.Info("Validating DaemonOrder creation", "name", daemonOrder.GetName(), "namespace", daemonOrder.GetNamespace())

	// Validate daemon order specification
	if warnings, err := v.validateDaemonOrderSpec(daemonOrder); err != nil {
		return warnings, fmt.Errorf("daemonorder specification validation failed: %w", err)
	}

	daemonorderlog.V(1).Info("DaemonOrder validation passed", "name", daemonOrder.Name)
	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type DaemonOrder.
func (v *DaemonOrderCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	daemonOrder, ok := newObj.(*corev1alpha1.DaemonOrder)
	if !ok {
		return nil, fmt.Errorf("expected a DaemonOrder object for the newObj but got %T", newObj)
	}

	oldDaemonOrder, ok := oldObj.(*corev1alpha1.DaemonOrder)
	if !ok {
		return nil, fmt.Errorf("expected a DaemonOrder object for the oldObj but got %T", oldObj)
	}

	daemonorderlog.V(1).Info("Validating DaemonOrder update", "name", daemonOrder.Name)

	// Validate daemon order specification
	if warnings, err := v.validateDaemonOrderSpec(daemonOrder); err != nil {
		return warnings, fmt.Errorf("daemonorder specification validation failed: %w", err)
	}

	// Validate immutable fields
	if warnings, err := v.validateImmutableFields(oldDaemonOrder, daemonOrder); err != nil {
		return warnings, fmt.Errorf("immutable field validation failed: %w", err)
	}

	return nil, nil
}

// validateDaemonOrderSpec validates the daemon order specification
func (v *DaemonOrderCustomValidator) validateDaemonOrderSpec(daemonOrder *corev1alpha1.DaemonOrder) (admission.Warnings, error) {
	var warnings admission.Warnings

	// Validate ticket template
	if err := v.validateTicketTemplate(&daemonOrder.Spec.Template); err != nil {
		return warnings, fmt.Errorf("ticket template validation failed: %w", err)
	}

	// Validate selectors if specified
	if daemonOrder.Spec.Selector != nil && len(daemonOrder.Spec.Selector.MatchLabels) == 0 && len(daemonOrder.Spec.Selector.MatchExpressions) == 0 {
		return warnings, fmt.Errorf("selector cannot be empty when specified")
	}

	return warnings, nil
}

// validateTicketTemplate validates the ticket template
func (v *DaemonOrderCustomValidator) validateTicketTemplate(template *corev1alpha1.TicketTemplate) error {
	// Validate window
	if template.Spec.Window != nil {
		// Validate duration format
		if template.Spec.Window.Duration != "" {
			if _, err := time.ParseDuration(template.Spec.Window.Duration); err != nil {
				return fmt.Errorf("invalid window duration format: %w", err)
			}
		}

		// Validate start time format if specified
		if template.Spec.Window.StartTime != nil && *template.Spec.Window.StartTime != "" {
			if _, err := time.Parse(time.RFC3339, *template.Spec.Window.StartTime); err != nil {
				return fmt.Errorf("invalid start time format, must be RFC3339: %w", err)
			}
		}
	}

	// Validate lifecycle policy
	if template.Spec.Lifecycle != nil {
		if template.Spec.Lifecycle.TTLSecondsAfterFinished != nil && *template.Spec.Lifecycle.TTLSecondsAfterFinished < 0 {
			return fmt.Errorf("TTL seconds cannot be negative")
		}
	}

	// Validate resources
	if template.Spec.Resources != nil {
		if err := v.validateResources(template.Spec.Resources); err != nil {
			return fmt.Errorf("resource validation failed: %w", err)
		}
	}

	// Validate node affinity constraints for DaemonOrder
	if template.Spec.NodeSelector != nil && len(template.Spec.NodeSelector) == 0 {
		return fmt.Errorf("node selector cannot be empty when specified")
	}

	return nil
}

// validateResources validates resource requirements
func (v *DaemonOrderCustomValidator) validateResources(resources *corev1.ResourceRequirements) error {
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

	return nil
}

// validateImmutableFields validates that immutable fields haven't changed
func (v *DaemonOrderCustomValidator) validateImmutableFields(oldDaemonOrder, newDaemonOrder *corev1alpha1.DaemonOrder) (admission.Warnings, error) {
	var warnings admission.Warnings

	// Template selector fields should be immutable for daemon orders
	if oldDaemonOrder.Spec.Selector != nil && newDaemonOrder.Spec.Selector != nil {
		// This is a simplified check - in practice, you might want more sophisticated validation
		if len(oldDaemonOrder.Spec.Selector.MatchLabels) != len(newDaemonOrder.Spec.Selector.MatchLabels) {
			warnings = append(warnings, "Changing selector may affect existing tickets on all nodes")
		}
	}

	// RefreshPolicy changes should be warned about
	if oldDaemonOrder.Spec.RefreshPolicy != newDaemonOrder.Spec.RefreshPolicy {
		warnings = append(warnings, "Changing refresh policy may affect existing ticket lifecycle on all nodes")
	}

	// Template changes that affect scheduling
	oldNodeSelector := oldDaemonOrder.Spec.Template.Spec.NodeSelector
	newNodeSelector := newDaemonOrder.Spec.Template.Spec.NodeSelector

	if len(oldNodeSelector) != len(newNodeSelector) {
		warnings = append(warnings, "Changing node selector may affect ticket placement across nodes")
	} else if len(oldNodeSelector) > 0 && len(newNodeSelector) > 0 {
		for key, oldValue := range oldNodeSelector {
			if newValue, exists := newNodeSelector[key]; !exists || oldValue != newValue {
				warnings = append(warnings, "Changing node selector may affect ticket placement across nodes")
				break
			}
		}
	}

	return warnings, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type DaemonOrder.
func (v *DaemonOrderCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	daemonOrder, ok := obj.(*corev1alpha1.DaemonOrder)
	if !ok {
		return nil, fmt.Errorf("expected a DaemonOrder object but got %T", obj)
	}

	daemonorderlog.V(1).Info("Validating DaemonOrder deletion", "name", daemonOrder.Name)

	// Check if daemonorder has active tickets
	warnings, err := v.validateDaemonOrderDeletion(ctx, daemonOrder)
	if err != nil {
		return warnings, err
	}

	return warnings, nil
}

// validateDaemonOrderDeletion validates that it's safe to delete the daemon order
func (v *DaemonOrderCustomValidator) validateDaemonOrderDeletion(ctx context.Context, daemonOrder *corev1alpha1.DaemonOrder) (admission.Warnings, error) {
	var warnings admission.Warnings

	// Get tickets owned by this daemon order
	ticketList := &corev1alpha1.TicketList{}
	listOpts := []client.ListOption{
		client.InNamespace(daemonOrder.Namespace),
		client.MatchingLabels{"korder.dev/daemonorder": daemonOrder.Name},
	}

	if err := v.Client.List(ctx, ticketList, listOpts...); err != nil {
		return warnings, fmt.Errorf("failed to list tickets: %w", err)
	}

	// Check for active tickets
	activeTickets := 0
	claimedTickets := 0
	readyTickets := 0
	for _, ticket := range ticketList.Items {
		switch ticket.Status.Phase {
		case corev1alpha1.TicketReady:
			readyTickets++
		case corev1alpha1.TicketClaimed:
			claimedTickets++
		}
		// Count non-terminal tickets as active
		if ticket.Status.Phase != corev1alpha1.TicketExpired {
			activeTickets++
		}
	}

	if activeTickets > 0 {
		warnings = append(warnings, fmt.Sprintf("DaemonOrder has %d active tickets across nodes that will be deleted", activeTickets))
	}

	if readyTickets > 0 {
		warnings = append(warnings, fmt.Sprintf("DaemonOrder has %d ready (reserved) tickets that will be deleted", readyTickets))
	}

	if claimedTickets > 0 {
		warnings = append(warnings, fmt.Sprintf("DaemonOrder has %d claimed tickets that will be deleted", claimedTickets))
	}

	// Allow deletion but warn about consequences
	return warnings, nil
}
