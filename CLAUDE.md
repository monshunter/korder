# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

korder is a declarative Kubernetes order system that guarantees resource allocation through an order and ticket mechanism. It's built using the Kubebuilder framework and implements custom Kubernetes controllers and webhooks.

## Development Commands

### Core Operations
- `make build` - Build the manager binary
- `make run` - Run the controller locally (requires valid kubeconfig)
- `make test` - Run unit tests with coverage
- `make test-e2e` - Run end-to-end tests using Kind cluster
- `make lint` - Run golangci-lint linter
- `make lint-fix` - Run linter with automatic fixes

### Code Generation
- `make generate` - Generate DeepCopy methods for API types
- `make manifests` - Generate CRDs, RBAC, and webhook configurations
- `make fmt` - Format Go code
- `make vet` - Run go vet static analysis

### Individual Testing
- `go test ./internal/controller -run TestOrder` - Run specific Order controller tests
- `go test ./internal/controller -run TestTicket` - Run specific Ticket controller tests
- `go test ./internal/webhook/v1 -run TestPodWebhook` - Run Pod webhook tests
- `go test ./internal/webhook/v1alpha1 -run TestOrderWebhook` - Run Order webhook tests

### Docker Operations  
- `make docker-build` - Build container image (default: controller:latest)
- `make docker-push` - Push container image
- `make docker-buildx` - Multi-platform build and push

### Kubernetes Deployment
- `make install` - Install CRDs to cluster
- `make uninstall` - Remove CRDs from cluster  
- `make deploy` - Deploy controller to cluster
- `make undeploy` - Remove controller from cluster
- `make build-installer` - Generate consolidated install.yaml

### Testing Infrastructure
- `make setup-test-e2e` - Create Kind cluster for e2e tests
- `make cleanup-test-e2e` - Delete Kind test cluster

## Architecture

### Core Components
- **Order Controller** (`internal/controller/order_controller.go`) - Manages Order lifecycle and creates Tickets
- **Ticket Controller** (`internal/controller/ticket_controller.go`) - Manages Ticket lifecycle and guardian pods
- **Quota Controller** (`internal/controller/quota_controller.go`) - Manages resource quotas and enforces limits across Orders and Tickets
- **Pod Webhook** (`internal/webhook/v1/pod_webhook.go`) - Intercepts pod creation to bind to tickets
- **Order/Ticket/Quota Webhooks** (`internal/webhook/v1alpha1/`) - Validation and defaulting for custom resources

### API Types
- **Order** (`api/v1alpha1/order_types.go`) - Declares resource requirements and manages ticket lifecycle
- **Ticket** (`api/v1alpha1/ticket_types.go`) - Represents individual resource reservations with guardian pods
- **Quota** (`api/v1alpha1/quota_types.go`) - Defines resource quotas and limits for Orders and Tickets across different scopes

### System Design
The system implements a resource reservation pattern where:
1. Orders define resource requirements and quantities
2. Tickets are created for each resource reservation with guardian pods to hold the resources
3. Business pods are scheduled to claimed tickets through webhook intervention
4. Guardian pods are terminated when tickets are claimed or expired
5. Quotas enforce resource limits and policies across Orders and Tickets at various scopes (cluster, namespace, or object-level)

### Configuration Structure
- `config/crd/` - Custom Resource Definitions
- `config/rbac/` - Role-based access control
- `config/webhook/` - Webhook configurations with CEL-based filtering
- `config/manager/` - Controller deployment manifests
- `config/default/` - Kustomize overlay for complete deployment

### Webhook Configuration
The Pod webhooks use CEL expressions and namespace selectors for precise filtering:
- **CEL Filtering**: Only intercepts Pods with korder annotations (`ticket-request`, `order-request`, `ticket-claimed`)
- **Namespace Filtering**: Excludes system namespaces (kube-system, kube-public, kube-node-lease, korder-system)
- **Automatic Patching**: Uses Kustomize patches in `config/webhook/pod-webhook-patch.yaml` and `validating-webhook-patch.yaml`

## Quota System

### Overview
The Quota system provides advanced resource management capabilities that extend beyond Kubernetes' native ResourceQuota. It enables fine-grained control over Order and Ticket resource allocation with support for multiple scopes, time-based quotas, and hierarchical resource distribution.

### Key Features
- **Multi-scope Support**: Cluster-wide, namespace-based, or object-selector based quotas
- **Time-based Quotas**: Different resource limits based on time windows (e.g., business hours vs off-hours)
- **Hierarchical Allocation**: Sub-quotas for different environments or teams
- **Order/Ticket Limits**: Control over the number of Orders and Tickets, not just resource quantities
- **Duration Limits**: Maximum reservation time constraints

### Quota Types and Scopes

#### Scope Types
1. **Cluster**: Single quota for the entire cluster
2. **NamespaceSelector**: Quota applied to namespaces matching label selectors
3. **NamespaceList**: Quota applied to explicitly listed namespaces
4. **ObjectSelector**: Quota applied to objects matching label selectors

#### Resource Limits
- `orders`: Maximum number of Orders allowed
- `tickets`: Maximum number of Tickets allowed
- `reserved.cpu`: Total CPU that can be reserved
- `reserved.memory`: Total memory that can be reserved
- `max-duration`: Maximum time resources can be reserved

### Configuration Examples

#### Basic Team Quota
```yaml
apiVersion: core.korder.dev/v1alpha1
kind: Quota
metadata:
  name: team-a-quota
spec:
  scope:
    type: NamespaceSelector
    namespaceSelector:
      matchLabels:
        team: "team-a"
  hard:
    orders: "20"
    tickets: "200"
    reserved.cpu: "100"
    reserved.memory: "200Gi"
    max-duration: "24h"
```

#### Advanced Quota with Time Windows
```yaml
apiVersion: core.korder.dev/v1alpha1
kind: Quota
metadata:
  name: cluster-quota-with-time-windows
spec:
  scope:
    type: Cluster
  hard:
    reserved.cpu: "1000"
    reserved.memory: "2000Gi"
  timeWindows:
    - name: "business-hours"
      schedule: "0 9-17 * * 1-5"  # Mon-Fri 9AM-5PM
      multiplier: 2.0             # Allow 2x resources during business hours
    - name: "off-hours"
      schedule: "0 18-8 * * *"    # Evenings and weekends
      multiplier: 0.5             # Limit to 50% during off-hours
```

#### Hierarchical Environment Allocation
```yaml
apiVersion: core.korder.dev/v1alpha1
kind: Quota
metadata:
  name: team-environment-quota
spec:
  scope:
    type: NamespaceList
    namespaces: ["team-a-dev", "team-a-staging", "team-a-prod"]
  hard:
    reserved.cpu: "200"
    reserved.memory: "400Gi"
  allocation:
    - namespace: "team-a-prod"
      limits:
        reserved.cpu: "120"      # 60% for production
        reserved.memory: "240Gi"
    - namespace: "team-a-staging"
      limits:
        reserved.cpu: "50"       # 25% for staging
        reserved.memory: "100Gi"
    - namespace: "team-a-dev"
      limits:
        reserved.cpu: "30"       # 15% for development
        reserved.memory: "60Gi"
```

### RBAC Configuration
The system provides three predefined roles for Quota management:

#### Quota Admin Role (`config/rbac/quota_admin_role.yaml`)
- Full permissions (`*`) over Quota resources
- Intended for cluster administrators who manage quota policies
- Can create, update, delete, and view all Quota resources

#### Quota Editor Role (`config/rbac/quota_editor_role.yaml`)
- Create, update, delete, get, list, patch, and watch Quota resources
- Cannot manage RBAC or permissions for others
- Suitable for team leads or resource managers

#### Quota Viewer Role (`config/rbac/quota_viewer_role.yaml`)
- Read-only access (get, list, watch) to Quota resources
- Ideal for monitoring and observability purposes
- Cannot modify quota configurations

### Management Commands

#### Creating Quotas
```bash
# Apply a quota configuration
kubectl apply -f config/samples/core_v1alpha1_quota.yaml

# Create from the example documentation
kubectl apply -f docs/quota.yaml
```

#### Monitoring Quotas
```bash
# List all quotas
kubectl get quotas

# Get detailed quota information
kubectl describe quota <quota-name>

# Check quota status
kubectl get quota <quota-name> -o yaml
```

#### Debugging Quota Issues
```bash
# Check quota controller logs
kubectl logs -n korder-system deployment/korder-controller-manager -c manager | grep quota

# Verify RBAC permissions
kubectl auth can-i create quotas --as=<user>
kubectl auth can-i get quotas --as=<user>
```

### Integration with Orders and Tickets

#### Quota Enforcement Flow
1. **Order Creation**: Quota controller validates if the new Order would exceed quota limits
2. **Ticket Generation**: Each Ticket creation is checked against applicable quotas
3. **Resource Reservation**: Guardian pods are subject to quota constraints
4. **Duration Limits**: Orders exceeding max-duration are rejected or auto-expired

#### Quota Validation Points
- Order webhook validates against applicable quotas before creation
- Ticket controller checks quotas before creating guardian pods
- Time-based quota multipliers are applied during validation
- Hierarchical allocation is enforced for namespace-specific limits

### Development and Testing

#### Quota Controller Testing
```bash
# Run quota controller unit tests
make test

# Run specific quota controller tests
go test ./internal/controller -run TestQuota

# Test quota validation logic
go test ./internal/webhook/v1alpha1 -run TestQuotaWebhook
```

#### E2E Testing Scenarios
- Create Orders that exceed quota limits
- Test time-window based quota enforcement
- Verify hierarchical allocation across namespaces
- Test quota updates and their impact on existing Orders/Tickets

#### Sample Test Cases
Located in `internal/controller/quota_controller_test.go`:
- Basic quota reconciliation
- Quota scope validation
- Resource limit enforcement
- Time-window multiplier application

## Testing

### Unit Tests
Located in `internal/controller/*_test.go` and `internal/webhook/*/*_test.go`. Use Ginkgo/Gomega testing framework.

### E2E Tests
Located in `test/e2e/`. Requires Kind cluster and tests full system integration.

### Test Environment Setup
The project uses envtest for controller testing with automatic Kubernetes API server setup.

## Key Files
- `cmd/main.go` - Main controller entry point with webhook and metrics server setup
- `PROJECT` - Kubebuilder project configuration
- `Dockerfile` - Container build configuration
- `docs/requirements.md` - Chinese language system requirements and design (contains architectural details)
- `docs/quota.yaml` - Comprehensive Quota configuration example with advanced features
- `config/samples/core_v1alpha1_quota.yaml` - Basic Quota sample configuration
- `config/rbac/quota_*_role.yaml` - RBAC roles for Quota management (admin, editor, viewer)
- `config/crd/bases/core.korder.dev_quotas.yaml` - Quota Custom Resource Definition