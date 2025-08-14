# kubernetes 资源订单系统
- 通过订单系统保证资源符合系统和业务的需求

## 背景
- CA/Karpenter 无法保证资源的长期存在, 也不能及时补充资源
- 业务方需要长期占用资源，但是又不希望长期运行业务Pod
- 业务方需要在特定时间窗口内，拥有特定数量的资源
- 业务方需要在特定时间窗口内，拥有特定数量的资源，并且这些资源需要满足特定的条件
- 系统需要一直预留足够的资源，以满足突发的业务需求
## 目标
- 通过订单系统，保证资源的长期存在
- 通过订单系统，保证资源的及时补充
- 通过订单系统，保证资源的特定时间窗口内，拥有特定数量的资源
- 通过订单系统，保证资源的特定时间窗口内，拥有特定数量的资源，并且这些资源需要满足特定的条件
- 通过订单系统，保证系统一直预留足够的资源，以满足突发的业务需求
## API关系
- Quota 限制资源的使用配额, 参考quota.yaml
- Order 管理 **Ticket的生命周期**, 参考order.yaml, daemonOrder.yaml, cronOrder.yaml
  - Kind: Order 类比Deployment
  - Kind: DaemonOrder 类比Daemonset
  - Kind: CronOrder 类比CronJob
- Ticket 描述资源的属性, 参考ticket.yaml
- Ticket 会创建并绑定守护者Pod，守护者Pod会一直运行，直到Ticket过期或者被使用
- 用户通过 Order 声明一组资源的属性和数量，订单系统会根据这些属性和数量，创建Ticket，并且绑定守护者Pod
- 每个守护者Pod都对应一个Ticket，守护者Pod上将复用Ticket上的属性
- 当Ticket被使用时，订单系统会将Ticket标记为Claimed，并且记录被使用的Pod，同时守护者Pod会被删除
- 当Ticket过期时，订单系统会将Ticket标记为Expired，并且删除守护者Pod，一个过期的Ticket会根据保留策略或被自动删除
- 系统将复用默认调度器的调度插件来为业务的Pod选择Ticket（也就是绑定到守护者Pod所在的节点上），这些插件包含但不限于:
  - NodeAffinity
  - PodAffinity
  - PodAntiAffinity
  - Taints and Tolerations
  - NodeSelector
  - TopologySpreadConstraints
  - Locality
- 如何不能通过以上插件满足需求，业务Pod不会被调度到Ticket上，这个Pod最后会fallback到调度器安排调度
- 用户也可以直接创建Ticket，但是不推荐，因为无法被Order管理生命周期
- 用户通过在业务Pod上使用特定annotation，声明对Ticket的需求，订单系统会根据这些需求，找到合适的Ticket，并且将业务Pod绑定到守护者Pod所在的节点上
- 用户也可以通过Pod上使用特定annotation，声明通过那个Order来满足资源需求
- 订单系统会通过Quota机制，保证资源的使用符合配额限制, Quota作用于Order和Ticket
- 订单系统会通过Admission Webhook，保证资源的创建符合订单系统的要求
- 订单系统会通过Metrics，提供资源的使用情况和订单系统的运行情况
- 订单系统会通过RBAC，保证资源的访问符合权限限制
- 订单系统会通过OwnerReferences，保证资源的级联删除
- 订单系统会通过Finalizer，保证资源的有序删除
- 使用事件event来记录Ticket的生命周期的变化

## 组件
- Order Controller
- Ticket Controller
- Quota Controller
- Admission Webhook
  - Order Webhook
  - Ticket Webhook
  - Pod Webhook
  - Quota Webhook
- Metrics
- RBAC

