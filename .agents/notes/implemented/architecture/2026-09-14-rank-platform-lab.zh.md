# Agent Note: 完整 Rank Kubernetes 实验

Status: implemented

[English](2026-09-14-rank-platform-lab.md) | 中文

## 问题

仅 Execution 的 fixture 无法证明 Rank 已发布端点、结构化会话及持久化中控在进程替换后能共同工作。可重复的集群实验必须使用平台相同的公开客户端和打包 Harness。

## 方案

[完整实验](../../../../rank/deploy/k8s/README.md#complete-platform-experiment)运行 Web 网关、Rank、中控和真实 DSH Worker，并使用 PostgreSQL/River 与 S3。只声明依赖的[运行时根包](../../../../apps/rank-runtime/README.md)显式列出必需的工作区 peer。暂存步骤实体化依赖链接并验证 Linux 原生归档，使镜像不依赖开发者检出目录或 macOS 模块。镜像标签标识暂存内容，包括二进制、运行时、Web 资源及复制进镜像的清单。

Rank HTTP 副本采用[滚动 API 生命周期](../bug-fix/2026-09-17-rank-api-rolling-drain.md)，调度器与中控各保留一个拥有者并使用 `Recreate`。Rank 业务与登录记录使用 PostgreSQL schema；中控会话和私有 Webhook 密钥使用独立 PVC。Worker 共享持久任务和对象，使用独立临时工作目录。完整维护升级先停止生产者并排空消费者，再运行顺序执行的 Execution/Rank 迁移 Job；即使 schema 兼容的 HTTP 替换可以独立滚动，该维护路径仍有明确中断。

网关通过私有实验 Basic 凭据保护管理与静态路由，将公开端点的 Bearer 鉴权交给 Rank，并隐藏内部控制命令。端口转发绑定回环地址。[数据库权限决策](2026-09-15-rank-database-permissions.md)隔离 schema 拥有者、应用登录账号与中控凭据。这个可信单操作者拓扑不证明生产身份、租户隔离或恶意代码隔离。

显式维护选项可选择 [OpenSandbox 后端](2026-09-15-execution-opensandbox.md)。Docker 生命周期服务运行在 minikube 外，完整 Harness 容器运行在可信 Worker Pod 外。相同的平台验证器检查远程分配身份与 S3 支持的结果。此拓扑验证一台笔记本上的集成，不代表 Kubernetes 原生 Sandbox 控制器。

## 考虑过的替代方案

**仅验收 Execution fixture。** 可以证明投递，却遗漏 Rank 持久派发、端点 API 和独立的中控会话。

**将开发者检出目录安装到每个镜像。** 会保留宿主链接和原生二进制，增加部署内容并降低可重复性。明确声明的运行时依赖闭包使打包错误能在发布前发现。

**将共享 Worker Pod 当作沙箱。** 资源限制约束可信实验进程池，不能隔离各项任务，也不能阻止不可信运行时读取 Worker 凭据。

## 后果

[验收脚本](../../../../rank/deploy/k8s/verify-platform.mjs)运行已有每日热点与 OpenAI SDK 聊天客户端，检查原生结构化历史、SSE 重放和 S3 证据读取。替换 Rank/中控 Pod 后，验证服务续聊和中控会话恢复。Worker 为零时，已接受请求跨 Rank 替换保留，扩容后以第一次尝试完成，旧产物仍可读取。脚本在成功或失败后恢复两个 Worker 副本。

[验收记录](../../../../rank/deploy/reports/2026-09-14-platform.md)使用确定性模型响应和来源快照，不表示实时新闻或外部模型质量。浏览器验收覆盖两轮试用、刷新恢复、Agent 到端点导航、接入、设置和文档。物理主机容量、生产鉴权、完整运行时隔离及自动扩缩容分别遵循[生产提案](../../proposed/architecture/2026-09-14-rank-production-execution.md)的验收要求。
