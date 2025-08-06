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
	"strings"
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
var quotalog = logf.Log.WithName("quota-resource")

// SetupQuotaWebhookWithManager registers the webhook for Quota in the manager.
func SetupQuotaWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&corev1alpha1.Quota{}).
		WithValidator(&QuotaCustomValidator{Client: mgr.GetClient()}).
		WithDefaulter(&QuotaCustomDefaulter{Client: mgr.GetClient()}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-core-korder-dev-v1alpha1-quota,mutating=true,failurePolicy=fail,sideEffects=None,groups=core.korder.dev,resources=quotas,verbs=create;update,versions=v1alpha1,name=mquota-v1alpha1.kb.io,admissionReviewVersions=v1

// QuotaCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Quota when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type QuotaCustomDefaulter struct {
	Client client.Client
}

var _ webhook.CustomDefaulter = &QuotaCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Quota.
func (d *QuotaCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	quota, ok := obj.(*corev1alpha1.Quota)
	if !ok {
		return fmt.Errorf("expected an Quota object but got %T", obj)
	}

	quotalog.Info("Setting defaults for Quota", "name", quota.GetName(), "namespace", quota.GetNamespace())

	// Set default scope if not specified
	if quota.Spec.Scope.Type == "" {
		quota.Spec.Scope.Type = corev1alpha1.ClusterScope
	}

	// Set default hard limits if none specified
	if quota.Spec.Hard == nil {
		quota.Spec.Hard = make(corev1.ResourceList)
	}

	// Set some reasonable defaults if no limits are specified
	if len(quota.Spec.Hard) == 0 {
		quota.Spec.Hard["orders"] = *resource.NewQuantity(100, resource.DecimalSI)
		quota.Spec.Hard["tickets"] = *resource.NewQuantity(1000, resource.DecimalSI)
		quota.Spec.Hard["reserved.cpu"] = resource.MustParse("1000")
		quota.Spec.Hard["reserved.memory"] = resource.MustParse("2000Gi")
	}

	// Set default duration limits if not specified
	if _, exists := quota.Spec.Hard["max-duration"]; !exists {
		quota.Spec.Hard["max-duration"] = *resource.NewQuantity(int64(time.Hour*24*7/time.Second), resource.DecimalSI) // 7 days in seconds
	}

	quotalog.V(1).Info("Applied defaults to Quota", "name", quota.Name, "scope", quota.Spec.Scope.Type)
	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-core-korder-dev-v1alpha1-quota,mutating=false,failurePolicy=fail,sideEffects=None,groups=core.korder.dev,resources=quotas,verbs=create;update,versions=v1alpha1,name=vquota-v1alpha1.kb.io,admissionReviewVersions=v1

// QuotaCustomValidator struct is responsible for validating the Quota resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type QuotaCustomValidator struct {
	Client client.Client
}

var _ webhook.CustomValidator = &QuotaCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Quota.
func (v *QuotaCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	quota, ok := obj.(*corev1alpha1.Quota)
	if !ok {
		return nil, fmt.Errorf("expected a Quota object but got %T", obj)
	}

	quotalog.Info("Validating Quota creation", "name", quota.GetName(), "namespace", quota.GetNamespace())

	// Validate quota specification
	warnings, err := v.validateQuotaSpec(quota)
	if err != nil {
		return warnings, fmt.Errorf("quota specification validation failed: %w", err)
	}

	// Validate scope configuration
	if err := v.validateQuotaScope(ctx, quota); err != nil {
		return warnings, fmt.Errorf("quota scope validation failed: %w", err)
	}

	// Check for conflicting quotas
	if err := v.validateNoConflictingQuotas(ctx, quota); err != nil {
		return warnings, fmt.Errorf("quota conflict validation failed: %w", err)
	}

	quotalog.V(1).Info("Quota validation passed", "name", quota.Name)
	return warnings, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Quota.
func (v *QuotaCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	quota, ok := newObj.(*corev1alpha1.Quota)
	if !ok {
		return nil, fmt.Errorf("expected a Quota object for the newObj but got %T", newObj)
	}

	oldQuota, ok := oldObj.(*corev1alpha1.Quota)
	if !ok {
		return nil, fmt.Errorf("expected a Quota object for the oldObj but got %T", oldObj)
	}

	quotalog.V(1).Info("Validating Quota update", "name", quota.Name)

	// Validate immutable fields
	warnings, err := v.validateQuotaImmutableFields(oldQuota, quota)
	if err != nil {
		return warnings, fmt.Errorf("immutable field validation failed: %w", err)
	}

	// Validate quota specification
	specWarnings, err := v.validateQuotaSpec(quota)
	if err != nil {
		return append(warnings, specWarnings...), fmt.Errorf("quota specification validation failed: %w", err)
	}

	return append(warnings, specWarnings...), nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Quota.
func (v *QuotaCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	quota, ok := obj.(*corev1alpha1.Quota)
	if !ok {
		return nil, fmt.Errorf("expected a Quota object but got %T", obj)
	}

	quotalog.V(1).Info("Validating Quota deletion", "name", quota.Name)

	var warnings admission.Warnings

	// Check if quota is currently being used
	if err := v.checkQuotaUsage(ctx, quota); err != nil {
		warnings = append(warnings, fmt.Sprintf("Quota is currently in use: %v", err))
	}

	// Allow deletion but provide warnings
	return warnings, nil
}

// validateQuotaSpec validates the quota specification
func (v *QuotaCustomValidator) validateQuotaSpec(quota *corev1alpha1.Quota) (admission.Warnings, error) {
	var warnings admission.Warnings

	// Validate resource limits are non-negative
	for resourceName, quantity := range quota.Spec.Hard {
		if quantity.Sign() < 0 {
			return warnings, fmt.Errorf("resource limit for %s cannot be negative", resourceName)
		}
	}

	// Validate known resource types
	knownResources := map[string]bool{
		"orders":          true,
		"tickets":         true,
		"reserved.cpu":    true,
		"reserved.memory": true,
		"max-duration":    true,
	}

	for resourceName := range quota.Spec.Hard {
		resName := string(resourceName)
		if !knownResources[resName] && !strings.HasPrefix(resName, "reserved.") {
			warnings = append(warnings, fmt.Sprintf("Unknown resource type: %s", resName))
		}
	}

	// Validate scope configuration
	if quota.Spec.Scope.Type == "" {
		return warnings, fmt.Errorf("scope type must be specified")
	}

	switch quota.Spec.Scope.Type {
	case corev1alpha1.NamespaceListScope:
		if len(quota.Spec.Scope.Namespaces) == 0 {
			return warnings, fmt.Errorf("namespace list scope requires at least one namespace")
		}
	case corev1alpha1.NamespaceSelectorScope:
		if quota.Spec.Scope.NamespaceSelector == nil {
			return warnings, fmt.Errorf("namespace selector scope requires namespace selector")
		}
	case corev1alpha1.ObjectSelectorScope:
		if quota.Spec.Scope.ObjectSelector == nil {
			return warnings, fmt.Errorf("object selector scope requires object selector")
		}
	case corev1alpha1.ClusterScope:
		// No additional validation needed
	default:
		return warnings, fmt.Errorf("unknown scope type: %s", quota.Spec.Scope.Type)
	}

	// Validate time windows
	for i, window := range quota.Spec.TimeWindows {
		if window.Name == "" {
			return warnings, fmt.Errorf("time window %d must have a name", i)
		}
		if window.Schedule == "" {
			return warnings, fmt.Errorf("time window %s must have a schedule", window.Name)
		}
		if window.Multiplier == "" {
			return warnings, fmt.Errorf("time window %s must have a multiplier", window.Name)
		}
		// TODO: Parse and validate multiplier string (e.g., "1.5", "2.0")
		// TODO: Validate cron expression format
	}

	// Validate allocation
	for i, allocation := range quota.Spec.Allocation {
		if allocation.Namespace == "" {
			return warnings, fmt.Errorf("allocation %d must specify namespace", i)
		}
		for resourceName, quantity := range allocation.Limits {
			if quantity.Sign() < 0 {
				return warnings, fmt.Errorf("allocation %d limit for %s cannot be negative", i, resourceName)
			}
			// Check if allocation exceeds total quota
			if totalLimit, exists := quota.Spec.Hard[resourceName]; exists {
				if quantity.Cmp(totalLimit) > 0 {
					warnings = append(warnings, fmt.Sprintf("Allocation for namespace %s exceeds total quota for %s", allocation.Namespace, resourceName))
				}
			}
		}
	}

	return warnings, nil
}

// validateQuotaScope validates the quota scope configuration
func (v *QuotaCustomValidator) validateQuotaScope(_ context.Context, quota *corev1alpha1.Quota) error {
	// For namespace list scope, verify namespace list is not empty
	if quota.Spec.Scope.Type == corev1alpha1.NamespaceListScope {
		if len(quota.Spec.Scope.Namespaces) == 0 {
			return fmt.Errorf("namespace list scope requires at least one namespace")
		}
		// Validate namespace names
		for _, ns := range quota.Spec.Scope.Namespaces {
			if ns == "" {
				return fmt.Errorf("namespace name cannot be empty")
			}
		}
		quotalog.V(1).Info("Namespace list scope validation passed", "namespaces", quota.Spec.Scope.Namespaces)
	}

	return nil
}

// validateNoConflictingQuotas checks for conflicting quotas
func (v *QuotaCustomValidator) validateNoConflictingQuotas(ctx context.Context, quota *corev1alpha1.Quota) error {
	// Get all existing quotas
	quotaList := &corev1alpha1.QuotaList{}
	if err := v.Client.List(ctx, quotaList); err != nil {
		return err
	}

	for _, existingQuota := range quotaList.Items {
		// Skip self
		if existingQuota.Name == quota.Name && existingQuota.Namespace == quota.Namespace {
			continue
		}

		// Check for scope conflicts
		if v.quotaScopesConflict(&existingQuota, quota) {
			return fmt.Errorf("quota scope conflicts with existing quota %s", existingQuota.Name)
		}
	}

	return nil
}

// quotaScopesConflict checks if two quotas have conflicting scopes
func (v *QuotaCustomValidator) quotaScopesConflict(quota1, quota2 *corev1alpha1.Quota) bool {
	// Simplified conflict detection - two cluster scopes conflict
	if quota1.Spec.Scope.Type == corev1alpha1.ClusterScope && quota2.Spec.Scope.Type == corev1alpha1.ClusterScope {
		return true
	}

	// More sophisticated conflict detection could be added here
	// For example, overlapping namespace lists or selectors

	return false
}

// validateQuotaImmutableFields validates that immutable fields haven't changed
func (v *QuotaCustomValidator) validateQuotaImmutableFields(oldQuota, newQuota *corev1alpha1.Quota) (admission.Warnings, error) {
	var warnings admission.Warnings

	// Scope type should be immutable
	if oldQuota.Spec.Scope.Type != newQuota.Spec.Scope.Type {
		return warnings, fmt.Errorf("scope type is immutable")
	}

	// Namespace list should be immutable (can warn about scope changes)
	if oldQuota.Spec.Scope.Type == corev1alpha1.NamespaceListScope {
		if !v.stringSlicesEqual(oldQuota.Spec.Scope.Namespaces, newQuota.Spec.Scope.Namespaces) {
			warnings = append(warnings, "Changing namespace list may affect existing resources")
		}
	}

	return warnings, nil
}

// stringSlicesEqual compares two string slices for equality
func (v *QuotaCustomValidator) stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

// checkQuotaUsage checks if the quota is currently being used
func (v *QuotaCustomValidator) checkQuotaUsage(ctx context.Context, _ *corev1alpha1.Quota) error {
	// This is a simplified check - in practice, you'd want to check actual usage
	// For now, just check if there are any orders or tickets that might be affected

	// Check orders
	orderList := &corev1alpha1.OrderList{}
	if err := v.Client.List(ctx, orderList); err != nil {
		return err
	}

	if len(orderList.Items) > 0 {
		return fmt.Errorf("quota may be in use by %d orders", len(orderList.Items))
	}

	// Check tickets
	ticketList := &corev1alpha1.TicketList{}
	if err := v.Client.List(ctx, ticketList); err != nil {
		return err
	}

	if len(ticketList.Items) > 0 {
		return fmt.Errorf("quota may be in use by %d tickets", len(ticketList.Items))
	}

	return nil
}
