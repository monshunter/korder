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
)

// OrderReconciler reconciles a Order object
type OrderReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.korder.dev,resources=orders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.korder.dev,resources=orders/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.korder.dev,resources=orders/finalizers,verbs=update
// +kubebuilder:rbac:groups=core.korder.dev,resources=tickets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

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

// reconcileTickets manages ticket creation and lifecycle based on desired replicas
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

	// Filter active tickets (not expired)
	activeTickets := filterActiveTickets(currentTickets)
	currentCount := int32(len(activeTickets))

	log.V(1).Info("Reconciling tickets", "desired", desiredReplicas, "current", currentCount)

	if currentCount < desiredReplicas {
		// Create missing tickets
		needed := desiredReplicas - currentCount
		for i := int32(0); i < needed; i++ {
			if err := r.createTicket(ctx, order, int32(len(currentTickets))+i); err != nil {
				log.Error(err, "Failed to create ticket")
				return ctrl.Result{}, err
			}
		}
		log.Info("Created tickets", "created", needed)
	} else if currentCount > desiredReplicas {
		// Handle refresh policy for excess tickets
		if err := r.handleExcessTickets(ctx, order, activeTickets, desiredReplicas); err != nil {
			log.Error(err, "Failed to handle excess tickets")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
}

// handleExcessTickets handles tickets when current count exceeds desired count
func (r *OrderReconciler) handleExcessTickets(ctx context.Context, order *corev1alpha1.Order, activeTickets []corev1alpha1.Ticket, desiredReplicas int32) error {
	log := logf.FromContext(ctx)

	excess := int32(len(activeTickets)) - desiredReplicas
	if excess <= 0 {
		return nil
	}

	// Handle based on refresh policy
	switch order.Spec.RefreshPolicy {
	case corev1alpha1.AlwaysRefresh:
		// Delete excess tickets to maintain desired count
		for i := int32(0); i < excess; i++ {
			ticket := &activeTickets[len(activeTickets)-1-int(i)]
			if err := r.Delete(ctx, ticket); err != nil {
				log.Error(err, "Failed to delete excess ticket", "ticket", ticket.Name)
				return err
			}
			log.Info("Deleted excess ticket", "ticket", ticket.Name)
		}
	case corev1alpha1.OnClaimRefresh:
		// Only delete claimed tickets that are excess
		deletedCount := int32(0)
		for i := len(activeTickets) - 1; i >= 0 && deletedCount < excess; i-- {
			ticket := &activeTickets[i]
			if ticket.Status.Phase == corev1alpha1.TicketClaimed {
				if err := r.Delete(ctx, ticket); err != nil {
					log.Error(err, "Failed to delete claimed ticket", "ticket", ticket.Name)
					return err
				}
				log.Info("Deleted claimed ticket", "ticket", ticket.Name)
				deletedCount++
			}
		}
	case corev1alpha1.NeverRefresh:
		// Don't delete any tickets, just log the situation
		log.Info("Excess tickets detected but refresh policy is Never", "excess", excess)
	}

	return nil
}

// Removed old strategy handlers - using simple declarative approach

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
			Window:                    order.Spec.Template.Spec.Window,
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

// Removed DaemonSet-related methods - now handled by DaemonOrder controller

// SetupWithManager sets up the controller with the Manager.
func (r *OrderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.Order{}).
		Owns(&corev1alpha1.Ticket{}).
		Named("order").
		Complete(r)
}
