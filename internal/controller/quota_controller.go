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

package controller

import (
	"context"
	"fmt"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	corev1alpha1 "github.com/monshunter/korder/api/v1alpha1"
	"github.com/monshunter/korder/internal/metrics"
)

// QuotaReconciler reconciles a Quota object
type QuotaReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.korder.dev,resources=quotas,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.korder.dev,resources=quotas/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.korder.dev,resources=quotas/finalizers,verbs=update
// +kubebuilder:rbac:groups=core.korder.dev,resources=orders,verbs=get;list;watch
// +kubebuilder:rbac:groups=core.korder.dev,resources=tickets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

const (
	QuotaFinalizerName = "korder.dev/quota-finalizer"
)

// Reconcile enforces resource quotas on Orders and Tickets
func (r *QuotaReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime).Seconds()
		metrics.ObserveReconciliationDuration("quota", "success", duration)
	}()

	// Fetch the Quota instance
	quota := &corev1alpha1.Quota{}
	if err := r.Get(ctx, req.NamespacedName, quota); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Quota resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Quota")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if quota.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, quota)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(quota, QuotaFinalizerName) {
		controllerutil.AddFinalizer(quota, QuotaFinalizerName)
		if err := r.Update(ctx, quota); err != nil {
			log.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Calculate current resource usage
	usage, err := r.calculateResourceUsage(ctx, quota)
	if err != nil {
		log.Error(err, "Failed to calculate resource usage")
		return r.updateQuotaStatus(ctx, quota, "Failed", fmt.Sprintf("Failed to calculate usage: %v", err))
	}

	// Apply time window multipliers if applicable
	currentLimits := r.calculateCurrentLimits(quota)

	// Update quota status
	quota.Status.Hard = currentLimits
	quota.Status.Used = usage

	// Check if quota is exceeded
	exceeded := r.isQuotaExceeded(usage, currentLimits)

	if exceeded {
		log.Info("Quota exceeded", "quota", quota.Name, "used", usage, "hard", currentLimits)
		return r.updateQuotaStatus(ctx, quota, "Exceeded", "Resource quota limits exceeded")
	}

	log.V(1).Info("Quota within limits", "quota", quota.Name, "used", usage, "hard", currentLimits)
	return r.updateQuotaStatus(ctx, quota, "Enforced", "Quota is being enforced successfully")
}

// handleDeletion handles the cleanup when a quota is deleted
func (r *QuotaReconciler) handleDeletion(ctx context.Context, quota *corev1alpha1.Quota) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Remove finalizer (no cleanup needed for quotas)
	controllerutil.RemoveFinalizer(quota, QuotaFinalizerName)
	if err := r.Update(ctx, quota); err != nil {
		log.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	log.Info("Quota deleted successfully")
	return ctrl.Result{}, nil
}

// calculateResourceUsage calculates current resource usage based on quota scope
func (r *QuotaReconciler) calculateResourceUsage(ctx context.Context, quota *corev1alpha1.Quota) (corev1.ResourceList, error) {
	usage := corev1.ResourceList{}

	// Get orders and tickets based on quota scope
	orders, tickets, err := r.getResourcesInScope(ctx, quota)
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

	// Calculate reserved resources
	var totalCPU, totalMemory resource.Quantity
	for _, ticket := range tickets {
		if ticket.Spec.Resources != nil {
			if cpu, exists := ticket.Spec.Resources.Requests[corev1.ResourceCPU]; exists {
				totalCPU.Add(cpu)
			}
			if memory, exists := ticket.Spec.Resources.Requests[corev1.ResourceMemory]; exists {
				totalMemory.Add(memory)
			}
		}
	}

	if !totalCPU.IsZero() {
		usage[corev1.ResourceName("reserved.cpu")] = totalCPU
	}
	if !totalMemory.IsZero() {
		usage[corev1.ResourceName("reserved.memory")] = totalMemory
	}

	return usage, nil
}

// getResourcesInScope returns orders and tickets that fall within the quota scope
func (r *QuotaReconciler) getResourcesInScope(ctx context.Context, quota *corev1alpha1.Quota) ([]corev1alpha1.Order, []corev1alpha1.Ticket, error) {
	var orders []corev1alpha1.Order
	var tickets []corev1alpha1.Ticket

	switch quota.Spec.Scope.Type {
	case corev1alpha1.ClusterScope:
		// Get all orders and tickets in the cluster
		orderList := &corev1alpha1.OrderList{}
		if err := r.List(ctx, orderList); err != nil {
			return nil, nil, err
		}
		orders = orderList.Items

		ticketList := &corev1alpha1.TicketList{}
		if err := r.List(ctx, ticketList); err != nil {
			return nil, nil, err
		}
		tickets = ticketList.Items

	case corev1alpha1.NamespaceListScope:
		// Get resources from specified namespaces
		for _, ns := range quota.Spec.Scope.Namespaces {
			orderList := &corev1alpha1.OrderList{}
			if err := r.List(ctx, orderList, client.InNamespace(ns)); err != nil {
				return nil, nil, err
			}
			orders = append(orders, orderList.Items...)

			ticketList := &corev1alpha1.TicketList{}
			if err := r.List(ctx, ticketList, client.InNamespace(ns)); err != nil {
				return nil, nil, err
			}
			tickets = append(tickets, ticketList.Items...)
		}

	case corev1alpha1.NamespaceSelectorScope:
		// Get namespaces matching selector
		namespaces, err := r.getNamespacesMatchingSelector(ctx, quota.Spec.Scope.NamespaceSelector)
		if err != nil {
			return nil, nil, err
		}

		for _, ns := range namespaces {
			orderList := &corev1alpha1.OrderList{}
			if err := r.List(ctx, orderList, client.InNamespace(ns.Name)); err != nil {
				return nil, nil, err
			}
			orders = append(orders, orderList.Items...)

			ticketList := &corev1alpha1.TicketList{}
			if err := r.List(ctx, ticketList, client.InNamespace(ns.Name)); err != nil {
				return nil, nil, err
			}
			tickets = append(tickets, ticketList.Items...)
		}

	case corev1alpha1.ObjectSelectorScope:
		// Get resources matching object selector
		if quota.Spec.Scope.ObjectSelector != nil {
			selector, err := metav1.LabelSelectorAsSelector(quota.Spec.Scope.ObjectSelector)
			if err != nil {
				return nil, nil, err
			}

			orderList := &corev1alpha1.OrderList{}
			if err := r.List(ctx, orderList, client.MatchingLabelsSelector{Selector: selector}); err != nil {
				return nil, nil, err
			}
			orders = orderList.Items

			ticketList := &corev1alpha1.TicketList{}
			if err := r.List(ctx, ticketList, client.MatchingLabelsSelector{Selector: selector}); err != nil {
				return nil, nil, err
			}
			tickets = ticketList.Items
		}
	}

	return orders, tickets, nil
}

// getNamespacesMatchingSelector returns namespaces matching the label selector
func (r *QuotaReconciler) getNamespacesMatchingSelector(ctx context.Context, selector *metav1.LabelSelector) ([]corev1.Namespace, error) {
	if selector == nil {
		return nil, nil
	}

	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, err
	}

	namespaceList := &corev1.NamespaceList{}
	if err := r.List(ctx, namespaceList, client.MatchingLabelsSelector{Selector: labelSelector}); err != nil {
		return nil, err
	}

	return namespaceList.Items, nil
}

// calculateCurrentLimits calculates current limits considering time windows
func (r *QuotaReconciler) calculateCurrentLimits(quota *corev1alpha1.Quota) corev1.ResourceList {
	limits := quota.Spec.Hard.DeepCopy()

	// Apply time window multipliers
	if len(quota.Spec.TimeWindows) > 0 {
		activeWindow := r.getActiveTimeWindow(quota.Spec.TimeWindows)
		if activeWindow != nil {
			multiplier, err := strconv.ParseFloat(activeWindow.Multiplier, 64)
			if err == nil && multiplier > 0 {
				for resourceName, quantity := range limits {
					if quantity.Value() > 0 {
						newValue := int64(float64(quantity.Value()) * multiplier)
						limits[resourceName] = *resource.NewQuantity(newValue, quantity.Format)
					}
				}
				// Update active time window in status
				quota.Status.ActiveTimeWindow = &activeWindow.Name
			}
		}
	}

	return limits
}

// getActiveTimeWindow returns the currently active time window (simplified implementation)
func (r *QuotaReconciler) getActiveTimeWindow(timeWindows []corev1alpha1.TimeWindow) *corev1alpha1.TimeWindow {
	// TODO: Implement proper cron schedule evaluation
	// For now, return the first time window as a placeholder
	if len(timeWindows) > 0 {
		return &timeWindows[0]
	}
	return nil
}

// isQuotaExceeded checks if current usage exceeds the limits
func (r *QuotaReconciler) isQuotaExceeded(usage, limits corev1.ResourceList) bool {
	for resourceName, limit := range limits {
		if used, exists := usage[resourceName]; exists {
			if used.Cmp(limit) > 0 {
				return true
			}
		}
	}
	return false
}

// updateQuotaStatus updates the quota status
func (r *QuotaReconciler) updateQuotaStatus(ctx context.Context, quota *corev1alpha1.Quota, reason, message string) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	quota.Status.ObservedGeneration = &quota.Generation

	// Update conditions
	now := metav1.Now()
	conditionType := corev1alpha1.QuotaEnforced
	conditionStatus := corev1.ConditionTrue

	switch reason {
	case "Failed":
		conditionType = corev1alpha1.QuotaReady
		conditionStatus = corev1.ConditionFalse
	case "Exceeded":
		conditionType = corev1alpha1.QuotaExceeded
		conditionStatus = corev1.ConditionTrue
	}

	// Find existing condition or create new one
	var existingCondition *corev1alpha1.QuotaCondition
	for i := range quota.Status.Conditions {
		if quota.Status.Conditions[i].Type == conditionType {
			existingCondition = &quota.Status.Conditions[i]
			break
		}
	}

	if existingCondition == nil {
		quota.Status.Conditions = append(quota.Status.Conditions, corev1alpha1.QuotaCondition{
			Type:               conditionType,
			Status:             conditionStatus,
			LastTransitionTime: &now,
			Reason:             &reason,
			Message:            &message,
		})
	} else if existingCondition.Status != conditionStatus {
		existingCondition.Status = conditionStatus
		existingCondition.LastTransitionTime = &now
		existingCondition.Reason = &reason
		existingCondition.Message = &message
	}

	if err := r.Status().Update(ctx, quota); err != nil {
		log.Error(err, "Failed to update quota status")
		return ctrl.Result{}, err
	}

	// Requeue to check quota usage regularly
	return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
}

// ValidateQuota validates if a new order or ticket would exceed quotas
func (r *QuotaReconciler) ValidateQuota(ctx context.Context, namespace string, resourceRequests corev1.ResourceList) error {
	// Get all quotas that might apply
	quotaList := &corev1alpha1.QuotaList{}
	if err := r.List(ctx, quotaList); err != nil {
		return err
	}

	for _, quota := range quotaList.Items {
		if r.doesQuotaApply(ctx, &quota, namespace) {
			// Calculate current usage
			usage, err := r.calculateResourceUsage(ctx, &quota)
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
			limits := r.calculateCurrentLimits(&quota)
			if r.isQuotaExceeded(projectedUsage, limits) {
				return fmt.Errorf("quota %s would be exceeded: projected usage %v exceeds limits %v", quota.Name, projectedUsage, limits)
			}
		}
	}

	return nil
}

// doesQuotaApply checks if a quota applies to the given namespace
func (r *QuotaReconciler) doesQuotaApply(ctx context.Context, quota *corev1alpha1.Quota, namespace string) bool {
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
		// Check if namespace matches selector
		ns := &corev1.Namespace{}
		if err := r.Get(ctx, client.ObjectKey{Name: namespace}, ns); err != nil {
			return false
		}

		if quota.Spec.Scope.NamespaceSelector != nil {
			selector, err := metav1.LabelSelectorAsSelector(quota.Spec.Scope.NamespaceSelector)
			if err != nil {
				return false
			}
			return selector.Matches(labels.Set(ns.Labels))
		}
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *QuotaReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.Quota{}).
		Named("quota").
		Complete(r)
}
