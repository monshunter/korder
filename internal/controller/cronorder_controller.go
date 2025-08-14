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
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	corev1alpha1 "github.com/monshunter/korder/api/v1alpha1"
	"github.com/monshunter/korder/internal/metrics"
	"github.com/monshunter/korder/internal/scheduler"
)

// CronOrderReconciler reconciles a CronOrder object
type CronOrderReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	CronScheduler *scheduler.CronScheduler
}

// +kubebuilder:rbac:groups=core.korder.dev,resources=cronorders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.korder.dev,resources=cronorders/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.korder.dev,resources=cronorders/finalizers,verbs=update
// +kubebuilder:rbac:groups=core.korder.dev,resources=orders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

const (
	CronOrderFinalizerName = "korder.dev/cronorder-finalizer"
)

// Reconcile manages the lifecycle of orders based on cron schedule
func (r *CronOrderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime).Seconds()
		metrics.ObserveReconciliationDuration("cronorder", "success", duration)
	}()

	// Fetch the CronOrder instance
	cronOrder := &corev1alpha1.CronOrder{}
	if err := r.Get(ctx, req.NamespacedName, cronOrder); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("CronOrder resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get CronOrder")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if cronOrder.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, cronOrder)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(cronOrder, CronOrderFinalizerName) {
		controllerutil.AddFinalizer(cronOrder, CronOrderFinalizerName)
		if err := r.Update(ctx, cronOrder); err != nil {
			log.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Handle suspended cron orders
	if cronOrder.Spec.Suspend != nil && *cronOrder.Spec.Suspend {
		log.Info("CronOrder is suspended, skipping reconciliation")
		return r.updateCronOrderStatus(ctx, cronOrder)
	}

	// Initialize scheduler if not set
	if r.CronScheduler == nil {
		r.CronScheduler = scheduler.NewCronScheduler()
	}

	// Validate the schedule format
	if err := r.CronScheduler.ValidateSchedule(cronOrder.Spec.Schedule); err != nil {
		log.Error(err, "Invalid schedule format", "schedule", cronOrder.Spec.Schedule)
		return ctrl.Result{RequeueAfter: time.Minute}, fmt.Errorf("invalid schedule format: %w", err)
	}

	// Check if it's time to create a new order
	scheduledTime, err := r.getScheduledTimeForCronOrder(ctx, cronOrder, time.Now())
	if err != nil {
		log.Error(err, "Failed to get scheduled time")
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	if scheduledTime == nil {
		// No new order needs to be created
		log.V(1).Info("No scheduled time found, updating status")
		result, err := r.updateCronOrderStatus(ctx, cronOrder)
		if err != nil {
			return result, err
		}
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	log.Info("Creating new order for scheduled time", "scheduledTime", scheduledTime)

	// Create new order
	order, err := r.createOrderFromTemplate(ctx, cronOrder, *scheduledTime)
	if err != nil {
		log.Error(err, "Failed to create order from template")
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	log.Info("Created order from cron order", "order", order.Name, "scheduledTime", scheduledTime)

	// Update last schedule time
	cronOrder.Status.LastScheduleTime = &metav1.Time{Time: *scheduledTime}

	// Clean up old orders based on history limits
	if err := r.cleanupOldOrders(ctx, cronOrder); err != nil {
		log.Error(err, "Failed to cleanup old orders")
		// Don't return error, just log it
	}

	// Update status
	if _, err := r.updateCronOrderStatus(ctx, cronOrder); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// handleDeletion handles the cleanup when a cron order is deleted
func (r *CronOrderReconciler) handleDeletion(ctx context.Context, cronOrder *corev1alpha1.CronOrder) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Clean up owned orders (optional, depending on policy)
	// For now, we let orders continue running even if cron order is deleted

	// Remove finalizer
	controllerutil.RemoveFinalizer(cronOrder, CronOrderFinalizerName)
	if err := r.Update(ctx, cronOrder); err != nil {
		log.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	log.Info("CronOrder deleted successfully")
	return ctrl.Result{}, nil
}

// getScheduledTimeForCronOrder determines if a new order should be created
func (r *CronOrderReconciler) getScheduledTimeForCronOrder(ctx context.Context, cronOrder *corev1alpha1.CronOrder, now time.Time) (*time.Time, error) {
	log := logf.FromContext(ctx)

	// Get the schedule for the cron order
	sched, err := r.CronScheduler.ParseSchedule(cronOrder.Spec.Schedule)
	if err != nil {
		return nil, fmt.Errorf("failed to parse schedule %q: %w", cronOrder.Spec.Schedule, err)
	}

	// Find the most recent time that we should have created an order
	var earliestTime time.Time
	if cronOrder.Status.LastScheduleTime != nil {
		earliestTime = cronOrder.Status.LastScheduleTime.Time
	} else {
		// If we've never scheduled before, don't go back more than 100 schedules
		earliestTime = now.Add(-time.Hour * 24 * 7) // Go back 1 week max
	}

	// Check if we need to handle starting deadline
	if cronOrder.Spec.StartingDeadlineSeconds != nil {
		deadline := time.Duration(*cronOrder.Spec.StartingDeadlineSeconds) * time.Second
		if now.Sub(earliestTime) > deadline {
			earliestTime = now.Add(-deadline)
		}
	}

	// Find times when we should have created orders
	var scheduledTimes []time.Time
	for t := sched.Next(earliestTime); !t.After(now); t = sched.Next(t) {
		scheduledTimes = append(scheduledTimes, t)
		if len(scheduledTimes) > 100 {
			// Safety check to prevent infinite loops
			break
		}
	}

	if len(scheduledTimes) == 0 {
		log.V(1).Info("No scheduled times found", "now", now, "earliestTime", earliestTime)
		return nil, nil
	}

	// Check if we already have orders for these scheduled times
	orders, err := r.getOrdersOwnedByCronOrder(ctx, cronOrder)
	if err != nil {
		return nil, err
	}

	// Create a map of existing order scheduled times
	orderScheduleMap := make(map[time.Time]bool)
	for _, order := range orders {
		if scheduledTimeStr, exists := order.Annotations["korder.dev/scheduled-time"]; exists {
			if scheduledTime, err := time.Parse(time.RFC3339, scheduledTimeStr); err == nil {
				orderScheduleMap[scheduledTime] = true
			}
		}
	}

	// Find the latest time we haven't created an order for
	for i := len(scheduledTimes) - 1; i >= 0; i-- {
		scheduledTime := scheduledTimes[i]
		if !orderScheduleMap[scheduledTime] {
			// Check concurrency policy
			if len(orders) > 0 && cronOrder.Spec.ConcurrencyPolicy != corev1alpha1.AllowConcurrent {
				activeOrders := r.filterActiveOrders(orders)
				if len(activeOrders) > 0 {
					switch cronOrder.Spec.ConcurrencyPolicy {
					case corev1alpha1.ForbidConcurrent:
						log.Info("Skipping scheduled time due to ForbidConcurrent policy", "scheduledTime", scheduledTime)
						continue
					case corev1alpha1.ReplaceConcurrent:
						// Delete active orders
						for _, order := range activeOrders {
							if err := r.Delete(ctx, &order); err != nil {
								log.Error(err, "Failed to delete order for replacement", "order", order.Name)
							} else {
								log.Info("Deleted order for replacement", "order", order.Name)
							}
						}
					}
				}
			}
			return &scheduledTime, nil
		}
	}

	log.V(1).Info("All scheduled times already have orders", "scheduledTimes", len(scheduledTimes))
	return nil, nil
}

// createOrderFromTemplate creates an order from the cron order template
func (r *CronOrderReconciler) createOrderFromTemplate(ctx context.Context, cronOrder *corev1alpha1.CronOrder, scheduledTime time.Time) (*corev1alpha1.Order, error) {
	// Generate order name with scheduled time
	orderName := fmt.Sprintf("%s-%d", cronOrder.Name, scheduledTime.Unix())

	order := &corev1alpha1.Order{
		ObjectMeta: metav1.ObjectMeta{
			Name:      orderName,
			Namespace: cronOrder.Namespace,
			Labels:    make(map[string]string),
			Annotations: map[string]string{
				"korder.dev/scheduled-time": scheduledTime.Format(time.RFC3339),
			},
		},
		Spec: cronOrder.Spec.OrderTemplate.Spec,
	}

	// Copy labels from template
	for k, v := range cronOrder.Spec.OrderTemplate.Labels {
		order.Labels[k] = v
	}

	// Add cron order-specific labels
	order.Labels["korder.dev/cron-order"] = cronOrder.Name
	order.Labels["korder.dev/cron-order-uid"] = string(cronOrder.UID)

	// Set owner reference
	if err := controllerutil.SetControllerReference(cronOrder, order, r.Scheme); err != nil {
		return nil, err
	}

	if err := r.Create(ctx, order); err != nil {
		return nil, err
	}

	return order, nil
}

// getOrdersOwnedByCronOrder returns orders owned by the cron order
func (r *CronOrderReconciler) getOrdersOwnedByCronOrder(ctx context.Context, cronOrder *corev1alpha1.CronOrder) ([]corev1alpha1.Order, error) {
	orderList := &corev1alpha1.OrderList{}
	listOpts := []client.ListOption{
		client.InNamespace(cronOrder.Namespace),
		client.MatchingLabels{"korder.dev/cron-order": cronOrder.Name},
	}

	if err := r.List(ctx, orderList, listOpts...); err != nil {
		return nil, err
	}

	return orderList.Items, nil
}

// filterActiveOrders returns orders that are not completed
func (r *CronOrderReconciler) filterActiveOrders(orders []corev1alpha1.Order) []corev1alpha1.Order {
	var activeOrders []corev1alpha1.Order
	for _, order := range orders {
		// For simplicity, consider all orders as active unless they have a completion condition
		// In a real implementation, you'd check for specific completion conditions
		isCompleted := false
		for _, condition := range order.Status.Conditions {
			if condition.Type == corev1alpha1.OrderReady && condition.Status == corev1.ConditionTrue {
				// Check if all tickets are expired or claimed
				// For now, assume orders are active unless explicitly marked as failed
				if condition.Reason != nil && *condition.Reason == "Failed" {
					isCompleted = true
					break
				}
			}
		}
		if !isCompleted {
			activeOrders = append(activeOrders, order)
		}
	}
	return activeOrders
}

// cleanupOldOrders removes old orders based on history limits
func (r *CronOrderReconciler) cleanupOldOrders(ctx context.Context, cronOrder *corev1alpha1.CronOrder) error {
	log := logf.FromContext(ctx)

	orders, err := r.getOrdersOwnedByCronOrder(ctx, cronOrder)
	if err != nil {
		return err
	}

	// Separate successful and failed orders
	var successfulOrders, failedOrders []corev1alpha1.Order
	for _, order := range orders {
		isSuccessful := true
		for _, condition := range order.Status.Conditions {
			if condition.Type == corev1alpha1.OrderFailure && condition.Status == corev1.ConditionTrue {
				isSuccessful = false
				break
			}
		}

		if isSuccessful {
			successfulOrders = append(successfulOrders, order)
		} else {
			failedOrders = append(failedOrders, order)
		}
	}

	// Sort orders by creation time (newest first)
	sort.Slice(successfulOrders, func(i, j int) bool {
		return successfulOrders[i].CreationTimestamp.After(successfulOrders[j].CreationTimestamp.Time)
	})
	sort.Slice(failedOrders, func(i, j int) bool {
		return failedOrders[i].CreationTimestamp.After(failedOrders[j].CreationTimestamp.Time)
	})

	// Delete old successful orders
	successfulLimit := int32(3)
	if cronOrder.Spec.SuccessfulJobsHistoryLimit != nil {
		successfulLimit = *cronOrder.Spec.SuccessfulJobsHistoryLimit
	}
	if len(successfulOrders) > int(successfulLimit) {
		for i := int(successfulLimit); i < len(successfulOrders); i++ {
			order := successfulOrders[i]
			if err := r.Delete(ctx, &order); err != nil {
				log.Error(err, "Failed to delete old successful order", "order", order.Name)
			} else {
				log.Info("Deleted old successful order", "order", order.Name)
			}
		}
	}

	// Delete old failed orders
	failedLimit := int32(1)
	if cronOrder.Spec.FailedJobsHistoryLimit != nil {
		failedLimit = *cronOrder.Spec.FailedJobsHistoryLimit
	}
	if len(failedOrders) > int(failedLimit) {
		for i := int(failedLimit); i < len(failedOrders); i++ {
			order := failedOrders[i]
			if err := r.Delete(ctx, &order); err != nil {
				log.Error(err, "Failed to delete old failed order", "order", order.Name)
			} else {
				log.Info("Deleted old failed order", "order", order.Name)
			}
		}
	}

	return nil
}

// updateCronOrderStatus updates the cron order status
func (r *CronOrderReconciler) updateCronOrderStatus(ctx context.Context, cronOrder *corev1alpha1.CronOrder) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Get active orders
	orders, err := r.getOrdersOwnedByCronOrder(ctx, cronOrder)
	if err != nil {
		log.Error(err, "Failed to get owned orders for status update")
		return ctrl.Result{}, err
	}

	activeOrders := r.filterActiveOrders(orders)

	// Update active orders in status
	cronOrder.Status.Active = []corev1.ObjectReference{}
	for _, order := range activeOrders {
		cronOrder.Status.Active = append(cronOrder.Status.Active, corev1.ObjectReference{
			APIVersion: order.APIVersion,
			Kind:       order.Kind,
			Name:       order.Name,
			Namespace:  order.Namespace,
			UID:        order.UID,
		})
	}

	// Find last successful completion time
	var lastSuccessfulTime *metav1.Time
	for _, order := range orders {
		for _, condition := range order.Status.Conditions {
			if condition.Type == corev1alpha1.OrderReady && condition.Status == corev1.ConditionTrue {
				if condition.LastTransitionTime != nil {
					if lastSuccessfulTime == nil || condition.LastTransitionTime.After(lastSuccessfulTime.Time) {
						lastSuccessfulTime = condition.LastTransitionTime
					}
				}
			}
		}
	}
	cronOrder.Status.LastSuccessfulTime = lastSuccessfulTime

	if err := r.Status().Update(ctx, cronOrder); err != nil {
		log.Error(err, "Failed to update cron order status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CronOrderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.CronOrder{}).
		Owns(&corev1alpha1.Order{}).
		Named("cronorder").
		Complete(r)
}
