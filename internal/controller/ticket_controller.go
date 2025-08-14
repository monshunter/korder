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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	corev1alpha1 "github.com/monshunter/korder/api/v1alpha1"
	"github.com/monshunter/korder/internal/metrics"
)

// TicketReconciler reconciles a Ticket object
type TicketReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core.korder.dev,resources=tickets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core.korder.dev,resources=tickets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core.korder.dev,resources=tickets/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

const (
	TicketFinalizerName = "korder.dev/ticket-finalizer"
	GuardianPodPrefix   = "korder-guardian"
)

// Reconcile manages guardian pods and ticket lifecycle
func (r *TicketReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime).Seconds()
		metrics.ObserveReconciliationDuration("ticket", "success", duration)
	}()

	// Fetch the Ticket instance
	ticket := &corev1alpha1.Ticket{}
	if err := r.Get(ctx, req.NamespacedName, ticket); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Ticket resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Ticket")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if ticket.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, ticket)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(ticket, TicketFinalizerName) {
		controllerutil.AddFinalizer(ticket, TicketFinalizerName)
		if err := r.Update(ctx, ticket); err != nil {
			log.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Check if ticket is expired
	if r.isTicketExpired(ticket) {
		return r.handleExpiration(ctx, ticket)
	}

	// Handle ticket phases
	switch ticket.Status.Phase {
	case corev1alpha1.TicketPending, "": // Empty phase means new ticket
		return r.handlePendingTicket(ctx, ticket)
	case corev1alpha1.TicketReady:
		return r.handleReadyTicket(ctx, ticket)
	case corev1alpha1.TicketClaimed:
		return r.handleClaimedTicket(ctx, ticket)
	case corev1alpha1.TicketExpired:
		return r.handleExpiredTicket(ctx, ticket)
	default:
		log.Info("Unknown ticket phase", "phase", ticket.Status.Phase)
		return ctrl.Result{}, nil
	}
}

// handleDeletion handles the cleanup when a ticket is deleted
func (r *TicketReconciler) handleDeletion(ctx context.Context, ticket *corev1alpha1.Ticket) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Clean up guardian pod
	if err := r.deleteGuardianPod(ctx, ticket); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to delete guardian pod")
		return ctrl.Result{Requeue: true}, err
	}

	// Remove finalizer
	controllerutil.RemoveFinalizer(ticket, TicketFinalizerName)
	if err := r.Update(ctx, ticket); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to remove finalizer")
		return ctrl.Result{Requeue: true}, err
	}

	log.Info("Ticket deleted successfully")
	return ctrl.Result{}, nil
}

// handlePendingTicket activates a pending ticket and creates guardian pod
func (r *TicketReconciler) handlePendingTicket(ctx context.Context, ticket *corev1alpha1.Ticket) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Set activation time if not set
	now := metav1.Now()
	if ticket.Status.ActivationTime == nil {
		ticket.Status.ActivationTime = &now
	}

	// Calculate expiration time from window spec
	if ticket.Status.ExpirationTime == nil && ticket.Spec.Window != nil && ticket.Spec.Window.Duration != "" {
		duration, err := time.ParseDuration(ticket.Spec.Window.Duration)
		if err != nil {
			log.Error(err, "Failed to parse window duration", "duration", ticket.Spec.Window.Duration)
			return r.updateTicketStatus(ctx, ticket, corev1alpha1.TicketPending, "Failed", fmt.Sprintf("Invalid window duration: %v", err))
		}
		expirationTime := metav1.NewTime(ticket.Status.ActivationTime.Add(duration))
		ticket.Status.ExpirationTime = &expirationTime
	}

	// Create guardian pod
	pod, err := r.createGuardianPod(ctx, ticket)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			return r.handleReadyTicket(ctx, ticket)
		}
		log.Error(err, "Failed to create guardian pod")
		return r.updateTicketStatus(ctx, ticket, corev1alpha1.TicketPending, "Failed", fmt.Sprintf("Failed to create guardian pod: %v", err))
	}

	// Update ticket status
	ticket.Status.Phase = corev1alpha1.TicketReady
	guardianPodName := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	ticket.Status.GuardianPod = &guardianPodName
	ticket.Status.NodeName = &pod.Spec.NodeName

	log.Info("Ticket activated", "ticket", ticket.Name, "pod", pod.Name)
	return r.updateTicketStatus(ctx, ticket, corev1alpha1.TicketReady, "Activated", "Guardian pod created and ticket ready")
}

// handleReadyTicket manages a ready ticket
func (r *TicketReconciler) handleReadyTicket(ctx context.Context, ticket *corev1alpha1.Ticket) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Check if guardian pod still exists and is running
	if ticket.Status.GuardianPod != nil {
		// Parse namespace/name format
		parts := strings.Split(*ticket.Status.GuardianPod, "/")
		if len(parts) != 2 {
			log.Error(nil, "Invalid guardian pod format", "guardianPod", *ticket.Status.GuardianPod)
			return r.handlePendingTicket(ctx, ticket)
		}

		pod := &corev1.Pod{}
		podKey := types.NamespacedName{
			Name:      parts[1],
			Namespace: parts[0],
		}

		if err := r.Get(ctx, podKey, pod); err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("Guardian pod not found, recreating", "guardianPod", *ticket.Status.GuardianPod)
				return r.handlePendingTicket(ctx, ticket)
			}
			return ctrl.Result{}, err
		}

		// Update node name if pod is scheduled
		if pod.Spec.NodeName != "" && (ticket.Status.NodeName == nil || *ticket.Status.NodeName != pod.Spec.NodeName) {
			ticket.Status.NodeName = &pod.Spec.NodeName
		}

		// Check if pod is running
		if pod.Status.Phase == corev1.PodRunning {
			return r.updateTicketStatus(ctx, ticket, corev1alpha1.TicketReady, "Ready", "Guardian pod is running and ticket is ready")
		}
	}

	// Requeue to check status again
	return ctrl.Result{RequeueAfter: time.Second * 30}, nil
}

// handleClaimedTicket manages a claimed ticket
func (r *TicketReconciler) handleClaimedTicket(ctx context.Context, ticket *corev1alpha1.Ticket) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Delete guardian pod when ticket is claimed
	if err := r.deleteGuardianPod(ctx, ticket); err != nil {
		log.Error(err, "Failed to delete guardian pod for claimed ticket")
		return ctrl.Result{}, err
	}

	// Clear guardian pod reference since it's deleted
	ticket.Status.GuardianPod = nil

	log.Info("Guardian pod deleted for claimed ticket", "ticket", ticket.Name)
	return r.updateTicketStatus(ctx, ticket, corev1alpha1.TicketClaimed, "Claimed", "Ticket claimed and guardian pod deleted")
}

// handleExpiredTicket manages an expired ticket
func (r *TicketReconciler) handleExpiredTicket(ctx context.Context, ticket *corev1alpha1.Ticket) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Delete guardian pod
	if err := r.deleteGuardianPod(ctx, ticket); err != nil {
		log.Error(err, "Failed to delete guardian pod for expired ticket")
		return ctrl.Result{}, err
	}

	// Handle cleanup policy
	if ticket.Spec.Lifecycle != nil && ticket.Spec.Lifecycle.CleanupPolicy == corev1alpha1.DeleteCleanup {
		if ticket.Spec.Lifecycle.TTLSecondsAfterFinished != nil {
			// Schedule deletion after TTL
			deleteTime := metav1.NewTime(time.Now().Add(time.Duration(*ticket.Spec.Lifecycle.TTLSecondsAfterFinished) * time.Second))
			if time.Now().After(deleteTime.Time) {
				if err := r.Delete(ctx, ticket); err != nil && !apierrors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
				log.Info("Ticket deleted after TTL", "ticket", ticket.Name)
				return ctrl.Result{}, nil
			}
			return ctrl.Result{RequeueAfter: time.Until(deleteTime.Time)}, nil
		} else {
			// Delete immediately
			if err := r.Delete(ctx, ticket); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
			log.Info("Ticket deleted immediately", "ticket", ticket.Name)
			return ctrl.Result{}, nil
		}
	}

	log.Info("Expired ticket retained", "ticket", ticket.Name)
	return r.updateTicketStatus(ctx, ticket, corev1alpha1.TicketExpired, "Expired", "Ticket expired and guardian pod deleted")
}

// handleExpiration checks and handles ticket expiration
func (r *TicketReconciler) handleExpiration(ctx context.Context, ticket *corev1alpha1.Ticket) (ctrl.Result, error) {
	if ticket.Status.Phase != corev1alpha1.TicketExpired {
		ticket.Status.Phase = corev1alpha1.TicketExpired
		return r.updateTicketStatus(ctx, ticket, corev1alpha1.TicketExpired, "Expired", "Ticket duration exceeded")
	}
	return r.handleExpiredTicket(ctx, ticket)
}

// isTicketExpired checks if the ticket has expired
func (r *TicketReconciler) isTicketExpired(ticket *corev1alpha1.Ticket) bool {
	if ticket.Status.ExpirationTime == nil {
		return false
	}
	return time.Now().After(ticket.Status.ExpirationTime.Time)
}

// createGuardianPod creates a guardian pod for the ticket
func (r *TicketReconciler) createGuardianPod(ctx context.Context, ticket *corev1alpha1.Ticket) (*corev1.Pod, error) {
	guardianName := fmt.Sprintf("%s-%s", GuardianPodPrefix, ticket.Name)

	// Default resource requests if not specified
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("100m"),
			corev1.ResourceMemory: resource.MustParse("128Mi"),
		},
	}
	if ticket.Spec.Resources != nil {
		resources = *ticket.Spec.Resources
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      guardianName,
			Namespace: ticket.Namespace,
			Labels: map[string]string{
				"korder.dev/ticket":     ticket.Name,
				"korder.dev/ticket-uid": string(ticket.UID),
				"korder.dev/guardian":   "true",
				"korder.dev/component":  "guardian-pod",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyAlways,
			Containers: []corev1.Container{
				{
					Name:      "guardian",
					Image:     "registry.k8s.io/pause:3.10",
					Resources: resources,
				},
			},
			NodeSelector:              ticket.Spec.NodeSelector,
			Affinity:                  ticket.Spec.Affinity,
			Tolerations:               ticket.Spec.Tolerations,
			TopologySpreadConstraints: ticket.Spec.TopologySpreadConstraints,
		},
	}

	// Set node name if specified
	if ticket.Spec.NodeName != nil {
		pod.Spec.NodeName = *ticket.Spec.NodeName
	}

	// Set scheduler name if specified
	if ticket.Spec.SchedulerName != nil {
		pod.Spec.SchedulerName = *ticket.Spec.SchedulerName
	}

	// Set priority class if specified
	if ticket.Spec.PriorityClassName != nil {
		pod.Spec.PriorityClassName = *ticket.Spec.PriorityClassName
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(ticket, pod, r.Scheme); err != nil {
		return nil, err
	}

	if err := r.Create(ctx, pod); err != nil {
		return nil, err
	}

	return pod, nil
}

// deleteGuardianPod deletes the guardian pod associated with the ticket
func (r *TicketReconciler) deleteGuardianPod(ctx context.Context, ticket *corev1alpha1.Ticket) error {
	if ticket.Status.GuardianPod == nil {
		return nil // No pod to delete
	}

	// Parse namespace/name format
	parts := strings.Split(*ticket.Status.GuardianPod, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid guardian pod format: %s", *ticket.Status.GuardianPod)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      parts[1],
			Namespace: parts[0],
		},
	}

	if err := r.Delete(ctx, pod); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	return nil
}

// updateTicketStatus updates the ticket status
func (r *TicketReconciler) updateTicketStatus(ctx context.Context, ticket *corev1alpha1.Ticket, phase corev1alpha1.TicketPhase, reason, message string) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	ticket.Status.Phase = phase
	ticket.Status.Reason = &reason
	ticket.Status.Message = &message
	ticket.Status.ObservedGeneration = &ticket.Generation

	// Update conditions
	now := metav1.Now()
	conditionType := corev1alpha1.TicketReadyCondition
	conditionStatus := corev1.ConditionTrue

	switch phase {
	case corev1alpha1.TicketPending:
		conditionStatus = corev1.ConditionFalse
	case corev1alpha1.TicketExpired:
		conditionType = corev1alpha1.TicketExpiring
		conditionStatus = corev1.ConditionTrue
	}

	// Find existing condition or create new one
	var existingCondition *corev1alpha1.TicketCondition
	for i := range ticket.Status.Conditions {
		if ticket.Status.Conditions[i].Type == conditionType {
			existingCondition = &ticket.Status.Conditions[i]
			break
		}
	}

	if existingCondition == nil {
		ticket.Status.Conditions = append(ticket.Status.Conditions, corev1alpha1.TicketCondition{
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

	if err := r.Status().Update(ctx, ticket); err != nil {
		log.Error(err, "Failed to update ticket status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TicketReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1alpha1.Ticket{}).
		Owns(&corev1.Pod{}).
		Named("ticket").
		Complete(r)
}
