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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// OrderMetrics tracks order lifecycle events
	OrdersTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "korder_orders_total",
			Help: "Total number of orders created",
		},
		[]string{"namespace", "strategy"},
	)

	OrdersActive = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "korder_orders_active",
			Help: "Number of currently active orders",
		},
		[]string{"namespace", "strategy"},
	)

	// TicketMetrics tracks ticket lifecycle events
	TicketsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "korder_tickets_total",
			Help: "Total number of tickets created",
		},
		[]string{"namespace", "phase"},
	)

	TicketsActive = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "korder_tickets_active",
			Help: "Number of currently active tickets",
		},
		[]string{"namespace", "phase"},
	)

	GuardianPodsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "korder_guardian_pods_total",
			Help: "Total number of guardian pods created",
		},
		[]string{"namespace"},
	)

	// QuotaMetrics tracks quota usage
	QuotaUsage = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "korder_quota_usage_ratio",
			Help: "Current quota usage as a ratio (0-1)",
		},
		[]string{"quota_name", "resource_type"},
	)

	QuotaLimits = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "korder_quota_limits",
			Help: "Current quota limits",
		},
		[]string{"quota_name", "resource_type"},
	)

	// ResourceMetrics tracks resource allocation
	ReservedResources = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "korder_reserved_resources",
			Help: "Currently reserved resources",
		},
		[]string{"namespace", "resource_type", "unit"},
	)

	ClaimedResources = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "korder_claimed_resources",
			Help: "Currently claimed resources",
		},
		[]string{"namespace", "resource_type", "unit"},
	)

	// Performance metrics
	ReconciliationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "korder_reconciliation_duration_seconds",
			Help:    "Time spent reconciling resources",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"controller", "result"},
	)

	WebhookDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "korder_webhook_duration_seconds",
			Help:    "Time spent processing webhook requests",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"webhook", "operation", "result"},
	)

	// Error metrics
	ReconciliationErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "korder_reconciliation_errors_total",
			Help: "Total number of reconciliation errors",
		},
		[]string{"controller", "error_type"},
	)

	WebhookErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "korder_webhook_errors_total",
			Help: "Total number of webhook errors",
		},
		[]string{"webhook", "operation", "error_type"},
	)
)

// RegisterMetrics registers all custom metrics with the global prometheus registry
func RegisterMetrics() {
	metrics.Registry.MustRegister(
		OrdersTotal,
		OrdersActive,
		TicketsTotal,
		TicketsActive,
		GuardianPodsTotal,
		QuotaUsage,
		QuotaLimits,
		ReservedResources,
		ClaimedResources,
		ReconciliationDuration,
		WebhookDuration,
		ReconciliationErrors,
		WebhookErrors,
	)
}

// IncOrdersTotal increments the total orders counter
func IncOrdersTotal(namespace, strategy string) {
	OrdersTotal.WithLabelValues(namespace, strategy).Inc()
}

// SetOrdersActive sets the active orders gauge
func SetOrdersActive(namespace, strategy string, count float64) {
	OrdersActive.WithLabelValues(namespace, strategy).Set(count)
}

// IncTicketsTotal increments the total tickets counter
func IncTicketsTotal(namespace, phase string) {
	TicketsTotal.WithLabelValues(namespace, phase).Inc()
}

// SetTicketsActive sets the active tickets gauge
func SetTicketsActive(namespace, phase string, count float64) {
	TicketsActive.WithLabelValues(namespace, phase).Set(count)
}

// IncGuardianPodsTotal increments the total guardian pods counter
func IncGuardianPodsTotal(namespace string) {
	GuardianPodsTotal.WithLabelValues(namespace).Inc()
}

// SetQuotaUsage sets the quota usage ratio
func SetQuotaUsage(quotaName, resourceType string, ratio float64) {
	QuotaUsage.WithLabelValues(quotaName, resourceType).Set(ratio)
}

// SetQuotaLimits sets the quota limits
func SetQuotaLimits(quotaName, resourceType string, limit float64) {
	QuotaLimits.WithLabelValues(quotaName, resourceType).Set(limit)
}

// SetReservedResources sets the reserved resources gauge
func SetReservedResources(namespace, resourceType, unit string, amount float64) {
	ReservedResources.WithLabelValues(namespace, resourceType, unit).Set(amount)
}

// SetClaimedResources sets the claimed resources gauge
func SetClaimedResources(namespace, resourceType, unit string, amount float64) {
	ClaimedResources.WithLabelValues(namespace, resourceType, unit).Set(amount)
}

// ObserveReconciliationDuration observes reconciliation duration
func ObserveReconciliationDuration(controller, result string, duration float64) {
	ReconciliationDuration.WithLabelValues(controller, result).Observe(duration)
}

// ObserveWebhookDuration observes webhook duration
func ObserveWebhookDuration(webhook, operation, result string, duration float64) {
	WebhookDuration.WithLabelValues(webhook, operation, result).Observe(duration)
}

// IncReconciliationErrors increments reconciliation errors counter
func IncReconciliationErrors(controller, errorType string) {
	ReconciliationErrors.WithLabelValues(controller, errorType).Inc()
}

// IncWebhookErrors increments webhook errors counter
func IncWebhookErrors(webhook, operation, errorType string) {
	WebhookErrors.WithLabelValues(webhook, operation, errorType).Inc()
}
