# Execution 后端

[English](README.md) | 中文

该 Go 模块是可复用的执行控制平面，生成：

- `executiond`：版本化 HTTP API、幂等执行记录、生命周期控制、Repository 访问、Executor 选择与 Artifact 授权。
- `execution-worker`：在私有 workspace 和 Harness Home 中执行一次不可变 Harness Adapter 调用。

公共的 `contract` 与 `client` 包是唯一集成面。领域层与应用层依赖 Repository 和 Executor 接口。本地组合使用 SQLite 与 `LocalExecutor`；PostgreSQL、Docker、Kubernetes Job、Kata 与远程 Sandbox 适配器可以替换它们，且不需要导入 Rank 业务类型。

Harness 集成实现公共 `harness.Adapter` 接口，并通过稳定 Runner 类型注册。内置 Registry 包含确定性 Demo Adapter、第一方 DSH Adapter、可解析原生事件的 Claude Code、Codex 与 Hermes Adapter，以及 Pi 的无 Shell 部署 Adapter。Codex 消费临时的 `exec --json` 运行，Claude Code 消费不持久化 Session 的 `stream-json` 运行，Hermes 消费 usage report。原生 Codex 与 Claude Code 运行会复用启动 executiond 用户的 CLI 认证，同时保持每个任务的工作区与 Harness Home 隔离。默认 Codex 运行忽略用户配置，Claude Code 使用安全模式，两者都把冻结的 Agent 模型作为单次调用参数；绑定模型连接的 Codex 则生成私有的逐任务 Provider 配置。

`/v1/model-catalog` 为 DeepSeek、MiniMax、智谱 GLM、OpenAI、Claude 和 Kimi 提供官方连接默认值：端点、协议、可选模型 ID、美元/百万 Token 默认价格、计价说明、来源链接与更新时间。MiniMax、智谱和 Kimi 默认使用中国区官方 OpenAI 兼容端点；国际站仍可作为自定义连接使用。人民币官网价格按目录固定汇率 `¥7.00 = $1` 归一化为美元估算，并在每个模型的计价说明中披露，保证跨 Provider 图表使用同一币种。Chat Completions、Responses 与 Anthropic Messages 连接分别通过原生模型目录和最小推理请求验证。模型目录同步成功时替换连接发现列表；同步失败时保留上次成功结果；Provider 不支持 `/models` 时仍可通过最小请求验证显式填写的模型。连接可以覆盖任一继承价格，但不会修改本地目录。成本解析仍按 Provider 实际返回、连接覆盖、目录默认的顺序执行；缺少用量或价格时保持未知。

模型连接通过 `/v1/model-connections` 管理，`/v1/model-catalog` 提供内置 Provider 端点、模型与默认价格元数据。连接可以覆盖自身模型的价格。元数据保存在 execution Repository，API Key 单独保存在本地凭据目录，且不会序列化进执行规格、事件或产物。验证会在可用时同步 `/models`，并按所选协议执行一次最小请求。DSH 支持三种协议，Hermes 支持 Chat Completions，Codex 支持 Responses，Claude Code 使用自己的 CLI 登录。

执行成本优先使用 Provider 返回的实际费用，其次使用匹配的连接价格，最后使用内置目录价格。计算所得成本会冻结价格快照并标记为估算。缺少用量或匹配价格时，成本保持未知，不会套用其他 Provider 的价格或生成虚假估算。

`GET /v1/executions/{id}/events` 以 SSE 暴露持久化的单次执行事件日志。每条事件包含单调递增序号、尝试次数、类型、状态、可选数据与时间戳。客户端重连时通过 `Last-Event-ID` 或 `?after=` 重放已持久化的生命周期与 Harness 进度，再继续追踪实时事件。
