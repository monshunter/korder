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
var orderlog = logf.Log.WithName("order-resource")

// SetupOrderWebhookWithManager registers the webhook for Order in the manager.
func SetupOrderWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&corev1alpha1.Order{}).
		WithValidator(&OrderCustomValidator{}).
		WithDefaulter(&OrderCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-core-korder-dev-v1alpha1-order,mutating=true,failurePolicy=fail,sideEffects=None,groups=core.korder.dev,resources=orders,verbs=create;update,versions=v1alpha1,name=morder-v1alpha1.kb.io,admissionReviewVersions=v1

// OrderCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Order when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type OrderCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &OrderCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Order.
func (d *OrderCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	order, ok := obj.(*corev1alpha1.Order)

	if !ok {
		return fmt.Errorf("expected an Order object but got %T", obj)
	}
	orderlog.Info("Defaulting for Order", "name", order.GetName())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-core-korder-dev-v1alpha1-order,mutating=false,failurePolicy=fail,sideEffects=None,groups=core.korder.dev,resources=orders,verbs=create;update,versions=v1alpha1,name=vorder-v1alpha1.kb.io,admissionReviewVersions=v1

// OrderCustomValidator struct is responsible for validating the Order resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type OrderCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &OrderCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Order.
func (v *OrderCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	order, ok := obj.(*corev1alpha1.Order)
	if !ok {
		return nil, fmt.Errorf("expected a Order object but got %T", obj)
	}
	orderlog.Info("Validation for Order upon creation", "name", order.GetName())

	// TODO(user): fill in your validation logic upon object creation.

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Order.
func (v *OrderCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	order, ok := newObj.(*corev1alpha1.Order)
	if !ok {
		return nil, fmt.Errorf("expected a Order object for the newObj but got %T", newObj)
	}
	orderlog.Info("Validation for Order upon update", "name", order.GetName())

	// TODO(user): fill in your validation logic upon object update.

	return nil, nil
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Order.
func (v *OrderCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	order, ok := obj.(*corev1alpha1.Order)
	if !ok {
		return nil, fmt.Errorf("expected a Order object but got %T", obj)
	}
	orderlog.Info("Validation for Order upon deletion", "name", order.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
