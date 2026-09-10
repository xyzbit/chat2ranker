# Agent Note：持久的调用方 Function 与审批往返

状态：已实现

[English](2026-09-09-caller-function-round-trip.md) | 中文

## 问题

已部署 Agent 可以返回文本或受 Schema 约束的 JSON，但无法请求由外部调用方持有的 Function。如果把调用方 Function 当作 Agent 内建工具，Worker 就会获得其不应持有的凭据和执行权限。函数结果或审批也需要与精确调用及版本固定的 Conversation 建立持久关系，而不是作为无结构的后续消息处理。

## 决策

Deployment 开启 `allowCallerTools` 时，`POST /v1/responses` 最多接受 16 个严格 Function 定义。Rank 会规范化并校验每个唯一名称及其 Draft 2020-12 参数 Schema，把定义随 Response 持久化，将请求中的定义纳入幂等摘要，并加入不可变的模型可见 Execution 提示词。公开调用方不能修改 AgentVersion 的内建工具列表。

Agent 通过返回一个精确的内部 JSON 封装请求调用方执行。Rank 会识别该封装，拒绝未声明名称或违反 Schema 的参数，并投影一个带生成式 `call_id` 的 Responses `function_call` 输出条目。Rank 不会执行函数。即使存在可用 Function，普通 Agent 文本仍作为普通输出处理。

调用方通过 `previous_response_id` 提交一个顶层 `function_call_output` 条目，其中包含匹配的 `call_id` 与非空字符串。新 Response 会继承 Conversation 冻结的 Function 定义，并拒绝替换、缺失、重复、过期或非预期结果。Conversation 重建会在下一次 Execution 前记录函数请求及其结果。

带有 `require_approval: true` 的定义会产生 `requires_action` 与待处理审批，而不是已经完成且可执行的请求。`responses:approve` Scope 控制 `/v1/responses/{id}/approval`。审批决定是幂等的持久事件；批准会完成 Response 并允许续接结果，拒绝会取消 Response。审批绝不会授权 Rank 执行调用方代码。

## 验证

Rank HTTP 测试覆盖 Schema 校验、Deployment 策略、持久定义与调用、待处理审批、幂等审批、匹配的函数输出、定义继承、Conversation 历史及无效输出拒绝。组装后的对外服务快照通过 `rankd`、`executiond` 与真实 Worker 进程完成函数请求、受 Scope 控制的审批和结果续接。

## 备选方案

**把调用方 Function 挂载为 Worker 工具。**未采用，因为外部应用持有实现和凭据；Rank 无法根据公开请求安全地制造可执行插件。

**把工具结果作为普通用户文本接受。**未采用，因为服务器将无法证明值对应哪次调用，也无法拒绝过期和重复结果。

**让审批直接执行函数。**未采用，因为审批只是一项策略决定。外部调用方仍负责副作用，并显式提交结果值。

**允许在续接时修改工具定义。**未采用，因为同一个固定版本 Conversation 中的早期调用和后续结果会因此使用不同 Schema 解释。

## 结果

外部系统可以在不向 Agent Worker 暴露凭据的前提下完成调用方持有的 Function 循环，而且每次调用、审批和结果在重启后仍可重建。Function 选择目前使用严格提示词封装，而不是 Harness 原生 Tool Call 通道，因此格式错误的 Function 封装会使 Response 失败，且不支持并行调用。Webhook 交付仍属于独立的后续生命周期工作。
