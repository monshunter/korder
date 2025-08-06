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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	corev1alpha1 "github.com/monshunter/korder/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var ticketlog = logf.Log.WithName("ticket-resource")

// SetupTicketWebhookWithManager registers the webhook for Ticket in the manager.
func SetupTicketWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&corev1alpha1.Ticket{}).
		WithValidator(&TicketCustomValidator{}).
		WithDefaulter(&TicketCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-core-korder-dev-v1alpha1-ticket,mutating=true,failurePolicy=fail,sideEffects=None,groups=core.korder.dev,resources=tickets,verbs=create;update,versions=v1alpha1,name=mticket-v1alpha1.kb.io,admissionReviewVersions=v1

// TicketCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Ticket when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type TicketCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &TicketCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Ticket.
func (d *TicketCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	ticket, ok := obj.(*corev1alpha1.Ticket)

	if !ok {
		return fmt.Errorf("expected an Ticket object but got %T", obj)
	}
	ticketlog.Info("Defaulting for Ticket", "name", ticket.GetName())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-core-korder-dev-v1alpha1-ticket,mutating=false,failurePolicy=fail,sideEffects=None,groups=core.korder.dev,resources=tickets,verbs=create;update,versions=v1alpha1,name=vticket-v1alpha1.kb.io,admissionReviewVersions=v1

// TicketCustomValidator struct is responsible for validating the Ticket resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type TicketCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &TicketCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Ticket.
func (v *TicketCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	ticket, ok := obj.(*corev1alpha1.Ticket)
	if !ok {
		return nil, fmt.Errorf("expected a Ticket object but got %T", obj)
	}
	ticketlog.Info("Validation for Ticket upon creation", "name", ticket.GetName())

	// TODO(user): fill in your validation logic upon object creation.

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Ticket.
func (v *TicketCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	ticket, ok := newObj.(*corev1alpha1.Ticket)
	if !ok {
		return nil, fmt.Errorf("expected a Ticket object for the newObj but got %T", newObj)
	}
	ticketlog.Info("Validation for Ticket upon update", "name", ticket.GetName())

	// TODO(user): fill in your validation logic upon object update.

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Ticket.
func (v *TicketCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	ticket, ok := obj.(*corev1alpha1.Ticket)
	if !ok {
		return nil, fmt.Errorf("expected a Ticket object but got %T", obj)
	}
	ticketlog.Info("Validation for Ticket upon deletion", "name", ticket.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
