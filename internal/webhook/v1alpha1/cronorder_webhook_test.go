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

var _ = Describe("CronOrder Webhook", func() {
	var (
		obj       *corev1alpha1.CronOrder
		oldObj    *corev1alpha1.CronOrder
		validator CronOrderCustomValidator
		defaulter CronOrderCustomDefaulter
		ctx       context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		obj = &corev1alpha1.CronOrder{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cronorder",
				Namespace: "default",
			},
			Spec: corev1alpha1.CronOrderSpec{
				Schedule: "0 */6 * * *", // Every 6 hours
				OrderTemplate: corev1alpha1.OrderTemplate{
					Spec: corev1alpha1.OrderSpec{
						Template: corev1alpha1.TicketTemplate{
							Spec: corev1alpha1.TicketTemplateSpec{},
						},
					},
				},
			},
		}
		oldObj = obj.DeepCopy()
		validator = CronOrderCustomValidator{}
		defaulter = CronOrderCustomDefaulter{}
	})

	Context("When creating CronOrder under Defaulting Webhook", func() {
		It("Should apply default suspend state", func() {
			obj.Spec.Suspend = nil
			err := defaulter.Default(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(obj.Spec.Suspend).NotTo(BeNil())
			Expect(*obj.Spec.Suspend).To(BeFalse())
		})

		It("Should apply default concurrency policy", func() {
			obj.Spec.ConcurrencyPolicy = ""
			err := defaulter.Default(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(obj.Spec.ConcurrencyPolicy).To(Equal(corev1alpha1.AllowConcurrent))
		})

		It("Should apply default history limits", func() {
			obj.Spec.SuccessfulJobsHistoryLimit = nil
			obj.Spec.FailedJobsHistoryLimit = nil
			err := defaulter.Default(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(obj.Spec.SuccessfulJobsHistoryLimit).NotTo(BeNil())
			Expect(*obj.Spec.SuccessfulJobsHistoryLimit).To(Equal(int32(3)))
			Expect(obj.Spec.FailedJobsHistoryLimit).NotTo(BeNil())
			Expect(*obj.Spec.FailedJobsHistoryLimit).To(Equal(int32(1)))
		})

		It("Should apply default order template values", func() {
			obj.Spec.OrderTemplate.Spec.Replicas = nil
			obj.Spec.OrderTemplate.Spec.Paused = nil
			obj.Spec.OrderTemplate.Spec.RefreshPolicy = ""
			obj.Spec.OrderTemplate.Spec.MinReadySeconds = nil
			err := defaulter.Default(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(obj.Spec.OrderTemplate.Spec.Replicas).NotTo(BeNil())
			Expect(*obj.Spec.OrderTemplate.Spec.Replicas).To(Equal(int32(1)))
			Expect(obj.Spec.OrderTemplate.Spec.Paused).NotTo(BeNil())
			Expect(*obj.Spec.OrderTemplate.Spec.Paused).To(BeFalse())
			Expect(obj.Spec.OrderTemplate.Spec.RefreshPolicy).To(Equal(corev1alpha1.OnClaimRefresh))
			Expect(obj.Spec.OrderTemplate.Spec.MinReadySeconds).NotTo(BeNil())
			Expect(*obj.Spec.OrderTemplate.Spec.MinReadySeconds).To(Equal(int32(0)))
		})

		It("Should apply default ticket template values", func() {
			obj.Spec.OrderTemplate.Spec.Template.Spec.Window = nil
			obj.Spec.OrderTemplate.Spec.Template.Spec.Lifecycle = nil
			obj.Spec.OrderTemplate.Spec.Template.Spec.SchedulerName = nil
			err := defaulter.Default(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(obj.Spec.OrderTemplate.Spec.Template.Spec.Window).NotTo(BeNil())
			Expect(obj.Spec.OrderTemplate.Spec.Template.Spec.Window.Duration).To(Equal("24h"))
			Expect(obj.Spec.OrderTemplate.Spec.Template.Spec.Lifecycle).NotTo(BeNil())
			Expect(obj.Spec.OrderTemplate.Spec.Template.Spec.Lifecycle.CleanupPolicy).To(Equal(corev1alpha1.DeleteCleanup))
			Expect(obj.Spec.OrderTemplate.Spec.Template.Spec.SchedulerName).NotTo(BeNil())
			Expect(*obj.Spec.OrderTemplate.Spec.Template.Spec.SchedulerName).To(Equal("default-scheduler"))
		})

		It("Should set default labels", func() {
			obj.UID = "test-uid"
			err := defaulter.Default(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(obj.Spec.OrderTemplate.Labels).NotTo(BeNil())
			Expect(obj.Spec.OrderTemplate.Labels["korder.dev/cronorder"]).To(Equal("test-cronorder"))
			Expect(obj.Spec.OrderTemplate.Labels["korder.dev/cronorder-uid"]).To(Equal("test-uid"))
		})
	})

	Context("When creating CronOrder under Validating Webhook", func() {
		It("Should accept valid cron schedule", func() {
			obj.Spec.Schedule = "0 2 * * *"
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("Should reject empty schedule", func() {
			obj.Spec.Schedule = ""
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("schedule cannot be empty"))
			Expect(warnings).To(BeEmpty())
		})

		It("Should reject invalid cron schedule", func() {
			obj.Spec.Schedule = "invalid schedule"
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid cron schedule format"))
			Expect(warnings).To(BeEmpty())
		})

		It("Should reject invalid timezone", func() {
			timezone := "Invalid/Timezone"
			obj.Spec.TimeZone = &timezone
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid timezone"))
			Expect(warnings).To(BeEmpty())
		})

		It("Should accept valid timezone", func() {
			timezone := "America/New_York"
			obj.Spec.TimeZone = &timezone
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("Should reject negative starting deadline seconds", func() {
			startingDeadline := int64(-1)
			obj.Spec.StartingDeadlineSeconds = &startingDeadline
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("startingDeadlineSeconds cannot be negative"))
			Expect(warnings).To(BeEmpty())
		})

		It("Should warn about very short starting deadline", func() {
			startingDeadline := int64(5)
			obj.Spec.StartingDeadlineSeconds = &startingDeadline
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(HaveLen(1))
			Expect(warnings[0]).To(ContainSubstring("Very short starting deadline"))
		})

		It("Should reject negative history limits", func() {
			successfulLimit := int32(-1)
			obj.Spec.SuccessfulJobsHistoryLimit = &successfulLimit
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("successfulJobsHistoryLimit cannot be negative"))
			Expect(warnings).To(BeEmpty())
		})

		It("Should warn about high history limits", func() {
			successfulLimit := int32(40)
			failedLimit := int32(15)
			obj.Spec.SuccessfulJobsHistoryLimit = &successfulLimit
			obj.Spec.FailedJobsHistoryLimit = &failedLimit
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(HaveLen(1))
			Expect(warnings[0]).To(ContainSubstring("High history limits"))
		})

		It("Should reject negative replicas in order template", func() {
			replicas := int32(-1)
			obj.Spec.OrderTemplate.Spec.Replicas = &replicas
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("replicas cannot be negative"))
			Expect(warnings).To(BeEmpty())
		})

		It("Should reject invalid window duration in ticket template", func() {
			obj.Spec.OrderTemplate.Spec.Template.Spec.Window = &corev1alpha1.WindowSpec{
				Duration: "invalid-duration",
			}
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid window duration format"))
			Expect(warnings).To(BeEmpty())
		})

		It("Should reject resource requests exceeding limits", func() {
			obj.Spec.OrderTemplate.Spec.Template.Spec.Resources = &corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("2"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("1"),
				},
			}
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("resource request for cpu exceeds limit"))
			Expect(warnings).To(BeEmpty())
		})
	})

	Context("When updating CronOrder under Validating Webhook", func() {
		It("Should warn when changing schedule", func() {
			oldObj.Spec.Schedule = "0 2 * * *"
			obj.Spec.Schedule = "0 4 * * *"
			warnings, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(HaveLen(1))
			Expect(warnings[0]).To(ContainSubstring("Changing schedule"))
		})

		It("Should warn when changing timezone", func() {
			oldTz := "America/New_York"
			newTz := "Europe/London"
			oldObj.Spec.TimeZone = &oldTz
			obj.Spec.TimeZone = &newTz
			warnings, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(HaveLen(1))
			Expect(warnings[0]).To(ContainSubstring("Changing timezone"))
		})

		It("Should validate the new specification", func() {
			obj.Spec.Schedule = "invalid schedule"
			warnings, err := validator.ValidateUpdate(ctx, oldObj, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid cron schedule format"))
			Expect(warnings).To(BeEmpty())
		})
	})

	Context("When deleting CronOrder under Validating Webhook", func() {
		It("Should allow deletion", func() {
			warnings, err := validator.ValidateDelete(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})
	})
})
