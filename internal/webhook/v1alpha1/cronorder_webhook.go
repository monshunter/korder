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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	corev1alpha1 "github.com/monshunter/korder/api/v1alpha1"
	"github.com/monshunter/korder/internal/scheduler"
)

var cronorderlog = logf.Log.WithName("cronorder-resource")

const (
	defaultWindowDuration = "24h"
	defaultSchedulerName  = "default-scheduler"
)

// SetupCronOrderWebhookWithManager registers the webhook for CronOrder in the manager.
func SetupCronOrderWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&corev1alpha1.CronOrder{}).
		WithValidator(&CronOrderCustomValidator{Client: mgr.GetClient()}).
		WithDefaulter(&CronOrderCustomDefaulter{Client: mgr.GetClient()}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-core-korder-dev-v1alpha1-cronorder,mutating=true,failurePolicy=fail,sideEffects=None,groups=core.korder.dev,resources=cronorders,verbs=create;update,versions=v1alpha1,name=mcronorder-v1alpha1.kb.io,admissionReviewVersions=v1

// CronOrderCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind CronOrder when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type CronOrderCustomDefaulter struct {
	Client client.Client
}

var _ webhook.CustomDefaulter = &CronOrderCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind CronOrder.
func (d *CronOrderCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	cronOrder, ok := obj.(*corev1alpha1.CronOrder)
	if !ok {
		return fmt.Errorf("expected a CronOrder object but got %T", obj)
	}

	cronorderlog.Info("Setting defaults for CronOrder", "name", cronOrder.GetName(), "namespace", cronOrder.GetNamespace())

	// Set default suspend state
	if cronOrder.Spec.Suspend == nil {
		defaultSuspend := false
		cronOrder.Spec.Suspend = &defaultSuspend
	}

	// Set default concurrency policy
	if cronOrder.Spec.ConcurrencyPolicy == "" {
		cronOrder.Spec.ConcurrencyPolicy = corev1alpha1.AllowConcurrent
	}

	// Set default history limits
	if cronOrder.Spec.SuccessfulJobsHistoryLimit == nil {
		defaultSuccessfulLimit := int32(3)
		cronOrder.Spec.SuccessfulJobsHistoryLimit = &defaultSuccessfulLimit
	}

	if cronOrder.Spec.FailedJobsHistoryLimit == nil {
		defaultFailedLimit := int32(1)
		cronOrder.Spec.FailedJobsHistoryLimit = &defaultFailedLimit
	}

	// Set default order template values
	d.setOrderTemplateDefaults(&cronOrder.Spec.OrderTemplate)

	// Set default labels for order template
	if cronOrder.Spec.OrderTemplate.Labels == nil {
		cronOrder.Spec.OrderTemplate.Labels = make(map[string]string)
	}
	cronOrder.Spec.OrderTemplate.Labels["korder.dev/cronorder"] = cronOrder.Name
	cronOrder.Spec.OrderTemplate.Labels["korder.dev/cronorder-uid"] = string(cronOrder.UID)

	cronorderlog.V(1).Info("Applied defaults to CronOrder", "name", cronOrder.Name, "suspend", *cronOrder.Spec.Suspend)
	return nil
}

// setOrderTemplateDefaults sets default values for order template
func (d *CronOrderCustomDefaulter) setOrderTemplateDefaults(template *corev1alpha1.OrderTemplate) {
	// Set default replicas in order template
	if template.Spec.Replicas == nil {
		defaultReplicas := int32(1)
		template.Spec.Replicas = &defaultReplicas
	}

	// Set default paused state in order template
	if template.Spec.Paused == nil {
		defaultPaused := false
		template.Spec.Paused = &defaultPaused
	}

	// Set default refresh policy in order template
	if template.Spec.RefreshPolicy == "" {
		template.Spec.RefreshPolicy = corev1alpha1.OnClaimRefresh
	}

	// Set default minReadySeconds in order template
	if template.Spec.MinReadySeconds == nil {
		defaultMinReadySeconds := int32(0)
		template.Spec.MinReadySeconds = &defaultMinReadySeconds
	}

	// Set default ticket template values
	d.setTicketTemplateDefaults(&template.Spec.Template)
}

// setTicketTemplateDefaults sets default values for ticket template
func (d *CronOrderCustomDefaulter) setTicketTemplateDefaults(template *corev1alpha1.TicketTemplate) {
	// Set default window duration if not specified
	if template.Spec.Window == nil {
		template.Spec.Window = &corev1alpha1.WindowSpec{
			Duration: defaultWindowDuration, // 24 hours default
		}
	} else if template.Spec.Window.Duration == "" {
		template.Spec.Window.Duration = defaultWindowDuration
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
		schedulerName := defaultSchedulerName
		template.Spec.SchedulerName = &schedulerName
	}
}

// +kubebuilder:webhook:path=/validate-core-korder-dev-v1alpha1-cronorder,mutating=false,failurePolicy=fail,sideEffects=None,groups=core.korder.dev,resources=cronorders,verbs=create;update,versions=v1alpha1,name=vcronorder-v1alpha1.kb.io,admissionReviewVersions=v1

// CronOrderCustomValidator struct is responsible for validating the CronOrder resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type CronOrderCustomValidator struct {
	Client client.Client
}

var _ webhook.CustomValidator = &CronOrderCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type CronOrder.
func (v *CronOrderCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	cronOrder, ok := obj.(*corev1alpha1.CronOrder)
	if !ok {
		return nil, fmt.Errorf("expected a CronOrder object but got %T", obj)
	}

	cronorderlog.Info("Validating CronOrder creation", "name", cronOrder.GetName(), "namespace", cronOrder.GetNamespace())

	// Validate cron order specification
	if warnings, err := v.validateCronOrderSpec(cronOrder); err != nil {
		return warnings, fmt.Errorf("cronorder specification validation failed: %w", err)
	}

	cronorderlog.V(1).Info("CronOrder validation passed", "name", cronOrder.Name)
	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type CronOrder.
func (v *CronOrderCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	cronOrder, ok := newObj.(*corev1alpha1.CronOrder)
	if !ok {
		return nil, fmt.Errorf("expected a CronOrder object for the newObj but got %T", newObj)
	}

	oldCronOrder, ok := oldObj.(*corev1alpha1.CronOrder)
	if !ok {
		return nil, fmt.Errorf("expected a CronOrder object for the oldObj but got %T", oldObj)
	}

	cronorderlog.V(1).Info("Validating CronOrder update", "name", cronOrder.Name)

	// Validate cron order specification
	if warnings, err := v.validateCronOrderSpec(cronOrder); err != nil {
		return warnings, fmt.Errorf("cronorder specification validation failed: %w", err)
	}

	// Validate immutable fields
	if warnings, err := v.validateImmutableFields(oldCronOrder, cronOrder); err != nil {
		return warnings, fmt.Errorf("immutable field validation failed: %w", err)
	}

	return nil, nil
}

// validateCronOrderSpec validates the cron order specification
func (v *CronOrderCustomValidator) validateCronOrderSpec(cronOrder *corev1alpha1.CronOrder) (admission.Warnings, error) {
	var warnings admission.Warnings

	// Validate schedule format
	if err := v.validateSchedule(cronOrder.Spec.Schedule); err != nil {
		return warnings, fmt.Errorf("invalid schedule: %w", err)
	}

	// Validate timezone if specified
	if cronOrder.Spec.TimeZone != nil {
		if err := v.validateTimeZone(*cronOrder.Spec.TimeZone); err != nil {
			return warnings, fmt.Errorf("invalid timezone: %w", err)
		}
	}

	// Validate starting deadline seconds
	if cronOrder.Spec.StartingDeadlineSeconds != nil && *cronOrder.Spec.StartingDeadlineSeconds < 0 {
		return warnings, fmt.Errorf("startingDeadlineSeconds cannot be negative")
	}

	// Add warning for very short starting deadline
	if cronOrder.Spec.StartingDeadlineSeconds != nil && *cronOrder.Spec.StartingDeadlineSeconds < 10 {
		warnings = append(warnings, "Very short starting deadline may cause orders to be skipped frequently")
	}

	// Validate history limits
	if cronOrder.Spec.SuccessfulJobsHistoryLimit != nil && *cronOrder.Spec.SuccessfulJobsHistoryLimit < 0 {
		return warnings, fmt.Errorf("successfulJobsHistoryLimit cannot be negative")
	}

	if cronOrder.Spec.FailedJobsHistoryLimit != nil && *cronOrder.Spec.FailedJobsHistoryLimit < 0 {
		return warnings, fmt.Errorf("failedJobsHistoryLimit cannot be negative")
	}

	// Add warning for very high history limits
	successfulLimit := int32(3)
	if cronOrder.Spec.SuccessfulJobsHistoryLimit != nil {
		successfulLimit = *cronOrder.Spec.SuccessfulJobsHistoryLimit
	}
	failedLimit := int32(1)
	if cronOrder.Spec.FailedJobsHistoryLimit != nil {
		failedLimit = *cronOrder.Spec.FailedJobsHistoryLimit
	}

	if successfulLimit+failedLimit > 50 {
		warnings = append(warnings, "High history limits may consume excessive cluster resources")
	}

	// Validate order template
	if err := v.validateOrderTemplate(&cronOrder.Spec.OrderTemplate); err != nil {
		return warnings, fmt.Errorf("order template validation failed: %w", err)
	}

	return warnings, nil
}

// validateSchedule validates the cron schedule format
func (v *CronOrderCustomValidator) validateSchedule(schedule string) error {
	if strings.TrimSpace(schedule) == "" {
		return fmt.Errorf("schedule cannot be empty")
	}

	// Use internal scheduler to validate the schedule
	cronScheduler := scheduler.NewCronScheduler()
	if err := cronScheduler.ValidateSchedule(schedule); err != nil {
		return fmt.Errorf("invalid cron schedule format: %w", err)
	}

	return nil
}

// validateTimeZone validates the timezone format
func (v *CronOrderCustomValidator) validateTimeZone(timeZone string) error {
	if strings.TrimSpace(timeZone) == "" {
		return fmt.Errorf("timezone cannot be empty when specified")
	}

	// Try to load the timezone
	_, err := time.LoadLocation(timeZone)
	if err != nil {
		return fmt.Errorf("invalid timezone: %w", err)
	}

	return nil
}

// validateOrderTemplate validates the order template
func (v *CronOrderCustomValidator) validateOrderTemplate(template *corev1alpha1.OrderTemplate) error {
	// Validate replicas
	if template.Spec.Replicas != nil && *template.Spec.Replicas < 0 {
		return fmt.Errorf("order template replicas cannot be negative")
	}

	// Validate minReadySeconds
	if template.Spec.MinReadySeconds != nil && *template.Spec.MinReadySeconds < 0 {
		return fmt.Errorf("order template minReadySeconds cannot be negative")
	}

	// Validate ticket template
	if err := v.validateTicketTemplate(&template.Spec.Template); err != nil {
		return fmt.Errorf("order template ticket template validation failed: %w", err)
	}

	// Validate selectors if specified
	if template.Spec.Selector != nil && len(template.Spec.Selector.MatchLabels) == 0 && len(template.Spec.Selector.MatchExpressions) == 0 {
		return fmt.Errorf("order template selector cannot be empty when specified")
	}

	return nil
}

// validateTicketTemplate validates the ticket template
func (v *CronOrderCustomValidator) validateTicketTemplate(template *corev1alpha1.TicketTemplate) error {
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

	return nil
}

// validateImmutableFields validates that immutable fields haven't changed
func (v *CronOrderCustomValidator) validateImmutableFields(oldCronOrder, newCronOrder *corev1alpha1.CronOrder) (admission.Warnings, error) {
	var warnings admission.Warnings

	// Schedule changes should be warned about
	if oldCronOrder.Spec.Schedule != newCronOrder.Spec.Schedule {
		warnings = append(warnings, "Changing schedule will affect when orders are created")
	}

	// TimeZone changes should be warned about
	oldTZ := ""
	if oldCronOrder.Spec.TimeZone != nil {
		oldTZ = *oldCronOrder.Spec.TimeZone
	}
	newTZ := ""
	if newCronOrder.Spec.TimeZone != nil {
		newTZ = *newCronOrder.Spec.TimeZone
	}
	if oldTZ != newTZ {
		warnings = append(warnings, "Changing timezone may affect order scheduling times")
	}

	return warnings, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type CronOrder.
func (v *CronOrderCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	cronOrder, ok := obj.(*corev1alpha1.CronOrder)
	if !ok {
		return nil, fmt.Errorf("expected a CronOrder object but got %T", obj)
	}

	cronorderlog.V(1).Info("Validating CronOrder deletion", "name", cronOrder.Name)

	// Check if cronorder has active orders
	warnings, err := v.validateCronOrderDeletion(ctx, cronOrder)
	if err != nil {
		return warnings, err
	}

	return warnings, nil
}

// validateCronOrderDeletion validates that it's safe to delete the cron order
func (v *CronOrderCustomValidator) validateCronOrderDeletion(ctx context.Context, cronOrder *corev1alpha1.CronOrder) (admission.Warnings, error) {
	var warnings admission.Warnings

	// Get orders owned by this cron order
	orderList := &corev1alpha1.OrderList{}
	listOpts := []client.ListOption{
		client.InNamespace(cronOrder.Namespace),
		client.MatchingLabels{"korder.dev/cronorder": cronOrder.Name},
	}

	if err := v.Client.List(ctx, orderList, listOpts...); err != nil {
		return warnings, fmt.Errorf("failed to list orders: %w", err)
	}

	// Check for active orders
	activeOrders := 0
	for _, order := range orderList.Items {
		// Consider orders that are not in terminal state as active
		// Check if the order has Ready or Progressing conditions as True
		isActive := true
		for _, condition := range order.Status.Conditions {
			if condition.Type == corev1alpha1.OrderReady && condition.Status == "False" {
				// If Ready is False and there's a Failure condition, consider it inactive
				for _, failureCond := range order.Status.Conditions {
					if failureCond.Type == corev1alpha1.OrderFailure && failureCond.Status == "True" {
						isActive = false
						break
					}
				}
			}
		}
		if isActive {
			activeOrders++
		}
	}

	if activeOrders > 0 {
		warnings = append(warnings, fmt.Sprintf("CronOrder has %d active orders that will be orphaned", activeOrders))
	}

	// Allow deletion but warn about consequences
	return warnings, nil
}
