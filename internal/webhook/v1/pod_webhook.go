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
	return ctrl.NewWebhookManagedBy(mgr).For(&corev1.Pod{}).
		WithValidator(&PodCustomValidator{Client: mgr.GetClient()}).
		WithDefaulter(&PodCustomDefaulter{Client: mgr.GetClient()}).
		Complete()
}

const (
	// TicketRequestAnnotation is used to request a specific ticket
	TicketRequestAnnotation = "korder.dev/ticket-request"
	// OrderRequestAnnotation is used to request resources from a specific order
	OrderRequestAnnotation = "korder.dev/order-request"
	// TicketClaimedAnnotation is set when a pod claims a ticket
	TicketClaimedAnnotation = "korder.dev/ticket-claimed"
	// TicketNodeAnnotation is set to indicate which node the ticket is bound to
	TicketNodeAnnotation = "korder.dev/ticket-node"
)

// +kubebuilder:webhook:path=/mutate--v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,groups="",resources=pods,verbs=create;update,versions=v1,name=mpod-v1.kb.io,admissionReviewVersions=v1

// PodCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Pod when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type PodCustomDefaulter struct {
	Client client.Client
}

var _ webhook.CustomDefaulter = &PodCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Pod.
func (d *PodCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return fmt.Errorf("expected an Pod object but got %T", obj)
	}

	// Skip guardian pods and system pods
	if d.isSystemPod(pod) || d.isGuardianPod(pod) {
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

// isSystemPod checks if the pod is a system pod that should be ignored
func (d *PodCustomDefaulter) isSystemPod(pod *corev1.Pod) bool {
	systemNamespaces := []string{"kube-system", "kube-public", "kube-node-lease", "korder-system"}
	for _, ns := range systemNamespaces {
		if pod.Namespace == ns {
			return true
		}
	}
	return false
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
		return annotations[TicketRequestAnnotation] != "" ||
			annotations[OrderRequestAnnotation] != "" ||
			annotations[TicketClaimedAnnotation] != ""
	}
	return false
}

// findAvailableTicket finds an available ticket for the pod
func (d *PodCustomDefaulter) findAvailableTicket(ctx context.Context, pod *corev1.Pod) (*corev1alpha1.Ticket, error) {
	annotations := pod.GetAnnotations()
	if annotations == nil {
		return nil, nil
	}

	// Check for manual ticket claim first (already bound)
	if ticketName := annotations[TicketClaimedAnnotation]; ticketName != "" {
		return d.getManuallyClaimedTicket(ctx, pod.Namespace, ticketName)
	}

	// Check for ticket request with new format
	if ticketRequest := annotations[TicketRequestAnnotation]; ticketRequest != "" {
		return d.parseTicketRequest(ctx, pod, ticketRequest)
	}

	// Check for order-based request (legacy support)
	if orderName := annotations[OrderRequestAnnotation]; orderName != "" {
		return d.findTicketFromOrder(ctx, pod, orderName)
	}

	return nil, nil
}

// parseTicketRequest parses the ticket request annotation and finds appropriate ticket
func (d *PodCustomDefaulter) parseTicketRequest(ctx context.Context, pod *corev1.Pod, ticketRequest string) (*corev1alpha1.Ticket, error) {
	// Parse the new annotation formats:
	// "name:ticket-sample" - specific ticket by name
	// "selector: app=app-name,env=dev" - tickets matching label selector
	// "group:order-sample" - tickets from specific order

	if strings.HasPrefix(ticketRequest, "name:") {
		ticketName := strings.TrimSpace(strings.TrimPrefix(ticketRequest, "name:"))
		return d.getSpecificTicket(ctx, pod.Namespace, ticketName)
	}

	if strings.HasPrefix(ticketRequest, "selector:") {
		selectorStr := strings.TrimSpace(strings.TrimPrefix(ticketRequest, "selector:"))
		return d.findTicketBySelector(ctx, pod, selectorStr)
	}

	if strings.HasPrefix(ticketRequest, "group:") {
		orderName := strings.TrimSpace(strings.TrimPrefix(ticketRequest, "group:"))
		return d.findTicketFromOrder(ctx, pod, orderName)
	}

	// Fallback to treating it as a direct ticket name for backward compatibility
	return d.getSpecificTicket(ctx, pod.Namespace, ticketRequest)
}

// findTicketBySelector finds tickets matching the label selector
func (d *PodCustomDefaulter) findTicketBySelector(ctx context.Context, pod *corev1.Pod, selectorStr string) (*corev1alpha1.Ticket, error) {
	// Parse label selector format: "app=app-name,env=dev"
	labels := make(map[string]string)
	pairs := strings.Split(selectorStr, ",")
	for _, pair := range pairs {
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

	if err := d.Client.List(ctx, ticketList, listOpts...); err != nil {
		return nil, err
	}

	// Find the best matching available ticket
	return d.selectBestTicket(pod, ticketList.Items), nil
}

// getManuallyClaimedTicket handles manually claimed tickets
func (d *PodCustomDefaulter) getManuallyClaimedTicket(ctx context.Context, namespace, ticketName string) (*corev1alpha1.Ticket, error) {
	ticket := &corev1alpha1.Ticket{}
	if err := d.Client.Get(ctx, client.ObjectKey{Name: ticketName, Namespace: namespace}, ticket); err != nil {
		return nil, fmt.Errorf("manually claimed ticket %s not found: %w", ticketName, err)
	}

	// For manually claimed tickets, we need to validate they are in ready state
	// and then mark them as claimed without changing the pod's node assignment
	if ticket.Status.Phase != corev1alpha1.TicketReady {
		return nil, fmt.Errorf("manually claimed ticket %s is not in ready state (current: %s)", ticketName, ticket.Status.Phase)
	}

	return ticket, nil
}

// getSpecificTicket gets a specific ticket by name
func (d *PodCustomDefaulter) getSpecificTicket(ctx context.Context, namespace, ticketName string) (*corev1alpha1.Ticket, error) {
	ticket := &corev1alpha1.Ticket{}
	if err := d.Client.Get(ctx, client.ObjectKey{Name: ticketName, Namespace: namespace}, ticket); err != nil {
		return nil, err
	}

	// Check if ticket is available for claiming
	if ticket.Status.Phase == corev1alpha1.TicketReady && ticket.Status.NodeName != nil {
		return ticket, nil
	}

	return nil, fmt.Errorf("ticket %s is not available for claiming (phase: %s)", ticketName, ticket.Status.Phase)
}

// findTicketFromOrder finds an available ticket from the specified order
func (d *PodCustomDefaulter) findTicketFromOrder(ctx context.Context, pod *corev1.Pod, orderName string) (*corev1alpha1.Ticket, error) {
	// Get tickets from the specified order
	ticketList := &corev1alpha1.TicketList{}
	listOpts := []client.ListOption{
		client.InNamespace(pod.Namespace),
		client.MatchingLabels{"korder.dev/order": orderName},
	}

	if err := d.Client.List(ctx, ticketList, listOpts...); err != nil {
		return nil, err
	}

	// Find the best matching available ticket
	return d.selectBestTicket(pod, ticketList.Items), nil
}

// selectBestTicket selects the best available ticket for the pod
func (d *PodCustomDefaulter) selectBestTicket(pod *corev1.Pod, tickets []corev1alpha1.Ticket) *corev1alpha1.Ticket {
	var bestTicket *corev1alpha1.Ticket
	bestScore := -1

	for i := range tickets {
		ticket := &tickets[i]

		// Skip tickets that are not available
		if ticket.Status.Phase != corev1alpha1.TicketReady || ticket.Status.NodeName == nil {
			continue
		}

		// Calculate compatibility score
		score := d.calculateCompatibilityScore(pod, ticket)
		if score > bestScore {
			bestScore = score
			bestTicket = ticket
		}
	}

	return bestTicket
}

// calculateCompatibilityScore calculates how well a pod matches a ticket
func (d *PodCustomDefaulter) calculateCompatibilityScore(pod *corev1.Pod, ticket *corev1alpha1.Ticket) int {
	score := 0

	// Basic compatibility - if ticket has a node, it's available
	if ticket.Status.NodeName != nil {
		score += 10
	}

	// Check resource compatibility
	if d.checkResourceCompatibility(pod, ticket) {
		score += 20
	}

	// Check node selector compatibility
	if d.checkNodeSelectorCompatibility(pod, ticket) {
		score += 15
	}

	// Check affinity compatibility (simplified)
	if d.checkAffinityCompatibility(pod, ticket) {
		score += 10
	}

	return score
}

// checkResourceCompatibility checks if pod resources are compatible with ticket
func (d *PodCustomDefaulter) checkResourceCompatibility(_ *corev1.Pod, ticket *corev1alpha1.Ticket) bool {
	if ticket.Spec.Resources == nil {
		return true // No resource constraints on ticket
	}

	// This is a simplified check - in practice, you'd want more sophisticated resource matching
	// For now, just check if the ticket has resources defined
	return ticket.Spec.Resources.Requests != nil || ticket.Spec.Resources.Limits != nil
}

// checkNodeSelectorCompatibility checks node selector compatibility
func (d *PodCustomDefaulter) checkNodeSelectorCompatibility(pod *corev1.Pod, ticket *corev1alpha1.Ticket) bool {
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
func (d *PodCustomDefaulter) checkAffinityCompatibility(pod *corev1.Pod, ticket *corev1alpha1.Ticket) bool {
	// Simplified affinity check - in practice, this would be much more complex
	return true
}

// bindPodToTicket binds the pod to the ticket
func (d *PodCustomDefaulter) bindPodToTicket(ctx context.Context, pod *corev1.Pod, ticket *corev1alpha1.Ticket) error {
	// Initialize annotations if not present
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}

	// Check if this is a manual claim (pod already has ticket-claimed annotation)
	isManualClaim := pod.Annotations[TicketClaimedAnnotation] != ""

	if !isManualClaim {
		// For automatic binding, set pod node name to match ticket's node
		if ticket.Status.NodeName != nil {
			pod.Spec.NodeName = *ticket.Status.NodeName
		}
		// Clear request annotations and add claimed annotation
		delete(pod.Annotations, TicketRequestAnnotation)
		delete(pod.Annotations, OrderRequestAnnotation)
		pod.Annotations[TicketClaimedAnnotation] = ticket.Name
	}

	// Always add node annotation if ticket has a node
	if ticket.Status.NodeName != nil {
		pod.Annotations[TicketNodeAnnotation] = *ticket.Status.NodeName
	}

	// Update ticket status to claimed
	ticket.Status.Phase = corev1alpha1.TicketClaimed
	ticket.Status.ClaimedTime = &metav1.Time{Time: metav1.Now().Time}
	claimedPodName := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	ticket.Status.ClaimedPod = &claimedPodName

	if err := d.Client.Status().Update(ctx, ticket); err != nil {
		return fmt.Errorf("failed to update ticket status: %w", err)
	}

	bindingType := "automatic"
	if isManualClaim {
		bindingType = "manual"
	}
	podlog.Info("Pod bound to ticket", "pod", pod.Name, "ticket", ticket.Name, "node", ticket.Status.NodeName, "binding", bindingType)
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
	Client client.Client
}

var _ webhook.CustomValidator = &PodCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Pod.
func (v *PodCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil, fmt.Errorf("expected a Pod object but got %T", obj)
	}

	// Skip validation for system pods and guardian pods
	if v.isSystemPod(pod) || v.isGuardianPod(pod) {
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

	// Skip validation for system pods and guardian pods
	if v.isSystemPod(pod) || v.isGuardianPod(pod) {
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

// isSystemPod checks if the pod is a system pod that should be ignored
func (v *PodCustomValidator) isSystemPod(pod *corev1.Pod) bool {
	systemNamespaces := []string{"kube-system", "kube-public", "kube-node-lease", "korder-system"}
	for _, ns := range systemNamespaces {
		if pod.Namespace == ns {
			return true
		}
	}
	return false
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

	// Validate specific ticket request
	if ticketName := annotations[TicketRequestAnnotation]; ticketName != "" {
		if err := v.validateTicketExists(ctx, pod.Namespace, ticketName); err != nil {
			warnings = append(warnings, fmt.Sprintf("Requested ticket %s may not be available: %v", ticketName, err))
		}
	}

	// Validate order request
	if orderName := annotations[OrderRequestAnnotation]; orderName != "" {
		if err := v.validateOrderExists(ctx, pod.Namespace, orderName); err != nil {
			warnings = append(warnings, fmt.Sprintf("Requested order %s may not be available: %v", orderName, err))
		}
	}

	// Validate manual ticket claim
	if ticketName := annotations[TicketClaimedAnnotation]; ticketName != "" {
		if err := v.validateTicketExists(ctx, pod.Namespace, ticketName); err != nil {
			warnings = append(warnings, fmt.Sprintf("Manually claimed ticket %s may not be available: %v", ticketName, err))
		}
	}

	// Check for conflicting annotations
	conflictCount := 0
	if annotations[TicketRequestAnnotation] != "" {
		conflictCount++
	}
	if annotations[OrderRequestAnnotation] != "" {
		conflictCount++
	}
	if annotations[TicketClaimedAnnotation] != "" {
		conflictCount++
	}

	if conflictCount > 1 {
		return warnings, fmt.Errorf("pod can only have one of: ticket-request, order-request, or ticket-claimed annotations")
	}

	return warnings, nil
}

// validateTicketExists checks if the requested ticket exists and is available
func (v *PodCustomValidator) validateTicketExists(ctx context.Context, namespace, ticketName string) error {
	ticket := &corev1alpha1.Ticket{}
	if err := v.Client.Get(ctx, client.ObjectKey{Name: ticketName, Namespace: namespace}, ticket); err != nil {
		return fmt.Errorf("ticket not found: %w", err)
	}

	if ticket.Status.Phase != corev1alpha1.TicketReady {
		return fmt.Errorf("ticket is not in reserved state (current: %s)", ticket.Status.Phase)
	}

	return nil
}

// validateOrderExists checks if the requested order exists
func (v *PodCustomValidator) validateOrderExists(ctx context.Context, namespace, orderName string) error {
	order := &corev1alpha1.Order{}
	if err := v.Client.Get(ctx, client.ObjectKey{Name: orderName, Namespace: namespace}, order); err != nil {
		return fmt.Errorf("order not found: %w", err)
	}

	// Check if order is paused
	if order.Spec.Paused != nil && *order.Spec.Paused {
		return fmt.Errorf("order is paused")
	}

	return nil
}
