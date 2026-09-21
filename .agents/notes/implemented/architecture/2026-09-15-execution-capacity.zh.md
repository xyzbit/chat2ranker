# Agent Note：Execution 容量与有界扩缩容

Status: implemented

[English](2026-09-15-execution-capacity.md) | 中文

## 问题

队列总数包含增加 Worker 也无法启动的工作。模型/租户限额、运行时能力缺失和投递退避需要分别处理；业务失败的运行时也可能尚未停止，仍占用额度。

## 决策

受鉴权保护的 `GET /v1/capacity` 在同一个一致的数据库事务快照中读取排队及保留资源的执行，只返回在线/评测聚合计数和配置槽位，不返回提示词、结果、凭证或租户/模型标识。可选的独立 `EXECUTION_CAPACITY_TOKEN` 仅授权这一读取；服务令牌仍可使用。存储失败返回错误，不伪装成零需求。

PostgreSQL 使用自身准入限额。运行中及终态待清理记录占用同一套全局模型/租户额度，包括其他后端的保留额度。符合条件的排队记录按创建时间/ID 跨两个队列虚拟分配剩余额度，用于估算需求，不授权执行、不改变 River 的领取顺序。停用队列、不支持的后端/可执行文件/历史，以及没有请求级连接时缺少默认凭证，归为能力阻塞。CLI 安装检查独立于默认凭证。未来错误重试、缺失或已放弃的投递归为投递阻塞；额度等待的 scheduled snooze 计入下一次轮询需求。SQLite 只投影本地队列，不声称支持分布式配额。

[Sandbox 资源准入](2026-09-15-execution-runtime-pool.md)在两个 Repository 中增加持久 CPU/内存预算与冻结计划。投影同时应用资源预算和模型/租户限制，保留未知分配，并从 ready 需求中排除 `resourceBlocked` 工作。这是对运维分配预算的估算，不是物理主机空闲资源测量。

每个启用队列的需求为 active 加 ready。active 表示持久化的 running/cancelling 尝试，不是 Worker 存活测量。终态待清理记录保留额度，但不请求新 Worker。最老可执行等待时间是当前年龄采样，不是历史 p95，也不是额度释放后的等待时长。失去拥有者的 active 记录可在协调完成前保守地保留需求。API 和 Worker 必须使用同一组后端/能力配置、准入限额和槽位；接口不发现异构 Worker 池或供应商 RPM/TPM 限流。

可选的 [minikube 实验](../../../../rank/deploy/k8s/README.md) 使用 KEDA 2.20.2 的鉴权 Metrics API scaler。两个队列分别以单 Worker 槽位为目标，Kubernetes 取较大的副本需求。实验最少一个、最多三个 Worker，每 15 秒最多增加一个，45 秒稳定窗口后每 30 秒最多减少一个。不跨 API 副本累加同一份数据库统计，不为同一 Deployment 安装第二个 HPA。指标读取失败不转换成零值。平滑退出仍由 Execution Worker 负责。

Docker 实验通过检查版本的 OpenSandbox 0.2.3 构建补丁关闭 Sandbox 宿主机端口映射。上游端口探测运行在服务容器命名空间中，可能选中 Docker 宿主机已绑定的端口。私网容器端点及鉴权代理即可满足 Rank 访问，无须这一分配器。这不会授权重放创建结果不明的调用。带有精确 execution/attempt/scope 标签的 Docker 创建/销毁事件可以为经审计的人工修复提供丢失句柄；普通协调器随后确认不存在并释放额度，保留原失败结果。

## 考虑过的替代方案

**按排队总数或 CPU 扩容。** 模型额度耗尽会产生积压，却不需要更多计算资源；CPU 也无法表达等待模型回复占用的执行时间。

**先增加 Worker 注册中心和 Prometheus。** 当前 Worker 池只有一种配置，已有持久化全局状态，KEDA 可以直接读取聚合值。实时槽位占用及异构路由需要独立证据后再引入拥有权机制。

## 后果

真实数据库测试覆盖投递条件和清理保留额度；应用测试覆盖跨队列额度、能力排除、只读令牌和存储失败。组装后的无密钥生命周期快照在 SQLite 和独立 PostgreSQL/River 进程中执行同一接口。[实验脚本](../../../../rank/deploy/k8s/verify-autoscaling.py) 对延迟响应的确定性模型生成有界真实 DSH 流量，验证扩容、排空及模型额度为一时的行为，最后恢复限额和手动管理的两个 Worker。

[实测记录](../../../../rank/deploy/reports/2026-09-15-autoscaling.md)中的 16 个负载请求全部在第一次尝试完成，Worker 从一扩到三再缩到一，模型并发额度为一时保持一个 Worker。远程 Docker 容器不受 Kubernetes 调度，增加控制它们的 Worker 不会增加 Sandbox 宿主机。这一信号不测量供应商限流、物理主机容量或生产可用性。[生产方案](../../proposed/architecture/2026-09-14-rank-production-execution.md) 保留这些要求。[固化排队截止时间](2026-09-15-execution-queue-deadline.md)将过期排队工作从需求中排除。KEDA 安装涉及集群级资源，虽然 Worker 监听范围限制在 `rank-dev`。
