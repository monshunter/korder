# korder

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/monshunter/korder)](https://goreportcard.com/report/github.com/monshunter/korder)
[![Kubernetes](https://img.shields.io/badge/kubernetes-%231.28+-blue.svg)](https://kubernetes.io/)

[中文文档](README_zh.md) | English

**korder** is a declarative Kubernetes resource ordering system that ensures resource allocation through order and ticket mechanisms.

## 🏗️ Architecture Overview

```
┌──────────────────────────────────────────────────────────────────────────────────┐
│                                korder System Architecture                        │
├──────────────────────────────────────────────────────────────────────────────────┤
│                                                                                  │
│  User/Application                                                                │
│    │                                                                             │
│    │ 1. Create Order                                                             │
│    │                                                                             │
│    ▼                                                                             │
│ ┌──────────────────┐    2. Create Tickets  ┌──────────────────┐                  │
│ │  Order Controller│ ────────────────────▶ │ Ticket Controller│                  │
│ │                  │                       │                  │                  │
│ │ • Manage order   │                       │ • Manage ticket  │                  │
│ │   lifecycle      │                       │   lifecycle      │                  │
│ │ • Create tickets │                       │ • Create guardian│                  │
│ │   by strategy    │                       │   pods           │                  │
│ │ • Handle         │                       │ • Bind business  │                  │
│ │   scheduling     │                       │   pods           │                  │
│ └─────────────────-┘                       └────────────────-─┘                  │
│                                                    │                             │
│                                           3. Create Guardian Pod                 │
│                                                    │                             │
│                                                    ▼                             │
│ ┌─────────────────┐                       ┌─────────────────┐                    │
│ │ Quota Controller│                       │   Guardian Pod  │                    │
│ │                 │                       │                 │                    │
│ │ • Resource quota│                       │ • Hold resource │                    │
│ │   management    │                       │   reservation   │                    │
│ │ • Multi-scope   │                       │ • Wait for      │                    │
│ │   limits        │                       │   business      │                    │
│ │ • Time window   │                       │   binding       │                    │
│ │   quotas        │                       │ • Auto expiry   │                    │
│ │                 │                       │   cleanup       │                    │
│ └─────────────────┘                       └─────────────────┘                    │
│          │                                                                       │
│          │ 4. Quota validation                                                   │
│          │                                                                       │
│          ▼                                                                       │
│ ┌─────────────────┐    5. Intercept Pod  ┌─────────────────┐                     │
│ │ Admission       │ ◄─────────────────── │   Business Pod  │                     │
│ │ Webhook         │      creation        │                 │                     │
│ │                 │                      │ • Declare       │                     │
│ │ • Validate      │ ────────────────────▶│   resource      │                     │
│ │   Order/Quota   │    6. Bind to Ticket │   requirements  │                     │
│ │ • Intercept Pod │                      │ • Bind to       │                     │
│ │   creation      │                      │   ticket        │                     │
│ │                 │                      │ • Use reserved  │                     │
│ │                 │                      │   resources     │                     │
│ └─────────────────┘                      └─────────────────┘                     │
│                                                                                  │
├──────────────────────────────────────────────────────────────────────────────────┤
│                              Core Workflow                                       │
│                                                                                  │
│ 1. User creates Order, declaring resource requirements and quantities            │
│ 2. Order Controller creates corresponding number of Tickets based on strategy    │
│ 3. Ticket Controller creates Guardian Pod for each Ticket to hold resources      │
│ 4. Quota Controller validates resource usage against quota limits                │
│ 5. When user creates business Pod, Admission Webhook intercepts and binds to     │
│    appropriate Ticket                                                            │
│ 6. Guardian Pod is deleted, business Pod gets the reserved resources             │
│ 7. Ticket is marked as used, or automatically cleaned up after expiration        │
└──────────────────────────────────────────────────────────────────────────────────┘
```

## ✨ Core Features

- **🎯 Resource Reservation**: Pre-allocate and hold resources through guardian pods
- **📋 Declarative Orders**: Use Order CRD to declare resource requirements and management policies
- **🎫 Ticket Mechanism**: Tickets provide fine-grained resource allocation and lifecycle management
- **📊 Quota Management**: Multi-level, multi-scope resource quota control
- **⏰ Time Scheduling**: Support one-time, scheduled, and recurring resource allocation strategies
- **🔍 Auto Discovery**: Automatically bind business pods to reserved resources via Admission Webhook
- **🗂️ Multiple Strategies**: Different resource allocation modes like OneTime, Scheduled, Recurring
- **🌐 DaemonSet Mode**: Automatically reserve resources on every eligible node for infrastructure services

## 🚀 Quick Start

### Prerequisites

- Kubernetes 1.28+
- kubectl configured and connected to cluster
- Cluster admin permissions (for installing CRDs and RBAC)

### Install korder

#### Method 1: Using pre-built installation files

```bash
# Install CRDs and controllers
kubectl apply -f https://github.com/monshunter/korder/releases/latest/download/install.yaml

# Verify installation
kubectl get pods -n korder-system
```

#### Method 2: Build from source

```bash
# Clone repository
git clone https://github.com/monshunter/korder.git
cd korder

# Install CRDs
make install

# Deploy controller to cluster
make deploy

# Verify deployment
kubectl get pods -n korder-system
```

#### Method 3: Local development run

```bash
# Install CRDs
make install

# Run controller locally (requires valid kubeconfig)
make run
```

### Basic Usage Examples

#### 1. Create a simple resource order

```yaml
apiVersion: core.korder.dev/v1alpha1
kind: Order
metadata:
  name: my-order
  namespace: default
spec:
  # Create 3 tickets
  replicas: 3
  
  # One-time strategy
  strategy:
    type: OneTime
    refreshPolicy: OnClaim
  
  # Ticket template
  template:
    metadata:
      labels:
        app: my-application
    spec:
      # Ticket valid for 24 hours
      duration: 24h
      
      # Resource requirements
      resources:
        requests:
          cpu: 500m
          memory: 1Gi
        limits:
          cpu: 1000m
          memory: 2Gi
      
      # Node selection
      nodeSelector:
        node-type: compute
```

```bash
# Apply order
kubectl apply -f order.yaml

# Check order status
kubectl get orders

# Check generated tickets
kubectl get tickets

# Check guardian pods
kubectl get pods -l korder.dev/role=guardian
```

#### 2. Create business pod using reserved resources

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-business-pod
  annotations:
    # Declare need to use korder resources
    korder.dev/required: "true"
    # Optional: specify using specific order
    korder.dev/order: "my-order"
spec:
  containers:
  - name: app
    image: nginx:latest
    resources:
      requests:
        cpu: 500m
        memory: 1Gi
      limits:
        cpu: 1000m
        memory: 2Gi
```

```bash
# Create business pod
kubectl apply -f business-pod.yaml

# Check if pod is successfully bound to ticket
kubectl get pods my-business-pod -o yaml | grep korder.dev/

# Check ticket status changes
kubectl get tickets -o wide
```

#### 3. Scheduled resource order

```yaml
apiVersion: core.korder.dev/v1alpha1
kind: Order
metadata:
  name: scheduled-order
  namespace: default
spec:
  replicas: 2

  # Execute at 2 AM daily
  strategy:
    type: Scheduled
    schedule: "0 2 * * *"
    refreshPolicy: Always

  template:
    spec:
      duration: 8h  # 8-hour window
      resources:
        requests:
          cpu: 200m
          memory: 512Mi
```

#### 4. Configure resource quota

```yaml
apiVersion: core.korder.dev/v1alpha1
kind: Quota
metadata:
  name: team-quota
spec:
  # Apply to specific namespace
  scope:
    type: NamespaceSelector
    namespaceSelector:
      matchLabels:
        team: "backend"

  # Resource limits
  hard:
    orders: "10"              # Max 10 orders
    tickets: "50"             # Max 50 tickets
    reserved.cpu: "20"        # Max 20 CPU cores reserved
    reserved.memory: "40Gi"   # Max 40GB memory reserved
    max-duration: "24h"       # Max reservation time 24 hours
```

#### 5. DaemonSet mode for node-level reservations

```yaml
apiVersion: core.korder.dev/v1alpha1
kind: Order
metadata:
  name: monitoring-agent-reservation
  namespace: korder-system
spec:
  # Enable DaemonSet mode - one ticket per eligible node
  daemonSet: true

  # Strategy for maintaining reservations
  strategy:
    type: Recurring
    schedule: "0 */6 * * *"  # Check every 6 hours
    refreshPolicy: OnClaim

  # Ticket template applied to each node
  template:
    metadata:
      labels:
        app: monitoring-agent
        tier: infrastructure
    spec:
      duration: 24h

      # Resource requirements per node
      resources:
        requests:
          cpu: 100m
          memory: 200Mi
        limits:
          cpu: 200m
          memory: 400Mi

      # Schedule on all Linux nodes
      nodeSelector:
        kubernetes.io/os: linux

      # Tolerate common taints
      tolerations:
        - operator: Exists
          effect: NoSchedule
```

```bash
# Apply DaemonSet order
kubectl apply -f daemonset-order.yaml

# Check tickets created (one per eligible node)
kubectl get tickets -l korder.dev/order=monitoring-agent-reservation

# Check guardian pods on each node
kubectl get pods -l korder.dev/role=guardian -o wide
```

### Monitoring and Debugging

```bash
# View all korder resources
kubectl get orders,tickets,quotas --all-namespaces

# View controller logs
kubectl logs -n korder-system deployment/korder-controller-manager -f

# View specific order events
kubectl describe order my-order

# View ticket details
kubectl describe ticket <ticket-name>

# Check quota usage
kubectl describe quota team-quota
```

## 📚 Advanced Usage

### Scheduling Strategies

- **OneTime**: One-time creation, tickets are recreated based on refreshPolicy after use
- **Scheduled**: Create tickets at scheduled times using cron expressions
- **Recurring**: Periodically create tickets with support for complex time patterns

### Quota Management

- **Multi-scope support**: Cluster, NamespaceSelector, NamespaceList, ObjectSelector
- **Time windows**: Different resource limits for different time periods
- **Hierarchical allocation**: Allocate sub-quotas for different environments

### Lifecycle Management

- **TTL control**: Retention time for tickets after completion
- **Cleanup policies**: Delete or Retain
- **Expiry handling**: Automatic cleanup of expired tickets and guardian pods

## 🛠️ Development

```bash
# Build project
make build

# Run tests
make test

# Run end-to-end tests
make test-e2e

# Code linting
make lint

# Generate code and manifests
make generate manifests
```

For detailed development guide, please refer to [CLAUDE.md](CLAUDE.md).

## 🤝 Contributing

Issues and Pull Requests are welcome!

## 📄 License

This project is licensed under [Apache 2.0](LICENSE).
