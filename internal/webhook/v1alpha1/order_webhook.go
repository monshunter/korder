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
var orderlog = logf.Log.WithName("order-resource")

// SetupOrderWebhookWithManager registers the webhook for Order in the manager.
func SetupOrderWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&corev1alpha1.Order{}).
		WithValidator(&OrderCustomValidator{Client: mgr.GetClient()}).
		WithDefaulter(&OrderCustomDefaulter{Client: mgr.GetClient()}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-core-korder-dev-v1alpha1-order,mutating=true,failurePolicy=fail,sideEffects=None,groups=core.korder.dev,resources=orders,verbs=create;update,versions=v1alpha1,name=morder-v1alpha1.kb.io,admissionReviewVersions=v1

// OrderCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Order when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type OrderCustomDefaulter struct {
	Client client.Client
}

var _ webhook.CustomDefaulter = &OrderCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Order.
func (d *OrderCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	order, ok := obj.(*corev1alpha1.Order)
	if !ok {
		return fmt.Errorf("expected an Order object but got %T", obj)
	}

	orderlog.Info("Setting defaults for Order", "name", order.GetName(), "namespace", order.GetNamespace())

	// Set default replicas
	if order.Spec.Replicas == nil {
		defaultReplicas := int32(1)
		order.Spec.Replicas = &defaultReplicas
	}

	// Set default paused state
	if order.Spec.Paused == nil {
		defaultPaused := false
		order.Spec.Paused = &defaultPaused
	}

	// Set default refresh policy
	if order.Spec.RefreshPolicy == "" {
		order.Spec.RefreshPolicy = corev1alpha1.OnClaimRefresh
	}

	// Set default minReadySeconds
	if order.Spec.MinReadySeconds == nil {
		defaultMinReadySeconds := int32(0)
		order.Spec.MinReadySeconds = &defaultMinReadySeconds
	}

	// Set default ticket template values
	d.setTicketTemplateDefaults(&order.Spec.Template)

	// Set default labels for tickets
	if order.Spec.Template.Labels == nil {
		order.Spec.Template.Labels = make(map[string]string)
	}
	order.Spec.Template.Labels["korder.dev/order"] = order.Name
	order.Spec.Template.Labels["korder.dev/order-uid"] = string(order.UID)

	orderlog.V(1).Info("Applied defaults to Order", "name", order.Name, "replicas", *order.Spec.Replicas)
	return nil
}

// setTicketTemplateDefaults sets default values for ticket template
func (d *OrderCustomDefaulter) setTicketTemplateDefaults(template *corev1alpha1.TicketTemplate) {
	// Set default window duration if not specified
	if template.Spec.Window == nil {
		template.Spec.Window = &corev1alpha1.WindowSpec{
			Duration: "24h", // 24 hours default
		}
	} else if template.Spec.Window.Duration == "" {
		template.Spec.Window.Duration = "24h"
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
		defaultScheduler := "default-scheduler"
		template.Spec.SchedulerName = &defaultScheduler
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-core-korder-dev-v1alpha1-order,mutating=false,failurePolicy=fail,sideEffects=None,groups=core.korder.dev,resources=orders,verbs=create;update,versions=v1alpha1,name=vorder-v1alpha1.kb.io,admissionReviewVersions=v1

// OrderCustomValidator struct is responsible for validating the Order resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type OrderCustomValidator struct {
	Client client.Client
}

var _ webhook.CustomValidator = &OrderCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Order.
func (v *OrderCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	order, ok := obj.(*corev1alpha1.Order)
	if !ok {
		return nil, fmt.Errorf("expected a Order object but got %T", obj)
	}

	orderlog.Info("Validating Order creation", "name", order.GetName(), "namespace", order.GetNamespace())

	// Validate quota compliance
	if err := v.validateQuotaCompliance(ctx, order); err != nil {
		return nil, fmt.Errorf("quota validation failed: %w", err)
	}

	// Validate order specification
	if warnings, err := v.validateOrderSpec(order); err != nil {
		return warnings, fmt.Errorf("order specification validation failed: %w", err)
	}

	orderlog.V(1).Info("Order validation passed", "name", order.Name)
	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Order.
func (v *OrderCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	order, ok := newObj.(*corev1alpha1.Order)
	if !ok {
		return nil, fmt.Errorf("expected a Order object for the newObj but got %T", newObj)
	}

	oldOrder, ok := oldObj.(*corev1alpha1.Order)
	if !ok {
		return nil, fmt.Errorf("expected a Order object for the oldObj but got %T", oldObj)
	}

	orderlog.V(1).Info("Validating Order update", "name", order.Name)

	// Validate immutable fields
	if warnings, err := v.validateImmutableFields(oldOrder, order); err != nil {
		return warnings, fmt.Errorf("immutable field validation failed: %w", err)
	}

	// Validate quota compliance for updates
	if err := v.validateQuotaComplianceUpdate(ctx, oldOrder, order); err != nil {
		return nil, fmt.Errorf("quota validation failed: %w", err)
	}

	return nil, nil
}

// validateOrderSpec validates the order specification
func (v *OrderCustomValidator) validateOrderSpec(order *corev1alpha1.Order) (admission.Warnings, error) {
	var warnings admission.Warnings

	// Validate replicas
	if order.Spec.Replicas != nil && *order.Spec.Replicas < 0 {
		return warnings, fmt.Errorf("replicas cannot be negative")
	}

	// Add warning for high replica counts
	if order.Spec.Replicas != nil && *order.Spec.Replicas > 100 {
		warnings = append(warnings, "High replica count may impact cluster performance")
	}

	// Validate minReadySeconds
	if order.Spec.MinReadySeconds != nil && *order.Spec.MinReadySeconds < 0 {
		return warnings, fmt.Errorf("minReadySeconds cannot be negative")
	}

	// Validate ticket template
	if err := v.validateTicketTemplate(&order.Spec.Template); err != nil {
		return warnings, fmt.Errorf("ticket template validation failed: %w", err)
	}

	// Validate selectors if specified
	if order.Spec.Selector != nil && len(order.Spec.Selector.MatchLabels) == 0 && len(order.Spec.Selector.MatchExpressions) == 0 {
		return warnings, fmt.Errorf("selector cannot be empty when specified")
	}

	return warnings, nil
}

// Removed strategy validation - using simple declarative approach

// validateTicketTemplate validates the ticket template
func (v *OrderCustomValidator) validateTicketTemplate(template *corev1alpha1.TicketTemplate) error {
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

	return nil
}

// validateResources validates resource requirements
func (v *OrderCustomValidator) validateResources(resources *corev1.ResourceRequirements) error {
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
func (v *OrderCustomValidator) validateImmutableFields(oldOrder, newOrder *corev1alpha1.Order) (admission.Warnings, error) {
	var warnings admission.Warnings

	// Template selector fields should be immutable
	if oldOrder.Spec.Selector != nil && newOrder.Spec.Selector != nil {
		// This is a simplified check - in practice, you might want more sophisticated validation
		if len(oldOrder.Spec.Selector.MatchLabels) != len(newOrder.Spec.Selector.MatchLabels) {
			warnings = append(warnings, "Changing selector may affect existing tickets")
		}
	}

	// RefreshPolicy changes should be warned about
	if oldOrder.Spec.RefreshPolicy != newOrder.Spec.RefreshPolicy {
		warnings = append(warnings, "Changing refresh policy may affect existing ticket lifecycle")
	}

	return warnings, nil
}

// validateQuotaCompliance validates that the order complies with quotas
func (v *OrderCustomValidator) validateQuotaCompliance(ctx context.Context, order *corev1alpha1.Order) error {
	// Calculate resource requirements for this order
	resourceRequests := v.calculateOrderResourceRequests(order)

	// Validate against quotas
	return v.validateAgainstQuotas(ctx, order.Namespace, resourceRequests)
}

// validateQuotaComplianceUpdate validates quota compliance for updates
func (v *OrderCustomValidator) validateQuotaComplianceUpdate(ctx context.Context, oldOrder, newOrder *corev1alpha1.Order) error {
	// Calculate the delta in resource requirements
	oldRequests := v.calculateOrderResourceRequests(oldOrder)
	newRequests := v.calculateOrderResourceRequests(newOrder)

	// Calculate the difference
	deltaRequests := make(corev1.ResourceList)
	for resourceName, newQuantity := range newRequests {
		if oldQuantity, exists := oldRequests[resourceName]; exists {
			delta := newQuantity.DeepCopy()
			delta.Sub(oldQuantity)
			if !delta.IsZero() {
				deltaRequests[resourceName] = delta
			}
		} else {
			deltaRequests[resourceName] = newQuantity
		}
	}

	// Only validate if there's an increase in resources
	hasIncrease := false
	for _, quantity := range deltaRequests {
		if quantity.Sign() > 0 {
			hasIncrease = true
			break
		}
	}

	if hasIncrease {
		return v.validateAgainstQuotas(ctx, newOrder.Namespace, deltaRequests)
	}

	return nil
}

// validateAgainstQuotas validates resource requests against applicable quotas
func (v *OrderCustomValidator) validateAgainstQuotas(ctx context.Context, namespace string, resourceRequests corev1.ResourceList) error {
	// Get all quotas that might apply
	quotaList := &corev1alpha1.QuotaList{}
	if err := v.Client.List(ctx, quotaList); err != nil {
		return err
	}

	for _, quota := range quotaList.Items {
		if v.doesQuotaApply(ctx, &quota, namespace) {
			// Calculate current usage
			usage, err := v.calculateQuotaUsage(ctx, &quota)
			if err != nil {
				return err
			}

			// Calculate what usage would be with the new request
			projectedUsage := usage.DeepCopy()
			for resourceName, quantity := range resourceRequests {
				if existing, exists := projectedUsage[resourceName]; exists {
					existing.Add(quantity)
					projectedUsage[resourceName] = existing
				} else {
					projectedUsage[resourceName] = quantity
				}
			}

			// Check if projected usage would exceed limits
			limits := quota.Spec.Hard
			if v.isQuotaExceeded(projectedUsage, limits) {
				return fmt.Errorf("quota %s would be exceeded: projected usage %v exceeds limits %v", quota.Name, projectedUsage, limits)
			}
		}
	}

	return nil
}

// doesQuotaApply checks if a quota applies to the given namespace (simplified version)
func (v *OrderCustomValidator) doesQuotaApply(_ context.Context, quota *corev1alpha1.Quota, namespace string) bool {
	switch quota.Spec.Scope.Type {
	case corev1alpha1.ClusterScope:
		return true
	case corev1alpha1.NamespaceListScope:
		for _, ns := range quota.Spec.Scope.Namespaces {
			if ns == namespace {
				return true
			}
		}
	case corev1alpha1.NamespaceSelectorScope:
		// This would require more complex logic to check namespace labels
		// For now, return false as a safe default
		return false
	case corev1alpha1.ObjectSelectorScope:
		// This would require checking object labels
		// For now, return false as a safe default
		return false
	}
	return false
}

// calculateQuotaUsage calculates current resource usage for a quota (simplified version)
func (v *OrderCustomValidator) calculateQuotaUsage(ctx context.Context, quota *corev1alpha1.Quota) (corev1.ResourceList, error) {
	usage := make(corev1.ResourceList)

	// Get orders and tickets based on quota scope (simplified)
	orders, tickets, err := v.getResourcesInScope(ctx, quota)
	if err != nil {
		return nil, err
	}

	// Count orders and tickets
	if len(orders) > 0 {
		usage[corev1.ResourceName("orders")] = *resource.NewQuantity(int64(len(orders)), resource.DecimalSI)
	}
	if len(tickets) > 0 {
		usage[corev1.ResourceName("tickets")] = *resource.NewQuantity(int64(len(tickets)), resource.DecimalSI)
	}

	return usage, nil
}

// getResourcesInScope returns orders and tickets in quota scope (simplified version)
func (v *OrderCustomValidator) getResourcesInScope(ctx context.Context, quota *corev1alpha1.Quota) ([]corev1alpha1.Order, []corev1alpha1.Ticket, error) {
	var orders []corev1alpha1.Order
	var tickets []corev1alpha1.Ticket

	// Simplified: only handle cluster scope and namespace list scope
	switch quota.Spec.Scope.Type {
	case corev1alpha1.ClusterScope:
		orderList := &corev1alpha1.OrderList{}
		if err := v.Client.List(ctx, orderList); err != nil {
			return nil, nil, err
		}
		orders = orderList.Items

		ticketList := &corev1alpha1.TicketList{}
		if err := v.Client.List(ctx, ticketList); err != nil {
			return nil, nil, err
		}
		tickets = ticketList.Items

	case corev1alpha1.NamespaceListScope:
		for _, ns := range quota.Spec.Scope.Namespaces {
			orderList := &corev1alpha1.OrderList{}
			if err := v.Client.List(ctx, orderList, client.InNamespace(ns)); err != nil {
				return nil, nil, err
			}
			orders = append(orders, orderList.Items...)

			ticketList := &corev1alpha1.TicketList{}
			if err := v.Client.List(ctx, ticketList, client.InNamespace(ns)); err != nil {
				return nil, nil, err
			}
			tickets = append(tickets, ticketList.Items...)
		}
	}

	return orders, tickets, nil
}

// isQuotaExceeded checks if current usage exceeds the limits
func (v *OrderCustomValidator) isQuotaExceeded(usage, limits corev1.ResourceList) bool {
	for resourceName, limit := range limits {
		if used, exists := usage[resourceName]; exists {
			if used.Cmp(limit) > 0 {
				return true
			}
		}
	}
	return false
}

// calculateOrderResourceRequests calculates the total resource requests for an order
func (v *OrderCustomValidator) calculateOrderResourceRequests(order *corev1alpha1.Order) corev1.ResourceList {
	requests := make(corev1.ResourceList)

	// Add order count
	requests[corev1.ResourceName("orders")] = *resource.NewQuantity(1, resource.DecimalSI)

	// Calculate ticket resources
	replicas := int64(1)
	if order.Spec.Replicas != nil {
		replicas = int64(*order.Spec.Replicas)
	}

	// Add ticket count
	requests[corev1.ResourceName("tickets")] = *resource.NewQuantity(replicas, resource.DecimalSI)

	// Add reserved resources if specified in template
	if order.Spec.Template.Spec.Resources != nil && order.Spec.Template.Spec.Resources.Requests != nil {
		for resourceName, quantity := range order.Spec.Template.Spec.Resources.Requests {
			total := quantity.DeepCopy()
			total.Set(total.Value() * replicas)

			reservedName := corev1.ResourceName("reserved." + string(resourceName))
			requests[reservedName] = total
		}
	}

	return requests
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Order.
func (v *OrderCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	order, ok := obj.(*corev1alpha1.Order)
	if !ok {
		return nil, fmt.Errorf("expected a Order object but got %T", obj)
	}

	orderlog.V(1).Info("Validating Order deletion", "name", order.Name)

	// Check if order has active tickets
	warnings, err := v.validateOrderDeletion(ctx, order)
	if err != nil {
		return warnings, err
	}

	return warnings, nil
}

// validateOrderDeletion validates that it's safe to delete the order
func (v *OrderCustomValidator) validateOrderDeletion(ctx context.Context, order *corev1alpha1.Order) (admission.Warnings, error) {
	var warnings admission.Warnings

	// Get tickets owned by this order
	ticketList := &corev1alpha1.TicketList{}
	listOpts := []client.ListOption{
		client.InNamespace(order.Namespace),
		client.MatchingLabels{"korder.dev/order": order.Name},
	}

	if err := v.Client.List(ctx, ticketList, listOpts...); err != nil {
		return warnings, fmt.Errorf("failed to list tickets: %w", err)
	}

	// Check for active tickets
	activeTickets := 0
	claimedTickets := 0
	for _, ticket := range ticketList.Items {
		switch ticket.Status.Phase {
		case corev1alpha1.TicketReady:
			activeTickets++
		case corev1alpha1.TicketClaimed:
			claimedTickets++
		}
	}

	if activeTickets > 0 {
		warnings = append(warnings, fmt.Sprintf("Order has %d active (reserved) tickets that will be deleted", activeTickets))
	}

	if claimedTickets > 0 {
		warnings = append(warnings, fmt.Sprintf("Order has %d claimed tickets that will be deleted", claimedTickets))
	}

	// Allow deletion but warn about consequences
	return warnings, nil
}
