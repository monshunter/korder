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
)

// DaemonOrderReconciler reconciles a DaemonOrder object
type DaemonOrderReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.korder.dev,resources=daemonorders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.korder.dev,resources=daemonorders/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.korder.dev,resources=daemonorders/finalizers,verbs=update
// +kubebuilder:rbac:groups=core.korder.dev,resources=tickets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch

const (
	DaemonOrderFinalizerName = "korder.dev/daemonorder-finalizer"
)

// Reconcile manages the lifecycle of tickets based on daemon order specifications
func (r *DaemonOrderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime).Seconds()
		metrics.ObserveReconciliationDuration("daemonorder", "success", duration)
	}()

	// Fetch the DaemonOrder instance
	daemonOrder := &corev1alpha1.DaemonOrder{}
	if err := r.Get(ctx, req.NamespacedName, daemonOrder); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("DaemonOrder resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get DaemonOrder")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if daemonOrder.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, daemonOrder)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(daemonOrder, DaemonOrderFinalizerName) {
		controllerutil.AddFinalizer(daemonOrder, DaemonOrderFinalizerName)
		if err := r.Update(ctx, daemonOrder); err != nil {
			log.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Handle paused daemon orders
	if daemonOrder.Spec.Paused != nil && *daemonOrder.Spec.Paused {
		log.Info("DaemonOrder is paused, skipping reconciliation")
		return r.updateDaemonOrderStatus(ctx, daemonOrder, "Paused", "DaemonOrder is paused")
	}

	// Reconcile tickets based on eligible nodes
	result, err := r.reconcileTickets(ctx, daemonOrder)
	if err != nil {
		log.Error(err, "Failed to reconcile tickets")
		if _, updateErr := r.updateDaemonOrderStatus(ctx, daemonOrder, "Failed", fmt.Sprintf("Failed to reconcile tickets: %v", err)); updateErr != nil {
			log.Error(updateErr, "Failed to update daemon order status after reconcile error")
		}
		return ctrl.Result{}, err
	}

	// Update daemon order status
	if _, err := r.updateDaemonOrderStatus(ctx, daemonOrder, "Ready", "DaemonOrder is processing normally"); err != nil {
		return ctrl.Result{}, err
	}

	return result, nil
}

// handleDeletion handles the cleanup when a daemon order is deleted
func (r *DaemonOrderReconciler) handleDeletion(ctx context.Context, daemonOrder *corev1alpha1.DaemonOrder) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Clean up owned tickets
	if err := r.deleteOwnedTickets(ctx, daemonOrder); err != nil {
		log.Error(err, "Failed to delete owned tickets")
		return ctrl.Result{}, err
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(daemonOrder, DaemonOrderFinalizerName)
	if err := r.Update(ctx, daemonOrder); err != nil {
		log.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	log.Info("DaemonOrder deleted successfully")
	return ctrl.Result{}, nil
}

// reconcileTickets manages ticket creation for daemon orders (one per eligible node)
func (r *DaemonOrderReconciler) reconcileTickets(ctx context.Context, daemonOrder *corev1alpha1.DaemonOrder) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Get eligible nodes
	eligibleNodes, err := r.getEligibleNodes(ctx, daemonOrder)
	if err != nil {
		log.Error(err, "Failed to get eligible nodes")
		return ctrl.Result{}, err
	}

	// Get current tickets owned by this daemon order
	currentTickets, err := r.getOwnedTickets(ctx, daemonOrder)
	if err != nil {
		return ctrl.Result{}, err
	}

	log.V(1).Info("Reconciling daemon order tickets", "eligibleNodes", len(eligibleNodes), "currentTickets", len(currentTickets))

	// Create tickets for nodes that don't have them
	for _, node := range eligibleNodes {
		hasTicket := false
		for _, ticket := range currentTickets {
			if ticket.Status.NodeName != nil && *ticket.Status.NodeName == node.Name {
				hasTicket = true
				break
			}
		}

		if !hasTicket {
			if err := r.createTicketForNode(ctx, daemonOrder, &node); err != nil {
				log.Error(err, "Failed to create ticket for node", "node", node.Name)
				return ctrl.Result{}, err
			}
			log.Info("Created ticket for node", "node", node.Name)
		}
	}

	// Handle refresh policy for misscheduled or excess tickets
	if err := r.handleMisscheduledTickets(ctx, daemonOrder, currentTickets, eligibleNodes); err != nil {
		log.Error(err, "Failed to handle misscheduled tickets")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
}

// getEligibleNodes returns nodes that are eligible for daemon order tickets
func (r *DaemonOrderReconciler) getEligibleNodes(ctx context.Context, daemonOrder *corev1alpha1.DaemonOrder) ([]corev1.Node, error) {
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
		if !r.nodeMatchesSelector(&node, daemonOrder.Spec.Template.Spec.NodeSelector) {
			log.V(1).Info("Skipping node: does not match nodeSelector", "node", node.Name)
			continue
		}

		// Check if tolerations allow scheduling on this node
		if !r.nodeToleratesTicket(&node, daemonOrder.Spec.Template.Spec.Tolerations) {
			log.V(1).Info("Skipping node: tolerations do not allow scheduling", "node", node.Name)
			continue
		}

		eligibleNodes = append(eligibleNodes, node)
		log.V(1).Info("Node is eligible for daemon order ticket", "node", node.Name)
	}

	log.Info("Found eligible nodes for daemon order", "total", len(nodeList.Items), "eligible", len(eligibleNodes))
	return eligibleNodes, nil
}

// createTicketForNode creates a ticket for a specific node
func (r *DaemonOrderReconciler) createTicketForNode(ctx context.Context, daemonOrder *corev1alpha1.DaemonOrder, node *corev1.Node) error {
	log := logf.FromContext(ctx)

	ticket := &corev1alpha1.Ticket{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", daemonOrder.Name, node.Name),
			Namespace: daemonOrder.Namespace,
			Labels:    make(map[string]string),
		},
		Spec: corev1alpha1.TicketSpec{
			Lifecycle:                 daemonOrder.Spec.Template.Spec.Lifecycle,
			Window:                    daemonOrder.Spec.Template.Spec.Window,
			SchedulerName:             daemonOrder.Spec.Template.Spec.SchedulerName,
			PriorityClassName:         daemonOrder.Spec.Template.Spec.PriorityClassName,
			Resources:                 daemonOrder.Spec.Template.Spec.Resources,
			NodeName:                  &node.Name, // Pin to specific node
			NodeSelector:              daemonOrder.Spec.Template.Spec.NodeSelector,
			Affinity:                  daemonOrder.Spec.Template.Spec.Affinity,
			Tolerations:               daemonOrder.Spec.Template.Spec.Tolerations,
			TopologySpreadConstraints: daemonOrder.Spec.Template.Spec.TopologySpreadConstraints,
		},
	}

	// Copy labels from template
	for k, v := range daemonOrder.Spec.Template.Labels {
		ticket.Labels[k] = v
	}

	// Add daemon order-specific labels
	ticket.Labels["korder.dev/daemon-order"] = daemonOrder.Name
	ticket.Labels["korder.dev/daemon-order-uid"] = string(daemonOrder.UID)
	ticket.Labels["korder.dev/node"] = node.Name

	// Set owner reference
	if err := controllerutil.SetControllerReference(daemonOrder, ticket, r.Scheme); err != nil {
		return err
	}

	if err := r.Create(ctx, ticket); err != nil {
		log.Error(err, "Failed to create ticket", "ticket", ticket.Name, "node", node.Name)
		metrics.IncReconciliationErrors("daemonorder", "ticket_creation_failed")
		return err
	}

	// Update metrics
	metrics.IncTicketsTotal(daemonOrder.Namespace, string(corev1alpha1.TicketPending))

	log.Info("Created ticket for node", "ticket", ticket.Name, "node", node.Name, "daemonOrder", daemonOrder.Name)
	return nil
}

// handleMisscheduledTickets handles tickets that shouldn't exist or need to be refreshed
func (r *DaemonOrderReconciler) handleMisscheduledTickets(ctx context.Context, daemonOrder *corev1alpha1.DaemonOrder, currentTickets []corev1alpha1.Ticket, eligibleNodes []corev1.Node) error {
	log := logf.FromContext(ctx)

	// Create a map of eligible node names for quick lookup
	eligibleNodeMap := make(map[string]bool)
	for _, node := range eligibleNodes {
		eligibleNodeMap[node.Name] = true
	}

	// Handle tickets based on refresh policy
	for _, ticket := range currentTickets {
		shouldDelete := false

		// Check if ticket is on a node that's no longer eligible
		if ticket.Status.NodeName != nil && !eligibleNodeMap[*ticket.Status.NodeName] {
			log.Info("Ticket on ineligible node", "ticket", ticket.Name, "node", *ticket.Status.NodeName)
			shouldDelete = true
		}

		// Handle refresh policy
		if !shouldDelete {
			switch daemonOrder.Spec.RefreshPolicy {
			case corev1alpha1.AlwaysRefresh:
				// Always delete and recreate
				shouldDelete = true
			case corev1alpha1.OnClaimRefresh:
				// Only delete claimed tickets
				if ticket.Status.Phase == corev1alpha1.TicketClaimed {
					shouldDelete = true
				}
			case corev1alpha1.NeverRefresh:
				// Don't delete unless misscheduled
				// (already handled above)
			}
		}

		if shouldDelete {
			if err := r.Delete(ctx, &ticket); err != nil && !apierrors.IsNotFound(err) {
				log.Error(err, "Failed to delete ticket", "ticket", ticket.Name)
				return err
			}
			log.Info("Deleted ticket", "ticket", ticket.Name, "reason", "refresh_policy")
		}
	}

	return nil
}

// getOwnedTickets returns tickets owned by the daemon order
func (r *DaemonOrderReconciler) getOwnedTickets(ctx context.Context, daemonOrder *corev1alpha1.DaemonOrder) ([]corev1alpha1.Ticket, error) {
	ticketList := &corev1alpha1.TicketList{}
	listOpts := []client.ListOption{
		client.InNamespace(daemonOrder.Namespace),
		client.MatchingLabels{"korder.dev/daemon-order": daemonOrder.Name},
	}

	if err := r.List(ctx, ticketList, listOpts...); err != nil {
		return nil, err
	}

	return ticketList.Items, nil
}

// deleteOwnedTickets deletes all tickets owned by the daemon order
func (r *DaemonOrderReconciler) deleteOwnedTickets(ctx context.Context, daemonOrder *corev1alpha1.DaemonOrder) error {
	tickets, err := r.getOwnedTickets(ctx, daemonOrder)
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

// updateDaemonOrderStatus updates the daemon order status with current state
func (r *DaemonOrderReconciler) updateDaemonOrderStatus(ctx context.Context, daemonOrder *corev1alpha1.DaemonOrder, reason, message string) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Get current tickets to calculate status
	tickets, err := r.getOwnedTickets(ctx, daemonOrder)
	if err != nil {
		log.Error(err, "Failed to get owned tickets for status update")
		return ctrl.Result{}, err
	}

	// Get eligible nodes
	eligibleNodes, err := r.getEligibleNodes(ctx, daemonOrder)
	if err != nil {
		log.Error(err, "Failed to get eligible nodes for status update")
		return ctrl.Result{}, err
	}

	// Calculate daemon set status fields
	desiredNumberScheduled := int32(len(eligibleNodes))
	currentNumberScheduled := int32(0)
	numberReady := int32(0)
	numberAvailable := int32(0)
	numberUnavailable := int32(0)
	numberMisscheduled := int32(0)

	// Create maps for tracking
	eligibleNodeMap := make(map[string]bool)
	for _, node := range eligibleNodes {
		eligibleNodeMap[node.Name] = true
	}

	nodeTicketMap := make(map[string]bool)
	for _, ticket := range tickets {
		if ticket.Status.NodeName != nil {
			nodeName := *ticket.Status.NodeName
			nodeTicketMap[nodeName] = true

			// Check if ticket is on an eligible node
			if eligibleNodeMap[nodeName] {
				currentNumberScheduled++
				switch ticket.Status.Phase {
				case corev1alpha1.TicketReady:
					numberReady++
					numberAvailable++
				case corev1alpha1.TicketClaimed:
					numberAvailable++
				case corev1alpha1.TicketPending:
					numberUnavailable++
				}
			} else {
				numberMisscheduled++
			}
		}
	}

	// Update status
	daemonOrder.Status.DesiredNumberScheduled = &desiredNumberScheduled
	daemonOrder.Status.CurrentNumberScheduled = &currentNumberScheduled
	daemonOrder.Status.NumberReady = &numberReady
	daemonOrder.Status.NumberAvailable = &numberAvailable
	daemonOrder.Status.NumberUnavailable = &numberUnavailable
	daemonOrder.Status.NumberMisscheduled = &numberMisscheduled
	daemonOrder.Status.UpdatedNumberScheduled = &currentNumberScheduled // For simplicity, assume all are updated
	daemonOrder.Status.ObservedGeneration = &daemonOrder.Generation

	// Update conditions
	now := metav1.Now()
	conditionType := corev1alpha1.DaemonOrderReady
	conditionStatus := metav1.ConditionTrue

	if reason == "Failed" {
		conditionType = corev1alpha1.DaemonOrderFailure
		conditionStatus = metav1.ConditionTrue
	}

	// Find existing condition or create new one
	var existingCondition *corev1alpha1.DaemonOrderCondition
	for i := range daemonOrder.Status.Conditions {
		if daemonOrder.Status.Conditions[i].Type == conditionType {
			existingCondition = &daemonOrder.Status.Conditions[i]
			break
		}
	}

	if existingCondition == nil {
		daemonOrder.Status.Conditions = append(daemonOrder.Status.Conditions, corev1alpha1.DaemonOrderCondition{
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

	if err := r.Status().Update(ctx, daemonOrder); err != nil {
		log.Error(err, "Failed to update daemon order status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// Node helper methods (similar to what was in order controller)
func (r *DaemonOrderReconciler) isNodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func (r *DaemonOrderReconciler) isNodeSchedulable(node *corev1.Node) bool {
	return !node.Spec.Unschedulable
}

func (r *DaemonOrderReconciler) nodeMatchesSelector(node *corev1.Node, nodeSelector map[string]string) bool {
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

func (r *DaemonOrderReconciler) nodeToleratesTicket(node *corev1.Node, tolerations []corev1.Toleration) bool {
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

func (r *DaemonOrderReconciler) tolerationToleratesTaint(toleration *corev1.Toleration, taint *corev1.Taint) bool {
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

// findDaemonOrdersForNode returns reconcile requests for daemon orders that might be affected by node changes
func (r *DaemonOrderReconciler) findDaemonOrdersForNode(ctx context.Context, obj client.Object) []reconcile.Request {
	node, ok := obj.(*corev1.Node)
	if !ok {
		return nil
	}

	log := logf.FromContext(ctx)
	log.V(1).Info("Node changed, finding affected daemon orders", "node", node.Name)

	// Get all daemon orders in all namespaces
	daemonOrderList := &corev1alpha1.DaemonOrderList{}
	if err := r.List(ctx, daemonOrderList); err != nil {
		log.Error(err, "Failed to list daemon orders for node change")
		return nil
	}

	var requests []reconcile.Request
	for _, daemonOrder := range daemonOrderList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      daemonOrder.Name,
				Namespace: daemonOrder.Namespace,
			},
		})
		log.V(1).Info("Enqueuing daemon order for reconciliation due to node change",
			"daemonOrder", daemonOrder.Name, "namespace", daemonOrder.Namespace, "node", node.Name)
	}

	log.V(1).Info("Found daemon orders affected by node change", "count", len(requests), "node", node.Name)
	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *DaemonOrderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.DaemonOrder{}).
		Owns(&corev1alpha1.Ticket{}).
		Watches(
			&corev1.Node{},
			handler.EnqueueRequestsFromMapFunc(r.findDaemonOrdersForNode),
		).
		Named("daemonorder").
		Complete(r)
}
