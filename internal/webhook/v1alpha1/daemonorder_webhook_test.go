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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	corev1alpha1 "github.com/monshunter/korder/api/v1alpha1"
)

var _ = Describe("DaemonOrder Webhook", func() {
	var (
		obj       *corev1alpha1.DaemonOrder
		oldObj    *corev1alpha1.DaemonOrder
		validator DaemonOrderCustomValidator
		defaulter DaemonOrderCustomDefaulter
		ctx       context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		obj = &corev1alpha1.DaemonOrder{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-daemonorder",
				Namespace: "default",
			},
			Spec: corev1alpha1.DaemonOrderSpec{
				Template: corev1alpha1.TicketTemplate{
					Spec: corev1alpha1.TicketTemplateSpec{},
				},
			},
		}
		oldObj = obj.DeepCopy()
		validator = DaemonOrderCustomValidator{}
		defaulter = DaemonOrderCustomDefaulter{}
	})

	Context("When creating DaemonOrder under Defaulting Webhook", func() {
		It("Should apply default paused state", func() {
			obj.Spec.Paused = nil
			err := defaulter.Default(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(obj.Spec.Paused).NotTo(BeNil())
			Expect(*obj.Spec.Paused).To(BeFalse())
		})

		It("Should apply default refresh policy", func() {
			obj.Spec.RefreshPolicy = ""
			err := defaulter.Default(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(obj.Spec.RefreshPolicy).To(Equal(corev1alpha1.OnClaimRefresh))
		})

		It("Should apply default ticket template values", func() {
			obj.Spec.Template.Spec.Window = nil
			obj.Spec.Template.Spec.Lifecycle = nil
			obj.Spec.Template.Spec.SchedulerName = nil
			err := defaulter.Default(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(obj.Spec.Template.Spec.Window).NotTo(BeNil())
			Expect(obj.Spec.Template.Spec.Window.Duration).To(Equal("24h"))
			Expect(obj.Spec.Template.Spec.Lifecycle).NotTo(BeNil())
			Expect(obj.Spec.Template.Spec.Lifecycle.CleanupPolicy).To(Equal(corev1alpha1.DeleteCleanup))
			Expect(obj.Spec.Template.Spec.SchedulerName).NotTo(BeNil())
			Expect(*obj.Spec.Template.Spec.SchedulerName).To(Equal("default-scheduler"))
		})

		It("Should apply default TTL when cleanup policy is Delete", func() {
			obj.Spec.Template.Spec.Lifecycle = &corev1alpha1.TicketLifecycle{
				CleanupPolicy: corev1alpha1.DeleteCleanup,
			}
			err := defaulter.Default(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(obj.Spec.Template.Spec.Lifecycle.TTLSecondsAfterFinished).NotTo(BeNil())
			Expect(*obj.Spec.Template.Spec.Lifecycle.TTLSecondsAfterFinished).To(Equal(int32(3600)))
		})

		It("Should set default labels", func() {
			obj.UID = "test-uid"
			err := defaulter.Default(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(obj.Spec.Template.Labels).NotTo(BeNil())
			Expect(obj.Spec.Template.Labels["korder.dev/daemonorder"]).To(Equal("test-daemonorder"))
			Expect(obj.Spec.Template.Labels["korder.dev/daemonorder-uid"]).To(Equal("test-uid"))
		})
	})

	Context("When creating DaemonOrder under Validating Webhook", func() {
		It("Should accept valid daemon order", func() {
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("Should reject invalid window duration in ticket template", func() {
			obj.Spec.Template.Spec.Window = &corev1alpha1.WindowSpec{
				Duration: "invalid-duration",
			}
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid window duration format"))
			Expect(warnings).To(BeEmpty())
		})

		It("Should reject invalid start time format", func() {
			startTime := "invalid-time"
			obj.Spec.Template.Spec.Window = &corev1alpha1.WindowSpec{
				Duration:  "1h",
				StartTime: &startTime,
			}
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid start time format"))
			Expect(warnings).To(BeEmpty())
		})

		It("Should accept valid RFC3339 start time", func() {
			startTime := "2023-01-01T10:00:00Z"
			obj.Spec.Template.Spec.Window = &corev1alpha1.WindowSpec{
				Duration:  "1h",
				StartTime: &startTime,
			}
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("Should reject negative TTL seconds", func() {
			ttl := int32(-1)
			obj.Spec.Template.Spec.Lifecycle = &corev1alpha1.TicketLifecycle{
				TTLSecondsAfterFinished: &ttl,
			}
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("TTL seconds cannot be negative"))
			Expect(warnings).To(BeEmpty())
		})

		It("Should reject resource requests exceeding limits", func() {
			obj.Spec.Template.Spec.Resources = &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("2Gi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			}
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("resource request for memory exceeds limit"))
			Expect(warnings).To(BeEmpty())
		})

		It("Should reject empty selector when specified", func() {
			obj.Spec.Selector = &metav1.LabelSelector{}
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("selector cannot be empty when specified"))
			Expect(warnings).To(BeEmpty())
		})

		It("Should accept valid selector", func() {
			obj.Spec.Selector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test",
				},
			}
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("Should reject empty node selector when specified", func() {
			obj.Spec.Template.Spec.NodeSelector = map[string]string{}
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("node selector cannot be empty when specified"))
			Expect(warnings).To(BeEmpty())
		})

		It("Should accept valid node selector", func() {
			obj.Spec.Template.Spec.NodeSelector = map[string]string{
				"kubernetes.io/os": "linux",
			}
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})
	})

	Context("When updating DaemonOrder under Validating Webhook", func() {
		It("Should warn when changing selector", func() {
			oldObj.Spec.Selector = &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "old"},
			}
			obj.Spec.Selector = &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "new"},
			}
			warnings, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(HaveLen(1))
			Expect(warnings[0]).To(ContainSubstring("Changing selector may affect existing tickets"))
		})

		It("Should warn when changing refresh policy", func() {
			oldObj.Spec.RefreshPolicy = corev1alpha1.OnClaimRefresh
			obj.Spec.RefreshPolicy = corev1alpha1.AlwaysRefresh
			warnings, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(HaveLen(1))
			Expect(warnings[0]).To(ContainSubstring("Changing refresh policy"))
		})

		It("Should warn when changing node selector", func() {
			oldObj.Spec.Template.Spec.NodeSelector = map[string]string{"kubernetes.io/os": "linux"}
			obj.Spec.Template.Spec.NodeSelector = map[string]string{"kubernetes.io/os": "windows"}
			warnings, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(HaveLen(1))
			Expect(warnings[0]).To(ContainSubstring("Changing node selector may affect ticket placement"))
		})

		It("Should validate the new specification", func() {
			obj.Spec.Template.Spec.Window = &corev1alpha1.WindowSpec{
				Duration: "invalid-duration",
			}
			warnings, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid window duration format"))
			Expect(warnings).To(BeEmpty())
		})
	})

	Context("When deleting DaemonOrder under Validating Webhook", func() {
		It("Should allow deletion", func() {
			warnings, err := validator.ValidateDelete(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})
	})
})
