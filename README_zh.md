# korder

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/monshunter/korder)](https://goreportcard.com/report/github.com/monshunter/korder)
[![Kubernetes](https://img.shields.io/badge/kubernetes-%231.28+-blue.svg)](https://kubernetes.io/)

中文 | [English](README.md)

**korder** 是一个声明式的 Kubernetes 资源订单系统，通过订单和票据机制保证资源分配。

## 🏗️ 架构概览

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                                korder 系统架构                                   │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                 │
│  用户/应用                                                                       │
│    │                                                                            │
│    │ 1. 创建 Order                                                               │
│    │                                                                            │
│    ▼                                                                            │
│ ┌─────────────────-┐    2. 创建 Tickets    ┌────────────────--─┐                 │
│ │  Order Controller│ ────────────────────▶ │ Ticket Controller│                 │
│ │                  │                       │                  │                 │
│ │ • 管理订单生命周期  │                       │ • 管理票据生命周期  │                 │
│ │ • 根据策略创建票据  │                       │ • 创建守护者 Pod   │                 │
│ │ • 处理调度和更新   │                       │ • 绑定业务 Pod     │                 │
│ └─────────────────-┘                       └────────────────-─┘                  │
│                                                    │                             │
│                                           3. 创建守护者Pod                         │
│                                                    │                             │
│                                                    ▼                             │
│ ┌─────────────────┐                       ┌─────────────────┐                    │
│ │ Quota Controller│                       │   Guardian Pod  │                    │
│ │                 │                       │                 │                    │
│ │ • 资源配额管理    │                       │ • 持有资源预留    │                    │
│ │ • 多范围限制      │                       │ • 等待业务绑定    │                    │
│ │ • 时间窗口配额    │                       │ • 自动过期清理    │                    │
│ └─────────────────┘                       └─────────────────┘                    │
│          │                                                                       │
│          │ 4. 配额验证                                                            │
│          │                                                                       │
│          ▼                                                                      │
│ ┌─────────────────┐    5. 拦截Pod创建     ┌─────────────────┐                     │
│ │ Admission       │ ◄─────────────────── │   Business Pod  │                    │
│ │ Webhook         │                      │                 │                    │
│ │                 │ ────────────────────▶│ • 声明资源需求    │                    │
│ │ • 验证Order/Quota│    6. 绑定到Ticket    │ • 绑定到票据     │                     │
│ │ • 拦截Pod创建    │                      │ • 使用预留资源    │                     │
│ └─────────────────┘                      └─────────────────┘                    │
│                                                                                 │
├─────────────────────────────────────────────────────────────────────────────────┤
│                              核心工作流程                                         │
│                                                                                 │
│ 1. 用户创建 Order，声明资源需求和数量                                                │
│ 2. Order Controller 根据策略创建对应数量的 Tickets                                  │
│ 3. Ticket Controller 为每个 Ticket 创建 Guardian Pod 持有资源                      │
│ 4. Quota Controller 验证资源使用是否符合配额限制                                     │
│ 5. 用户创建业务 Pod 时，Admission Webhook 拦截并绑定到合适的 Ticket                   │
│ 6. Guardian Pod 被删除，业务 Pod 获得预留的资源                                      │
│ 7. Ticket 被标记为已使用，或在过期后自动清理                                          │
└─────────────────────────────────────────────────────────────────────────────────┘
```

## ✨ 核心特性

- **🎯 资源预留**: 通过守护者 Pod 预先分配和持有资源
- **📋 声明式订单**: 使用 Order CRD 声明资源需求和管理策略  
- **🎫 票据机制**: Ticket 提供细粒度的资源分配和生命周期管理
- **📊 配额管理**: 多层级、多范围的资源配额控制
- **⏰ 时间调度**: 支持一次性、定时和周期性资源分配策略
- **🔍 自动发现**: 通过 Admission Webhook 自动绑定业务 Pod 到预留资源
- **🗂️ 多种策略**: OneTime、Scheduled、Recurring 等不同的资源分配模式

## 🚀 快速开始

### 前置条件

- Kubernetes 1.28+
- kubectl 配置并连接到集群
- 集群管理员权限（安装 CRDs 和 RBAC）

### 安装 korder

#### 方法一：使用预构建的安装文件

```bash
# 安装 CRDs 和控制器
kubectl apply -f https://github.com/monshunter/korder/releases/latest/download/install.yaml

# 验证安装
kubectl get pods -n korder-system
```

#### 方法二：从源码构建

```bash
# 克隆仓库
git clone https://github.com/monshunter/korder.git
cd korder

# 安装 CRDs
make install

# 部署控制器到集群
make deploy

# 验证部署
kubectl get pods -n korder-system
```

#### 方法三：本地开发运行

```bash
# 安装 CRDs
make install

# 本地运行控制器（需要有效的 kubeconfig）
make run
```

### 基础使用示例

#### 1. 创建简单的资源订单

```yaml
apiVersion: core.korder.dev/v1alpha1
kind: Order
metadata:
  name: my-order
  namespace: default
spec:
  # 创建 3 个票据
  replicas: 3
  
  # 一次性策略
  strategy:
    type: OneTime
    refreshPolicy: OnClaim
  
  # 票据模板
  template:
    metadata:
      labels:
        app: my-application
    spec:
      # 票据有效期 24 小时
      duration: 24h
      
      # 资源需求
      resources:
        requests:
          cpu: 500m
          memory: 1Gi
        limits:
          cpu: 1000m
          memory: 2Gi
      
      # 节点选择
      nodeSelector:
        node-type: compute
```

```bash
# 应用订单
kubectl apply -f order.yaml

# 查看订单状态
kubectl get orders

# 查看生成的票据
kubectl get tickets

# 查看守护者 Pod
kubectl get pods -l korder.dev/role=guardian
```

#### 2. 创建使用预留资源的业务 Pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-business-pod
  annotations:
    # 声明需要使用 korder 资源
    korder.dev/required: "true"
    # 可选：指定使用特定订单
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
# 创建业务 Pod
kubectl apply -f business-pod.yaml

# 查看 Pod 是否成功绑定到票据
kubectl get pods my-business-pod -o yaml | grep korder.dev/

# 查看票据状态变化
kubectl get tickets -o wide
```

#### 3. 定时资源订单

```yaml
apiVersion: core.korder.dev/v1alpha1
kind: Order
metadata:
  name: scheduled-order
  namespace: default
spec:
  replicas: 2
  
  # 每天凌晨 2 点执行
  strategy:
    type: Scheduled
    schedule: "0 2 * * *"
    refreshPolicy: Always
  
  template:
    spec:
      duration: 8h  # 8 小时窗口
      resources:
        requests:
          cpu: 200m
          memory: 512Mi
```

#### 4. 配置资源配额

```yaml
apiVersion: core.korder.dev/v1alpha1
kind: Quota
metadata:
  name: team-quota
spec:
  # 作用于特定命名空间
  scope:
    type: NamespaceSelector
    namespaceSelector:
      matchLabels:
        team: "backend"
  
  # 资源限制
  hard:
    orders: "10"              # 最多 10 个订单
    tickets: "50"             # 最多 50 个票据
    reserved.cpu: "20"        # 最多预留 20 核 CPU
    reserved.memory: "40Gi"   # 最多预留 40GB 内存
    max-duration: "24h"       # 最大预留时间 24 小时
```

### 监控和调试

```bash
# 查看所有 korder 资源
kubectl get orders,tickets,quotas --all-namespaces

# 查看控制器日志
kubectl logs -n korder-system deployment/korder-controller-manager -f

# 查看特定订单的事件
kubectl describe order my-order

# 查看票据详情
kubectl describe ticket <ticket-name>

# 检查配额使用情况
kubectl describe quota team-quota
```

## 📚 高级用法

### 调度策略

- **OneTime**: 一次性创建，票据被使用后根据 refreshPolicy 决定是否重新创建
- **Scheduled**: 按 cron 表达式定时创建票据
- **Recurring**: 周期性创建票据，支持复杂的时间模式

### 配额管理

- **多范围支持**: Cluster、NamespaceSelector、NamespaceList、ObjectSelector
- **时间窗口**: 不同时间段的不同资源限制
- **层级分配**: 为不同环境分配子配额

### 生命周期管理

- **TTL 控制**: 票据完成后的保留时间
- **清理策略**: Delete 或 Retain
- **过期处理**: 自动清理过期的票据和守护者 Pod

## 🛠️ 开发

```bash
# 构建项目
make build

# 运行测试
make test

# 运行端到端测试
make test-e2e

# 代码检查
make lint

# 生成代码和清单
make generate manifests
```

详细的开发指南请参考 [CLAUDE.md](CLAUDE.md)。

## 🤝 贡献

欢迎提交 Issue 和 Pull Request！

## 📄 许可证

本项目使用 [Apache 2.0](LICENSE) 许可证。