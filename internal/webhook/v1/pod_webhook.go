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

package v1

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
var podlog = logf.Log.WithName("pod-resource")

// SetupPodWebhookWithManager registers the webhook for Pod in the manager.
func SetupPodWebhookWithManager(mgr ctrl.Manager) error {
	ticketHelper := NewTicketHelper(mgr.GetClient())
	return ctrl.NewWebhookManagedBy(mgr).For(&corev1.Pod{}).
		WithValidator(&PodCustomValidator{Client: mgr.GetClient(), TicketHelper: ticketHelper}).
		WithDefaulter(&PodCustomDefaulter{Client: mgr.GetClient(), TicketHelper: ticketHelper}).
		Complete()
}

const (
	// TicketRequestAnnotation is used to request a specific ticket
	TicketRequestAnnotation = "korder.dev/ticket-request"
	// TicketClaimedAnnotation is set when a pod claims a ticket
	TicketClaimedAnnotation = "korder.dev/ticket-claimed"
	// TicketNodeAnnotation is set to indicate which node the ticket is bound to
	TicketNodeAnnotation = "korder.dev/ticket-node"
	// TicketGroupAnnotation is used to group tickets together
	TicketGroupAnnotation = "korder.dev/ticket-group"
)

// TicketHelper provides common ticket search and validation functionality
type TicketHelper struct {
	Client client.Client
}

// NewTicketHelper creates a new TicketHelper instance
func NewTicketHelper(c client.Client) *TicketHelper {
	return &TicketHelper{Client: c}
}

// FindTicketsByRequest finds all tickets matching the ticket request
func (h *TicketHelper) FindTicketsByRequest(ctx context.Context, pod *corev1.Pod, ticketRequest string) ([]corev1alpha1.Ticket, error) {
	// Parse the annotation formats:
	// "name:ticket-sample" - specific ticket by name
	// "selector: app=app-name,env=dev" - tickets matching label selector
	// "group:group-name" - tickets with matching korder.dev/ticket-group annotation

	if ticketName, found := strings.CutPrefix(ticketRequest, "name:"); found {
		ticketName = strings.TrimSpace(ticketName)
		if ticketName == "" {
			return nil, fmt.Errorf("empty ticket name in 'name:' format")
		}
		ticket, err := h.getSpecificTicket(ctx, pod.Namespace, ticketName)
		if err != nil {
			return nil, err
		}
		if ticket == nil {
			return []corev1alpha1.Ticket{}, nil
		}
		return []corev1alpha1.Ticket{*ticket}, nil
	}

	if selectorStr, found := strings.CutPrefix(ticketRequest, "selector:"); found {
		selectorStr = strings.TrimSpace(selectorStr)
		if selectorStr == "" {
			return nil, fmt.Errorf("empty selector in 'selector:' format")
		}
		return h.findTicketsBySelector(ctx, pod, selectorStr)
	}

	if groupName, found := strings.CutPrefix(ticketRequest, "group:"); found {
		groupName = strings.TrimSpace(groupName)
		if groupName == "" {
			return nil, fmt.Errorf("empty group name in 'group:' format")
		}
		return h.findTicketsByGroup(ctx, pod, groupName)
	}

	// Fallback: treat as direct ticket name for backward compatibility
	ticket, err := h.getSpecificTicket(ctx, pod.Namespace, ticketRequest)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return []corev1alpha1.Ticket{}, nil
	}
	return []corev1alpha1.Ticket{*ticket}, nil
}

// FindAvailableTicket finds the best available ticket for the pod
func (h *TicketHelper) FindAvailableTicket(ctx context.Context, pod *corev1.Pod, ticketRequest string) (*corev1alpha1.Ticket, error) {
	tickets, err := h.FindTicketsByRequest(ctx, pod, ticketRequest)
	if err != nil {
		return nil, err
	}
	return h.selectBestTicket(pod, tickets), nil
}

// ValidateTicketRequest validates the ticket request format
func (h *TicketHelper) ValidateTicketRequest(ticketRequest string) error {
	if ticketName, found := strings.CutPrefix(ticketRequest, "name:"); found {
		if strings.TrimSpace(ticketName) == "" {
			return fmt.Errorf("empty ticket name in 'name:' format")
		}
		return nil
	}

	if selectorStr, found := strings.CutPrefix(ticketRequest, "selector:"); found {
		selectorStr = strings.TrimSpace(selectorStr)
		if selectorStr == "" {
			return fmt.Errorf("empty selector in 'selector:' format")
		}
		return h.validateSelectorFormat(selectorStr)
	}

	if groupName, found := strings.CutPrefix(ticketRequest, "group:"); found {
		if strings.TrimSpace(groupName) == "" {
			return fmt.Errorf("empty group name in 'group:' format")
		}
		return nil
	}

	// Fallback: treat as direct ticket name - always valid format
	return nil
}

// ValidateClaimedTicketBelongsToRequest validates that claimed ticket belongs to the ticket request
func (h *TicketHelper) ValidateClaimedTicketBelongsToRequest(ctx context.Context, pod *corev1.Pod, ticketClaimed, ticketRequest string) error {
	// Find all tickets matching the request
	matchingTickets, err := h.FindTicketsByRequest(ctx, pod, ticketRequest)
	if err != nil {
		return err
	}

	// Check if claimed ticket is in the matching set
	for _, ticket := range matchingTickets {
		if ticket.Name == ticketClaimed {
			return nil // Found match
		}
	}

	return fmt.Errorf("claimed ticket %s does not match ticket-request %s", ticketClaimed, ticketRequest)
}

// getSpecificTicket gets a specific ticket by name
func (h *TicketHelper) getSpecificTicket(ctx context.Context, namespace, ticketName string) (*corev1alpha1.Ticket, error) {
	ticket := &corev1alpha1.Ticket{}
	if err := h.Client.Get(ctx, client.ObjectKey{Name: ticketName, Namespace: namespace}, ticket); err != nil {
		return nil, err
	}

	// Check if ticket is available for claiming
	if ticket.Status.Phase == corev1alpha1.TicketReady && ticket.Status.NodeName != nil {
		return ticket, nil
	}

	return nil, fmt.Errorf("ticket %s is not available for claiming (phase: %s)", ticketName, ticket.Status.Phase)
}

// findTicketsBySelector finds tickets matching the label selector
func (h *TicketHelper) findTicketsBySelector(ctx context.Context, pod *corev1.Pod, selectorStr string) ([]corev1alpha1.Ticket, error) {
	// Parse label selector format: "app=app-name,env=dev"
	labels := make(map[string]string)
	for _, pair := range strings.Split(selectorStr, ",") {
		kv := strings.Split(strings.TrimSpace(pair), "=")
		if len(kv) == 2 {
			labels[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}

	if len(labels) == 0 {
		return nil, fmt.Errorf("invalid selector format: %s", selectorStr)
	}

	// Get tickets matching the labels
	ticketList := &corev1alpha1.TicketList{}
	listOpts := []client.ListOption{
		client.InNamespace(pod.Namespace),
		client.MatchingLabels(labels),
	}

	if err := h.Client.List(ctx, ticketList, listOpts...); err != nil {
		return nil, err
	}

	return ticketList.Items, nil
}

// findTicketsByGroup finds tickets with the specified group annotation
func (h *TicketHelper) findTicketsByGroup(ctx context.Context, pod *corev1.Pod, groupName string) ([]corev1alpha1.Ticket, error) {
	// Get all tickets in the namespace and filter by group annotation
	ticketList := &corev1alpha1.TicketList{}
	listOpts := []client.ListOption{
		client.InNamespace(pod.Namespace),
	}

	if err := h.Client.List(ctx, ticketList, listOpts...); err != nil {
		return nil, err
	}

	// Filter tickets by group annotation
	var filteredTickets []corev1alpha1.Ticket
	for _, ticket := range ticketList.Items {
		if annotations := ticket.GetAnnotations(); annotations != nil {
			if ticketGroup, exists := annotations[TicketGroupAnnotation]; exists && ticketGroup == groupName {
				filteredTickets = append(filteredTickets, ticket)
			}
		}
	}

	return filteredTickets, nil
}

// selectBestTicket selects the best available ticket for the pod
func (h *TicketHelper) selectBestTicket(pod *corev1.Pod, tickets []corev1alpha1.Ticket) *corev1alpha1.Ticket {
	var bestTicket *corev1alpha1.Ticket
	bestScore := -1

	for i := range tickets {
		ticket := &tickets[i]

		// Skip tickets that are not available
		if ticket.Status.Phase != corev1alpha1.TicketReady || ticket.Status.NodeName == nil {
			continue
		}

		// Calculate compatibility score
		score := h.calculateCompatibilityScore(pod, ticket)
		if score > bestScore {
			bestScore = score
			bestTicket = ticket
		}
	}

	return bestTicket
}

// validateSelectorFormat validates the label selector format
func (h *TicketHelper) validateSelectorFormat(selectorStr string) error {
	for _, pair := range strings.Split(selectorStr, ",") {
		kv := strings.Split(strings.TrimSpace(pair), "=")
		if len(kv) != 2 || strings.TrimSpace(kv[0]) == "" || strings.TrimSpace(kv[1]) == "" {
			return fmt.Errorf("invalid selector format: %s (expected key=value pairs separated by commas)", selectorStr)
		}
	}
	return nil
}

// calculateCompatibilityScore calculates how well a pod matches a ticket
func (h *TicketHelper) calculateCompatibilityScore(pod *corev1.Pod, ticket *corev1alpha1.Ticket) int {
	score := 0

	// Basic compatibility - if ticket has a node, it's available
	if ticket.Status.NodeName != nil {
		score += 10
	}

	// Check resource compatibility
	if h.checkResourceCompatibility(ticket) {
		score += 20
	}

	// Check node selector compatibility
	if h.checkNodeSelectorCompatibility(pod, ticket) {
		score += 15
	}

	// Check affinity compatibility (simplified)
	if h.checkAffinityCompatibility() {
		score += 10
	}

	return score
}

// checkResourceCompatibility checks if ticket has resource constraints
func (h *TicketHelper) checkResourceCompatibility(ticket *corev1alpha1.Ticket) bool {
	if ticket.Spec.Resources == nil {
		return true // No resource constraints on ticket
	}

	// This is a simplified check - in practice, you'd want more sophisticated resource matching
	// For now, just check if the ticket has resources defined
	return ticket.Spec.Resources.Requests != nil || ticket.Spec.Resources.Limits != nil
}

// checkNodeSelectorCompatibility checks node selector compatibility
func (h *TicketHelper) checkNodeSelectorCompatibility(pod *corev1.Pod, ticket *corev1alpha1.Ticket) bool {
	// If pod has node selector, check if it's compatible with ticket's node selector
	if pod.Spec.NodeSelector != nil && ticket.Spec.NodeSelector != nil {
		for key, value := range pod.Spec.NodeSelector {
			if ticketValue, exists := ticket.Spec.NodeSelector[key]; !exists || ticketValue != value {
				return false
			}
		}
	}
	return true
}

// checkAffinityCompatibility checks basic affinity compatibility
func (h *TicketHelper) checkAffinityCompatibility() bool {
	// Simplified affinity check - in practice, this would be much more complex
	return true
}

// +kubebuilder:webhook:path=/mutate--v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,groups="",resources=pods,verbs=create;update,versions=v1,name=mpod-v1.kb.io,admissionReviewVersions=v1

// PodCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Pod when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type PodCustomDefaulter struct {
	Client       client.Client
	TicketHelper *TicketHelper
}

var _ webhook.CustomDefaulter = &PodCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Pod.
func (d *PodCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return fmt.Errorf("expected an Pod object but got %T", obj)
	}

	// Skip guardian pods
	if d.isGuardianPod(pod) {
		return nil
	}

	// Check if pod requests ticket binding first (early return for performance)
	if !d.requestsTicketBinding(pod) {
		podlog.V(1).Info("Pod does not request ticket binding", "pod", pod.Name)
		return nil
	}

	podlog.Info("Processing pod for ticket binding", "pod", pod.Name, "namespace", pod.Namespace)

	// Find and bind to an available ticket
	ticket, err := d.findAvailableTicket(ctx, pod)
	if err != nil {
		podlog.Error(err, "Failed to find available ticket", "pod", pod.Name)
		// Don't fail the pod creation, let it fallback to normal scheduling
		return nil
	}

	if ticket != nil {
		// Bind pod to ticket
		if err := d.bindPodToTicket(ctx, pod, ticket); err != nil {
			podlog.Error(err, "Failed to bind pod to ticket", "pod", pod.Name, "ticket", ticket.Name)
			return nil // Allow fallback to normal scheduling
		}
		podlog.Info("Successfully bound pod to ticket", "pod", pod.Name, "ticket", ticket.Name, "node", ticket.Status.NodeName)
	} else {
		podlog.Info("No available ticket found, pod will use normal scheduling", "pod", pod.Name)
	}

	return nil
}

// isGuardianPod checks if the pod is a guardian pod
func (d *PodCustomDefaulter) isGuardianPod(pod *corev1.Pod) bool {
	if labels := pod.GetLabels(); labels != nil {
		return labels["korder.dev/guardian"] == "true"
	}
	return false
}

// requestsTicketBinding checks if the pod requests ticket binding
func (d *PodCustomDefaulter) requestsTicketBinding(pod *corev1.Pod) bool {
	if annotations := pod.GetAnnotations(); annotations != nil {
		// Check for specific ticket request, order request, or manual ticket claim
		return annotations[TicketRequestAnnotation] != ""
	}
	return false
}

// findAvailableTicket finds an available ticket for the pod
func (d *PodCustomDefaulter) findAvailableTicket(ctx context.Context, pod *corev1.Pod) (*corev1alpha1.Ticket, error) {
	annotations := pod.GetAnnotations()
	if annotations == nil {
		return nil, nil
	}
	// Check for ticket request with new format
	if ticketRequest := annotations[TicketRequestAnnotation]; ticketRequest != "" {
		return d.TicketHelper.FindAvailableTicket(ctx, pod, ticketRequest)
	}

	return nil, nil
}

// bindPodToTicket binds the pod to the ticket
func (d *PodCustomDefaulter) bindPodToTicket(ctx context.Context, pod *corev1.Pod, ticket *corev1alpha1.Ticket) error {
	// Initialize annotations if not present
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}

	// Bind pod to ticket node
	pod.Spec.NodeName = *ticket.Status.NodeName
	pod.Annotations[TicketNodeAnnotation] = *ticket.Status.NodeName

	// Set claimed annotation (keep request annotation for validation)
	pod.Annotations[TicketClaimedAnnotation] = ticket.Name

	// Update ticket status to claimed
	ticket.Status.Phase = corev1alpha1.TicketClaimed
	ticket.Status.ClaimedTime = &metav1.Time{Time: metav1.Now().Time}
	claimedPodName := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	ticket.Status.ClaimedPod = &claimedPodName

	if err := d.Client.Status().Update(ctx, ticket); err != nil {
		return fmt.Errorf("failed to update ticket status: %w", err)
	}

	podlog.Info("Pod bound to ticket", "pod", pod.Name, "ticket", ticket.Name, "node", ticket.Status.NodeName)
	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate--v1-pod,mutating=false,failurePolicy=fail,sideEffects=None,groups="",resources=pods,verbs=create;update,versions=v1,name=vpod-v1.kb.io,admissionReviewVersions=v1

// PodCustomValidator struct is responsible for validating the Pod resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type PodCustomValidator struct {
	Client       client.Client
	TicketHelper *TicketHelper
}

var _ webhook.CustomValidator = &PodCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Pod.
func (v *PodCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil, fmt.Errorf("expected a Pod object but got %T", obj)
	}

	// Skip validation for system pods and guardian pods
	if v.isGuardianPod(pod) {
		return nil, nil
	}

	podlog.V(1).Info("Validating pod creation", "pod", pod.Name, "namespace", pod.Namespace)

	// Validate ticket annotations
	if warnings, err := v.validateTicketAnnotations(ctx, pod); err != nil {
		return warnings, err
	}

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Pod.
func (v *PodCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	pod, ok := newObj.(*corev1.Pod)
	if !ok {
		return nil, fmt.Errorf("expected a Pod object for the newObj but got %T", newObj)
	}

	// Skip validation for and guardian pods
	if v.isGuardianPod(pod) {
		return nil, nil
	}

	podlog.V(1).Info("Validating pod update", "pod", pod.Name, "namespace", pod.Namespace)

	// For updates, we generally allow them unless they're changing critical ticket bindings
	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Pod.
func (v *PodCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil, fmt.Errorf("expected a Pod object but got %T", obj)
	}

	podlog.V(1).Info("Validating pod deletion", "pod", pod.Name, "namespace", pod.Namespace)

	// Allow all deletions - ticket cleanup will be handled by the ticket controller
	return nil, nil
}

// isGuardianPod checks if the pod is a guardian pod
func (v *PodCustomValidator) isGuardianPod(pod *corev1.Pod) bool {
	if labels := pod.GetLabels(); labels != nil {
		return labels["korder.dev/guardian"] == "true"
	}
	return false
}

// validateTicketAnnotations validates ticket-related annotations
func (v *PodCustomValidator) validateTicketAnnotations(ctx context.Context, pod *corev1.Pod) (admission.Warnings, error) {
	annotations := pod.GetAnnotations()
	if annotations == nil {
		return nil, nil
	}

	var warnings admission.Warnings

	ticketRequest := annotations[TicketRequestAnnotation]
	ticketClaimed := annotations[TicketClaimedAnnotation]

	// Rule 1: ticket-claimed cannot exist without ticket-request
	if ticketClaimed != "" && ticketRequest == "" {
		return warnings, fmt.Errorf("ticket-claimed annotation cannot exist without ticket-request annotation")
	}

	// Rule 2: if ticketClaimed is empty, don't check ticketRequest (meaningless)
	if ticketClaimed == "" {
		return warnings, nil
	}

	// Rule 3: only when both annotations exist, validate the relationship
	if ticketRequest != "" && ticketClaimed != "" {
		// Validate ticket-request format first
		if err := v.TicketHelper.ValidateTicketRequest(ticketRequest); err != nil {
			return warnings, err
		}

		// Validate that ticketClaimed belongs to the set of tickets matching ticketRequest
		if err := v.TicketHelper.ValidateClaimedTicketBelongsToRequest(ctx, pod, ticketClaimed, ticketRequest); err != nil {
			return warnings, err
		}
	}

	return warnings, nil
}
