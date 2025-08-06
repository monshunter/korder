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

// OrderReconciler reconciles a Order object
type OrderReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	CronScheduler *scheduler.CronScheduler
}

// +kubebuilder:rbac:groups=core.korder.dev,resources=orders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.korder.dev,resources=orders/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.korder.dev,resources=orders/finalizers,verbs=update
// +kubebuilder:rbac:groups=core.korder.dev,resources=tickets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

const (
	OrderFinalizerName = "korder.dev/order-finalizer"
)

// Reconcile manages the lifecycle of tickets based on order specifications
func (r *OrderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime).Seconds()
		metrics.ObserveReconciliationDuration("order", "success", duration)
	}()

	// Fetch the Order instance
	order := &corev1alpha1.Order{}
	if err := r.Get(ctx, req.NamespacedName, order); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Order resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Order")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if order.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, order)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(order, OrderFinalizerName) {
		controllerutil.AddFinalizer(order, OrderFinalizerName)
		if err := r.Update(ctx, order); err != nil {
			log.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Handle paused orders
	if order.Spec.Paused != nil && *order.Spec.Paused {
		log.Info("Order is paused, skipping reconciliation")
		return r.updateOrderStatus(ctx, order, "Paused", "Order is paused")
	}

	// Reconcile tickets based on strategy
	result, err := r.reconcileTickets(ctx, order)
	if err != nil {
		log.Error(err, "Failed to reconcile tickets")
		if _, updateErr := r.updateOrderStatus(ctx, order, "Failed", fmt.Sprintf("Failed to reconcile tickets: %v", err)); updateErr != nil {
			log.Error(updateErr, "Failed to update order status after reconcile error")
		}
		return ctrl.Result{}, err
	}

	// Update order status
	if _, err := r.updateOrderStatus(ctx, order, "Ready", "Order is processing normally"); err != nil {
		return ctrl.Result{}, err
	}

	return result, nil
}

// handleDeletion handles the cleanup when an order is deleted
func (r *OrderReconciler) handleDeletion(ctx context.Context, order *corev1alpha1.Order) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Clean up owned tickets
	if err := r.deleteOwnedTickets(ctx, order); err != nil {
		log.Error(err, "Failed to delete owned tickets")
		return ctrl.Result{}, err
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(order, OrderFinalizerName)
	if err := r.Update(ctx, order); err != nil {
		log.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	log.Info("Order deleted successfully")
	return ctrl.Result{}, nil
}

// reconcileTickets manages ticket creation and lifecycle based on order strategy
func (r *OrderReconciler) reconcileTickets(ctx context.Context, order *corev1alpha1.Order) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Get current tickets owned by this order
	currentTickets, err := r.getOwnedTickets(ctx, order)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Calculate desired replicas
	desiredReplicas := int32(1)
	if order.Spec.Replicas != nil {
		desiredReplicas = *order.Spec.Replicas
	}

	// Handle different strategies
	switch order.Spec.Strategy.Type {
	case corev1alpha1.OneTimeStrategy:
		return r.handleOneTimeStrategy(ctx, order, currentTickets, desiredReplicas)
	case corev1alpha1.ScheduledStrategy:
		return r.handleScheduledStrategy(ctx, order, currentTickets, desiredReplicas)
	case corev1alpha1.RecurringStrategy:
		return r.handleRecurringStrategy(ctx, order, currentTickets, desiredReplicas)
	default:
		log.Error(fmt.Errorf("unknown strategy type: %s", order.Spec.Strategy.Type), "Invalid strategy")
		return ctrl.Result{}, fmt.Errorf("unknown strategy type: %s", order.Spec.Strategy.Type)
	}
}

// handleOneTimeStrategy creates tickets once
func (r *OrderReconciler) handleOneTimeStrategy(ctx context.Context, order *corev1alpha1.Order, currentTickets []corev1alpha1.Ticket, desiredReplicas int32) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// For OneTime strategy, create tickets if they don't exist
	activeTickets := filterActiveTickets(currentTickets)

	if int32(len(activeTickets)) < desiredReplicas {
		needed := desiredReplicas - int32(len(activeTickets))
		for i := int32(0); i < needed; i++ {
			if err := r.createTicket(ctx, order, int32(len(currentTickets))+i); err != nil {
				log.Error(err, "Failed to create ticket")
				return ctrl.Result{}, err
			}
		}
		log.Info("Created tickets for OneTime strategy", "created", needed)
	}

	return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
}

// handleScheduledStrategy creates tickets at scheduled time
func (r *OrderReconciler) handleScheduledStrategy(ctx context.Context, order *corev1alpha1.Order, currentTickets []corev1alpha1.Ticket, desiredReplicas int32) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Validate schedule
	if order.Spec.Strategy.Schedule == nil || *order.Spec.Strategy.Schedule == "" {
		log.Error(fmt.Errorf("schedule required for Scheduled strategy"), "Missing schedule")
		return ctrl.Result{}, fmt.Errorf("schedule required for Scheduled strategy")
	}

	// Initialize scheduler if not set
	if r.CronScheduler == nil {
		r.CronScheduler = scheduler.NewCronScheduler()
	}

	// Validate the schedule format
	if err := r.CronScheduler.ValidateSchedule(*order.Spec.Strategy.Schedule); err != nil {
		log.Error(err, "Invalid schedule format", "schedule", *order.Spec.Strategy.Schedule)
		return ctrl.Result{}, fmt.Errorf("invalid schedule format: %w", err)
	}

	// Check if it's time to run
	isTimeToRun, err := r.CronScheduler.IsTimeToRun(*order.Spec.Strategy.Schedule)
	if err != nil {
		log.Error(err, "Failed to check schedule", "schedule", *order.Spec.Strategy.Schedule)
		return ctrl.Result{}, err
	}

	if !isTimeToRun {
		// Calculate next run time for better requeue timing
		nextRun, err := r.CronScheduler.NextRunTime(*order.Spec.Strategy.Schedule)
		if err != nil {
			log.V(1).Info("Failed to calculate next run time, using default requeue", "error", err)
			return ctrl.Result{RequeueAfter: time.Minute}, nil
		}
		requeueAfter := time.Until(nextRun)
		if requeueAfter <= 0 {
			requeueAfter = time.Minute
		}
		log.V(1).Info("Not time to run, requeuing", "nextRun", nextRun, "requeueAfter", requeueAfter)
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	log.Info("Schedule matched, creating tickets", "schedule", *order.Spec.Strategy.Schedule)

	// Check if tickets already exist for this schedule run
	// For scheduled strategy, we typically create tickets once per schedule trigger
	activeTickets := filterActiveTickets(currentTickets)
	if len(activeTickets) >= int(desiredReplicas) {
		log.V(1).Info("Sufficient tickets already exist", "active", len(activeTickets), "desired", desiredReplicas)
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	// Create missing tickets
	needed := desiredReplicas - int32(len(activeTickets))
	for i := int32(0); i < needed; i++ {
		if err := r.createTicket(ctx, order, int32(len(currentTickets))+i); err != nil {
			log.Error(err, "Failed to create scheduled ticket")
			return ctrl.Result{}, err
		}
	}

	log.Info("Created scheduled tickets", "created", needed, "schedule", *order.Spec.Strategy.Schedule)
	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// handleRecurringStrategy creates tickets repeatedly
func (r *OrderReconciler) handleRecurringStrategy(ctx context.Context, order *corev1alpha1.Order, currentTickets []corev1alpha1.Ticket, desiredReplicas int32) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Validate schedule
	if order.Spec.Strategy.Schedule == nil || *order.Spec.Strategy.Schedule == "" {
		log.Error(fmt.Errorf("schedule required for Recurring strategy"), "Missing schedule")
		return ctrl.Result{}, fmt.Errorf("schedule required for Recurring strategy")
	}

	// Initialize scheduler if not set
	if r.CronScheduler == nil {
		r.CronScheduler = scheduler.NewCronScheduler()
	}

	// Validate the schedule format
	if err := r.CronScheduler.ValidateSchedule(*order.Spec.Strategy.Schedule); err != nil {
		log.Error(err, "Invalid schedule format", "schedule", *order.Spec.Strategy.Schedule)
		return ctrl.Result{}, fmt.Errorf("invalid schedule format: %w", err)
	}

	// Check if it's time to run
	isTimeToRun, err := r.CronScheduler.IsTimeToRun(*order.Spec.Strategy.Schedule)
	if err != nil {
		log.Error(err, "Failed to check schedule", "schedule", *order.Spec.Strategy.Schedule)
		return ctrl.Result{}, err
	}

	if !isTimeToRun {
		// For recurring strategy, still maintain desired replica count
		activeTickets := filterActiveTickets(currentTickets)
		if int32(len(activeTickets)) < desiredReplicas {
			needed := desiredReplicas - int32(len(activeTickets))
			for i := int32(0); i < needed; i++ {
				if err := r.createTicket(ctx, order, int32(len(currentTickets))+i); err != nil {
					log.Error(err, "Failed to create recurring ticket")
					return ctrl.Result{}, err
				}
			}
			log.Info("Created recurring tickets", "created", needed)
		}
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	log.Info("Recurring schedule matched", "schedule", *order.Spec.Strategy.Schedule)

	// For recurring strategy, handle refresh policy
	switch order.Spec.Strategy.RefreshPolicy {
	case corev1alpha1.AlwaysRefresh:
		// Delete all current tickets and create new ones
		for _, ticket := range currentTickets {
			if err := r.Delete(ctx, &ticket); err != nil {
				log.Error(err, "Failed to delete ticket for refresh", "ticket", ticket.Name)
			}
		}
		// Create new tickets
		for i := int32(0); i < desiredReplicas; i++ {
			if err := r.createTicket(ctx, order, i); err != nil {
				log.Error(err, "Failed to create refreshed ticket")
				return ctrl.Result{}, err
			}
		}
		log.Info("Refreshed all tickets", "count", desiredReplicas)
	case corev1alpha1.OnClaimRefresh:
		// Only refresh claimed tickets
		claimedCount := 0
		for _, ticket := range currentTickets {
			if ticket.Status.Phase == corev1alpha1.TicketClaimed {
				if err := r.Delete(ctx, &ticket); err != nil {
					log.Error(err, "Failed to delete claimed ticket", "ticket", ticket.Name)
				} else {
					claimedCount++
				}
			}
		}
		// Create replacements for claimed tickets
		for i := 0; i < claimedCount; i++ {
			if err := r.createTicket(ctx, order, int32(len(currentTickets))+int32(i)); err != nil {
				log.Error(err, "Failed to create replacement ticket")
				return ctrl.Result{}, err
			}
		}
		if claimedCount > 0 {
			log.Info("Refreshed claimed tickets", "count", claimedCount)
		}
	case corev1alpha1.NeverRefresh:
		// Maintain desired count without refreshing
		activeTickets := filterActiveTickets(currentTickets)
		if int32(len(activeTickets)) < desiredReplicas {
			needed := desiredReplicas - int32(len(activeTickets))
			for i := int32(0); i < needed; i++ {
				if err := r.createTicket(ctx, order, int32(len(currentTickets))+i); err != nil {
					log.Error(err, "Failed to create additional ticket")
					return ctrl.Result{}, err
				}
			}
			log.Info("Created additional tickets", "created", needed)
		}
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// createTicket creates a new ticket based on the order template
func (r *OrderReconciler) createTicket(ctx context.Context, order *corev1alpha1.Order, index int32) error {
	log := logf.FromContext(ctx)

	ticket := &corev1alpha1.Ticket{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", order.Name, index),
			Namespace: order.Namespace,
			Labels:    make(map[string]string),
		},
		Spec: corev1alpha1.TicketSpec{
			Lifecycle:                 order.Spec.Template.Spec.Lifecycle,
			StartTime:                 order.Spec.Template.Spec.StartTime,
			Duration:                  order.Spec.Template.Spec.Duration,
			SchedulerName:             order.Spec.Template.Spec.SchedulerName,
			PriorityClassName:         order.Spec.Template.Spec.PriorityClassName,
			Resources:                 order.Spec.Template.Spec.Resources,
			NodeName:                  order.Spec.Template.Spec.NodeName,
			NodeSelector:              order.Spec.Template.Spec.NodeSelector,
			Affinity:                  order.Spec.Template.Spec.Affinity,
			Tolerations:               order.Spec.Template.Spec.Tolerations,
			TopologySpreadConstraints: order.Spec.Template.Spec.TopologySpreadConstraints,
		},
	}

	// Copy labels from template
	for k, v := range order.Spec.Template.Labels {
		ticket.Labels[k] = v
	}

	// Add order-specific labels
	ticket.Labels["korder.dev/order"] = order.Name
	ticket.Labels["korder.dev/order-uid"] = string(order.UID)

	// Set owner reference
	if err := controllerutil.SetControllerReference(order, ticket, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, ticket); err != nil {
		log.Error(err, "Failed to create ticket", "ticket", ticket.Name)
		metrics.IncReconciliationErrors("order", "ticket_creation_failed")
		return err
	}

	// Update metrics
	metrics.IncTicketsTotal(order.Namespace, string(corev1alpha1.TicketPending))

	log.Info("Created ticket", "ticket", ticket.Name, "order", order.Name)
	return nil
}

// getOwnedTickets returns tickets owned by the order
func (r *OrderReconciler) getOwnedTickets(ctx context.Context, order *corev1alpha1.Order) ([]corev1alpha1.Ticket, error) {
	ticketList := &corev1alpha1.TicketList{}
	listOpts := []client.ListOption{
		client.InNamespace(order.Namespace),
		client.MatchingLabels{"korder.dev/order": order.Name},
	}

	if err := r.List(ctx, ticketList, listOpts...); err != nil {
		return nil, err
	}

	return ticketList.Items, nil
}

// deleteOwnedTickets deletes all tickets owned by the order
func (r *OrderReconciler) deleteOwnedTickets(ctx context.Context, order *corev1alpha1.Order) error {
	tickets, err := r.getOwnedTickets(ctx, order)
	if err != nil {
		return err
	}

	for _, ticket := range tickets {
		if err := r.Delete(ctx, &ticket); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}

// filterActiveTickets returns tickets that are not expired or failed
func filterActiveTickets(tickets []corev1alpha1.Ticket) []corev1alpha1.Ticket {
	var active []corev1alpha1.Ticket
	for _, ticket := range tickets {
		if ticket.Status.Phase != corev1alpha1.TicketExpired {
			active = append(active, ticket)
		}
	}
	return active
}

// updateOrderStatus updates the order status with current state
func (r *OrderReconciler) updateOrderStatus(ctx context.Context, order *corev1alpha1.Order, reason, message string) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Get current tickets to calculate status
	tickets, err := r.getOwnedTickets(ctx, order)
	if err != nil {
		log.Error(err, "Failed to get owned tickets for status update")
		return ctrl.Result{}, err
	}

	// Calculate replica counts
	totalReplicas := int32(len(tickets))
	activeTickets := filterActiveTickets(tickets)
	availableReplicas := int32(len(activeTickets))

	var unavailableReplicas, terminalReplicas int32
	for _, ticket := range tickets {
		switch ticket.Status.Phase {
		case corev1alpha1.TicketExpired:
			terminalReplicas++
		case corev1alpha1.TicketPending:
			unavailableReplicas++
		}
	}

	// Update status
	order.Status.Replicas = &totalReplicas
	order.Status.AvailableReplicas = &availableReplicas
	order.Status.UnavailableReplicas = &unavailableReplicas
	order.Status.TerminalReplicas = &terminalReplicas
	order.Status.UpdatedReplicas = &availableReplicas
	order.Status.ObservedGeneration = &order.Generation

	// Update conditions
	now := metav1.Now()
	conditionType := corev1alpha1.OrderReady
	conditionStatus := corev1.ConditionTrue

	if reason == "Failed" {
		conditionType = corev1alpha1.OrderFailure
		conditionStatus = corev1.ConditionTrue
	}

	// Find existing condition or create new one
	var existingCondition *corev1alpha1.OrderCondition
	for i := range order.Status.Conditions {
		if order.Status.Conditions[i].Type == conditionType {
			existingCondition = &order.Status.Conditions[i]
			break
		}
	}

	if existingCondition == nil {
		order.Status.Conditions = append(order.Status.Conditions, corev1alpha1.OrderCondition{
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

	if err := r.Status().Update(ctx, order); err != nil {
		log.Error(err, "Failed to update order status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *OrderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.Order{}).
		Owns(&corev1alpha1.Ticket{}).
		Named("order").
		Complete(r)
}
