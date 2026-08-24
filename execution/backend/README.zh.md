# Execution 后端

[English](README.md) | 中文

该 Go 模块是可复用的执行控制平面，生成：

- `executiond`：版本化 HTTP API、幂等执行记录、生命周期控制、Repository 访问、Executor 选择与 Artifact 授权。
- `execution-worker`：在私有 workspace 和 Harness Home 中执行一次不可变 Harness Adapter 调用。

公共的 `contract` 与 `client` 包是唯一集成面。领域层与应用层依赖 Repository 和 Executor 接口。本地组合使用 SQLite 与 `LocalExecutor`；PostgreSQL、Docker、Kubernetes Job、Kata 与远程 Sandbox 适配器可以替换它们，且不需要导入 Rank 业务类型。

Harness 集成实现公共 `harness.Adapter` 接口，并通过稳定 Runner 类型注册。内置 Registry 包含确定性 Demo Adapter、第一方 DSH Adapter，以及 Pi、Claude Code、Codex 和 Hermes 的无 Shell 命令 Adapter。命令 Adapter 通过显式参数占位符接收不可变 `ExecutionSpec`，不执行 Shell 插值。

`GET /v1/executions/{id}/events` 以 SSE 暴露持久化的单次执行事件日志。每条事件包含单调递增序号、尝试次数、类型、状态、可选数据与时间戳。客户端重连时通过 `Last-Event-ID` 或 `?after=` 重放已持久化的生命周期与 Harness 进度，再继续追踪实时事件。
