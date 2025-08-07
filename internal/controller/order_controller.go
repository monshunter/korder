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
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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
	var desiredReplicas int32
	if order.Spec.DaemonSet != nil && *order.Spec.DaemonSet {
		// DaemonSet mode: calculate replicas based on eligible nodes
		eligibleNodes, err := r.getEligibleNodes(ctx, order)
		if err != nil {
			log.Error(err, "Failed to get eligible nodes for DaemonSet order")
			return ctrl.Result{}, err
		}
		desiredReplicas = int32(len(eligibleNodes))
		log.Info("DaemonSet mode: calculated desired replicas based on eligible nodes",
			"eligibleNodes", len(eligibleNodes), "desiredReplicas", desiredReplicas)
	} else {
		// Normal mode: use specified replicas
		desiredReplicas = int32(1)
		if order.Spec.Replicas != nil {
			desiredReplicas = *order.Spec.Replicas
		}
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

// getEligibleNodes returns nodes that are eligible for DaemonSet tickets based on nodeSelector and tolerations
func (r *OrderReconciler) getEligibleNodes(ctx context.Context, order *corev1alpha1.Order) ([]corev1.Node, error) {
	log := logf.FromContext(ctx)

	// Get all nodes in the cluster
	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList); err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	var eligibleNodes []corev1.Node

	for _, node := range nodeList.Items {
		// Skip nodes that are not ready or schedulable
		if !r.isNodeReady(&node) || !r.isNodeSchedulable(&node) {
			log.V(1).Info("Skipping node: not ready or not schedulable", "node", node.Name)
			continue
		}

		// Check if node matches nodeSelector from ticket template
		if !r.nodeMatchesSelector(&node, order.Spec.Template.Spec.NodeSelector) {
			log.V(1).Info("Skipping node: does not match nodeSelector", "node", node.Name)
			continue
		}

		// Check if tolerations allow scheduling on this node
		if !r.nodeToleratesTicket(&node, order.Spec.Template.Spec.Tolerations) {
			log.V(1).Info("Skipping node: tolerations do not allow scheduling", "node", node.Name)
			continue
		}

		eligibleNodes = append(eligibleNodes, node)
		log.V(1).Info("Node is eligible for DaemonSet ticket", "node", node.Name)
	}

	log.Info("Found eligible nodes for DaemonSet order", "total", len(nodeList.Items), "eligible", len(eligibleNodes))
	return eligibleNodes, nil
}

// isNodeReady checks if a node is in Ready condition
func (r *OrderReconciler) isNodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// isNodeSchedulable checks if a node is schedulable (not cordoned)
func (r *OrderReconciler) isNodeSchedulable(node *corev1.Node) bool {
	return !node.Spec.Unschedulable
}

// nodeMatchesSelector checks if a node matches the given nodeSelector
func (r *OrderReconciler) nodeMatchesSelector(node *corev1.Node, nodeSelector map[string]string) bool {
	if nodeSelector == nil {
		return true
	}

	for key, value := range nodeSelector {
		if nodeValue, exists := node.Labels[key]; !exists || nodeValue != value {
			return false
		}
	}
	return true
}

// nodeToleratesTicket checks if the ticket's tolerations allow it to be scheduled on the node
func (r *OrderReconciler) nodeToleratesTicket(node *corev1.Node, tolerations []corev1.Toleration) bool {
	// If no tolerations specified, only schedule on nodes without taints
	if len(tolerations) == 0 {
		return len(node.Spec.Taints) == 0
	}

	// Check each taint on the node
	for _, taint := range node.Spec.Taints {
		tolerated := false
		for _, toleration := range tolerations {
			if r.tolerationToleratesTaint(&toleration, &taint) {
				tolerated = true
				break
			}
		}
		if !tolerated {
			return false
		}
	}
	return true
}

// tolerationToleratesTaint checks if a toleration tolerates a specific taint
func (r *OrderReconciler) tolerationToleratesTaint(toleration *corev1.Toleration, taint *corev1.Taint) bool {
	// Handle operator Exists
	if toleration.Operator == corev1.TolerationOpExists {
		// If key is empty, tolerate all taints
		if toleration.Key == "" {
			return true
		}
		// If key matches and effect matches (or effect is empty), tolerate
		return toleration.Key == taint.Key && (toleration.Effect == "" || toleration.Effect == taint.Effect)
	}

	// Handle operator Equal (default)
	return toleration.Key == taint.Key &&
		toleration.Value == taint.Value &&
		(toleration.Effect == "" || toleration.Effect == taint.Effect)
}

// findOrdersForNode returns reconcile requests for DaemonSet orders that might be affected by node changes
func (r *OrderReconciler) findOrdersForNode(ctx context.Context, obj client.Object) []reconcile.Request {
	node, ok := obj.(*corev1.Node)
	if !ok {
		return nil
	}

	log := logf.FromContext(ctx)
	log.V(1).Info("Node changed, finding affected DaemonSet orders", "node", node.Name)

	// Get all orders in all namespaces
	orderList := &corev1alpha1.OrderList{}
	if err := r.List(ctx, orderList); err != nil {
		log.Error(err, "Failed to list orders for node change")
		return nil
	}

	var requests []reconcile.Request
	for _, order := range orderList.Items {
		// Only consider DaemonSet orders
		if order.Spec.DaemonSet != nil && *order.Spec.DaemonSet {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      order.Name,
					Namespace: order.Namespace,
				},
			})
			log.V(1).Info("Enqueuing DaemonSet order for reconciliation due to node change",
				"order", order.Name, "namespace", order.Namespace, "node", node.Name)
		}
	}

	log.V(1).Info("Found DaemonSet orders affected by node change", "count", len(requests), "node", node.Name)
	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *OrderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.Order{}).
		Owns(&corev1alpha1.Ticket{}).
		Watches(
			&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.findOrdersForNode),
		).
		Named("order").
		Complete(r)
}
