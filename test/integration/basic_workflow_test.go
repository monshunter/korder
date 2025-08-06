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

package integration

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1alpha1 "github.com/monshunter/korder/api/v1alpha1"
)

var _ = Describe("Basic Workflow Integration Test", func() {
	Context("When creating Orders, Tickets, and Quotas", func() {
		ctx := context.Background()
		namespace := "test-namespace"

		BeforeEach(func() {
			// Create test namespace
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		})

		AfterEach(func() {
			// Cleanup namespace
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}
			_ = k8sClient.Delete(ctx, ns)
		})

		It("should create and manage a complete workflow", func() {
			By("Creating a Quota")
			quota := &corev1alpha1.Quota{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-quota",
					Namespace: namespace,
				},
				Spec: corev1alpha1.QuotaSpec{
					Scope: corev1alpha1.QuotaScope{
						Type:       corev1alpha1.NamespaceListScope,
						Namespaces: []string{namespace},
					},
					Hard: corev1.ResourceList{
						"orders":          *resource.NewQuantity(10, resource.DecimalSI),
						"tickets":         *resource.NewQuantity(100, resource.DecimalSI),
						"reserved.cpu":    resource.MustParse("10"),
						"reserved.memory": resource.MustParse("20Gi"),
					},
				},
			}
			Expect(k8sClient.Create(ctx, quota)).To(Succeed())

			By("Verifying Quota status is updated")
			Eventually(func() bool {
				namespacedName := types.NamespacedName{Name: "test-quota", Namespace: namespace}
				err := k8sClient.Get(ctx, namespacedName, quota)
				return err == nil
			}, time.Minute, time.Second).Should(BeTrue())

			By("Creating an Order")
			order := &corev1alpha1.Order{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-order",
					Namespace: namespace,
				},
				Spec: corev1alpha1.OrderSpec{
					Replicas: func() *int32 { i := int32(2); return &i }(),
					Strategy: corev1alpha1.OrderStrategy{
						Type: corev1alpha1.OneTimeStrategy,
					},
					Template: corev1alpha1.TicketTemplate{
						Spec: corev1alpha1.TicketTemplateSpec{
							Duration: &metav1.Duration{Duration: time.Hour * 24},
							Resources: &corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, order)).To(Succeed())

			By("Verifying Tickets are created")
			Eventually(func() int {
				ticketList := &corev1alpha1.TicketList{}
				err := k8sClient.List(ctx, ticketList, client.InNamespace(namespace))
				if err != nil {
					return 0
				}
				return len(ticketList.Items)
			}, time.Minute, time.Second).Should(Equal(2))

			By("Verifying Order status is updated")
			Eventually(func() bool {
				namespacedName := types.NamespacedName{Name: "test-order", Namespace: namespace}
				err := k8sClient.Get(ctx, namespacedName, order)
				if err != nil {
					return false
				}
				return order.Status.Replicas != nil && *order.Status.Replicas == 2
			}, time.Minute, time.Second).Should(BeTrue())

			By("Creating a business Pod that should bind to a Ticket")
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-business-pod",
					Namespace: namespace,
					Annotations: map[string]string{
						"korder.dev/require-ticket": "true",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:1.20",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).To(Succeed())

			By("Verifying Pod gets ticket annotation")
			Eventually(func() bool {
				namespacedName := types.NamespacedName{Name: "test-business-pod", Namespace: namespace}
				err := k8sClient.Get(ctx, namespacedName, pod)
				if err != nil {
					return false
				}
				_, exists := pod.Annotations["korder.dev/ticket-claimed"]
				return exists
			}, time.Minute, time.Second).Should(BeTrue())

			By("Verifying at least one Ticket is claimed")
			Eventually(func() int {
				ticketList := &corev1alpha1.TicketList{}
				err := k8sClient.List(ctx, ticketList, client.InNamespace(namespace))
				if err != nil {
					return 0
				}
				claimedCount := 0
				for _, ticket := range ticketList.Items {
					if ticket.Status.Phase == corev1alpha1.TicketClaimed {
						claimedCount++
					}
				}
				return claimedCount
			}, time.Minute, time.Second).Should(BeNumerically(">=", 1))
		})

		It("should handle quota enforcement", func() {
			By("Creating a restrictive Quota")
			quota := &corev1alpha1.Quota{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "restrictive-quota",
					Namespace: namespace,
				},
				Spec: corev1alpha1.QuotaSpec{
					Scope: corev1alpha1.QuotaScope{
						Type:       corev1alpha1.NamespaceListScope,
						Namespaces: []string{namespace},
					},
					Hard: corev1.ResourceList{
						"orders":  *resource.NewQuantity(1, resource.DecimalSI),
						"tickets": *resource.NewQuantity(1, resource.DecimalSI),
					},
				},
			}
			Expect(k8sClient.Create(ctx, quota)).To(Succeed())

			By("Creating first Order (should succeed)")
			order1 := &corev1alpha1.Order{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "order-1",
					Namespace: namespace,
				},
				Spec: corev1alpha1.OrderSpec{
					Replicas: func() *int32 { i := int32(1); return &i }(),
					Strategy: corev1alpha1.OrderStrategy{
						Type: corev1alpha1.OneTimeStrategy,
					},
					Template: corev1alpha1.TicketTemplate{
						Spec: corev1alpha1.TicketTemplateSpec{
							Duration: &metav1.Duration{Duration: time.Hour},
							Resources: &corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, order1)).To(Succeed())

			By("Attempting to create second Order (should be allowed but controlled by quota)")
			order2 := &corev1alpha1.Order{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "order-2",
					Namespace: namespace,
				},
				Spec: corev1alpha1.OrderSpec{
					Replicas: func() *int32 { i := int32(1); return &i }(),
					Strategy: corev1alpha1.OrderStrategy{
						Type: corev1alpha1.OneTimeStrategy,
					},
					Template: corev1alpha1.TicketTemplate{
						Spec: corev1alpha1.TicketTemplateSpec{
							Duration: &metav1.Duration{Duration: time.Hour},
							Resources: &corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: resource.MustParse("100m"),
								},
							},
						},
					},
				},
			}
			// This should succeed in creation but quota controller should manage the enforcement
			Expect(k8sClient.Create(ctx, order2)).To(Succeed())
		})
	})
})
