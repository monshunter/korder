# DaemonSet Mode in korder

## Overview

DaemonSet mode is a powerful feature in korder that automatically reserves resources on every eligible node in the cluster. This is particularly useful for infrastructure services, monitoring agents, security scanners, and other node-level workloads that need guaranteed resource availability.

## Why DaemonSet Mode?

### Traditional Challenges

1. **Infrastructure Service Resource Competition**: System services like monitoring agents, log collectors, and security scanners often compete with business workloads for node resources
2. **Node Pressure Relief**: Without proper resource reservation, nodes can become overloaded, leading to performance degradation
3. **Over-subscription Risk**: Kubernetes allows over-subscription of resources, which can cause resource starvation for critical infrastructure services
4. **Manual Management Overhead**: Managing individual reservations for each node is time-consuming and error-prone

### DaemonSet Mode Benefits

1. **Automatic Node Discovery**: Automatically detects eligible nodes and creates reservations
2. **Dynamic Scaling**: Automatically adjusts when nodes are added or removed from the cluster
3. **Infrastructure Protection**: Ensures critical infrastructure services always have resources available
4. **Simplified Management**: Single Order manages reservations across all eligible nodes
5. **Resource Buffer**: Provides a buffer against resource over-subscription

## How It Works

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        DaemonSet Order                         │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ spec:                                                   │    │
│  │   daemonSet: true                                       │    │
│  │   template:                                             │    │
│  │     spec:                                               │    │
│  │       nodeSelector: {...}                              │    │
│  │       tolerations: [...]                               │    │
│  │       resources: {...}                                 │    │
│  └─────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Order Controller                             │
│  1. Discovers eligible nodes using nodeSelector & tolerations  │
│  2. Calculates desired replicas = number of eligible nodes     │
│  3. Creates one Ticket per eligible node                       │
│  4. Watches for node changes and adjusts accordingly           │
└─────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Per-Node Tickets                          │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐            │
│  │   Node-1    │  │   Node-2    │  │   Node-3    │            │
│  │   Ticket    │  │   Ticket    │  │   Ticket    │            │
│  │     +       │  │     +       │  │     +       │            │
│  │  Guardian   │  │  Guardian   │  │  Guardian   │            │
│  │    Pod      │  │    Pod      │  │    Pod      │            │
│  └─────────────┘  └─────────────┘  └─────────────┘            │
└─────────────────────────────────────────────────────────────────┘
```

### Node Eligibility Criteria

A node is considered eligible for DaemonSet tickets if:

1. **Node is Ready**: The node must be in Ready condition
2. **Node is Schedulable**: The node must not be cordoned (unschedulable)
3. **NodeSelector Match**: The node labels must match the nodeSelector in the ticket template
4. **Toleration Compatibility**: The ticket's tolerations must allow scheduling on the node's taints

### Dynamic Behavior

- **Node Addition**: When a new eligible node joins the cluster, a new ticket is automatically created
- **Node Removal**: When a node is removed, its associated ticket is automatically cleaned up
- **Node State Changes**: When a node becomes unschedulable or changes labels, tickets are adjusted accordingly

## Configuration

### Basic DaemonSet Order

```yaml
apiVersion: core.korder.dev/v1alpha1
kind: Order
metadata:
  name: infrastructure-reservation
spec:
  # Enable DaemonSet mode
  daemonSet: true
  
  strategy:
    type: OneTime
    refreshPolicy: OnClaim
  
  template:
    spec:
      duration: 24h
      resources:
        requests:
          cpu: 100m
          memory: 200Mi
        limits:
          cpu: 200m
          memory: 400Mi
      
      # Target all Linux nodes
      nodeSelector:
        kubernetes.io/os: linux
      
      # Tolerate common taints
      tolerations:
        - operator: Exists
          effect: NoSchedule
```

### Advanced Configuration

```yaml
apiVersion: core.korder.dev/v1alpha1
kind: Order
metadata:
  name: gpu-node-buffer
spec:
  daemonSet: true
  
  strategy:
    type: Recurring
    schedule: "0 */6 * * *"  # Refresh every 6 hours
    refreshPolicy: Always
  
  template:
    spec:
      duration: 8h
      resources:
        requests:
          cpu: 500m
          memory: 1Gi
        limits:
          cpu: 1000m
          memory: 2Gi
      
      # Only GPU nodes
      nodeSelector:
        accelerator: nvidia-tesla-k80
        kubernetes.io/os: linux
      
      # Tolerate GPU-specific taints
      tolerations:
        - key: nvidia.com/gpu
          operator: Exists
          effect: NoSchedule
        - key: dedicated
          operator: Equal
          value: gpu
          effect: NoSchedule
```

## Use Cases

### 1. Monitoring and Observability

Reserve resources for monitoring agents like Prometheus Node Exporter, Datadog agents, or custom monitoring solutions.

### 2. Log Collection

Ensure log collectors like Fluentd, Filebeat, or Vector have guaranteed resources on every node.

### 3. Security and Compliance

Reserve resources for security scanning agents, vulnerability assessments, and compliance monitoring tools.

### 4. Network Services

Provide resource guarantees for network plugins, service mesh sidecars, or network monitoring tools.

### 5. Storage Services

Reserve resources for storage drivers, CSI node plugins, or distributed storage agents.

### 6. Resource Buffers

Create resource buffers on specific node types (GPU, high-memory, etc.) to prevent over-subscription.

## Best Practices

### 1. Resource Sizing

- Start with conservative resource requests
- Monitor actual usage and adjust accordingly
- Consider peak usage patterns for infrastructure services

### 2. Node Selection

- Use specific nodeSelectors to target appropriate nodes
- Avoid overly broad selectors that might affect unintended nodes
- Consider node roles and capabilities

### 3. Toleration Strategy

- Use `operator: Exists` for maximum flexibility
- Be specific with tolerations for security-sensitive workloads
- Consider toleration seconds for graceful handling of node issues

### 4. Refresh Policies

- Use `OnClaim` for long-running infrastructure services
- Use `Always` for periodic maintenance or scanning workloads
- Use `Never` for permanent resource buffers

### 5. Monitoring

- Monitor ticket creation and deletion events
- Track resource utilization across nodes
- Set up alerts for failed ticket creation

## Troubleshooting

### No Tickets Created

1. Check if any nodes match the nodeSelector
2. Verify tolerations allow scheduling on available nodes
3. Check node readiness and schedulability
4. Review Order controller logs

### Tickets Not Distributed Evenly

This is expected behavior - DaemonSet mode creates one ticket per eligible node, not evenly distributed tickets.

### Performance Impact

DaemonSet mode watches node events, which may increase controller load in large clusters. Monitor controller performance and adjust accordingly.

## Limitations

1. **Cluster Size**: Very large clusters (1000+ nodes) may experience increased controller overhead
2. **Node Churn**: Frequent node additions/removals may cause ticket churn
3. **Resource Constraints**: Each node must have sufficient resources for the reservation
4. **Scheduling Conflicts**: Multiple DaemonSet orders with overlapping node selectors may compete for resources

## Migration from Manual Management

To migrate from manually managed per-node reservations to DaemonSet mode:

1. Create a DaemonSet Order with appropriate nodeSelector and tolerations
2. Verify tickets are created correctly
3. Gradually remove manual reservations
4. Monitor for any scheduling issues
5. Adjust resource requests as needed
