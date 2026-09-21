# Agent 试用

[English](agent-playground.md) | 中文

## 会话

试用会话通过与外部 Response 相同的 Execution Service，执行不可变 Agent 版本的模型连接、提示词、工具和 Skill，不创建 Dataset、评测 Run、公开 API Key 或隐式 Deployment。部署试用在首次发送时解析别名，后续发布不会改变该会话固定的版本和发布修订。

Agent 详情打开会话面板，宽屏下与配置并排，手机端占满屏幕。所选版本恢复最新创建的直接试用会话，也可点击新建会话重新开始。历史按 Agent 分组，每条标明版本。关闭面板保留草稿和正在生成的回复。部署路由在当前页打开同一面板。会话链接支持刷新恢复，已有的 `#/trials?conversation=...` 链接打开 Agent 面板。

Rank 中控对话仍是实验工作区。`rank_open_trial` 工具持久化一个携带具体版本和可选草稿的 A2UI 跳转入口；打开入口不会执行 Agent。

统一对话侧栏包含 Rank 和试用两个 Tab。切换时分别保留草稿、历史和正在生成的回复，不会把试用消息传给 Rank。从 Agent 详情发送给 Rank 的请求，会以结构化 mention 带入正在查看的版本；用户明确选择其他 Agent 时以用户选择为准。评测链接创建独立 Rank 实验，选中当前版本及其默认开发集，并预填未发送的 mention 和评测请求。

## 运营端 API

这些路由使用现有本地运营端 API 访问方式，不使用公开调用凭据。试用归属于 Rank 配置的租户。公开调用 Key 无法读取、取消或续接试用 Response。

| 路由 | 行为 |
| --- | --- |
| `GET /api/trials?before=<conversation-id>` | 按创建时间从新到旧返回，每页 30 条；传入最后一个 ID 获取更早历史。 |
| `GET /api/trials/{id}` | 返回固定 Agent、会话以及有序的用户输入和 Response。 |
| `POST /api/trial-responses` | 开始时提供 `agentVersionId` 或 `deploymentReference` 及文本 `input`；续聊提供 `conversationId`、`previousResponseId` 和 `input`。 |
| `GET /api/trial-responses/{id}/events` | 可重放 SSE；通过 `Last-Event-ID` 或 `starting_after` 指定事件游标。 |
| `POST /api/trial-responses/{id}/cancel` | 取消已接受的执行，保留历史。 |

历史支持可选的 `agentFamilyId`、`agentVersionId` 和 `directOnly=true` 筛选，在分页前生效。仅直接试用的历史不包含部署试用。

创建支持 `Idempotency-Key`。相同 Key 和输入返回已接受的 Response；输入改变则冲突。续聊在事务内检查会话最新回复并拒绝并发发送。失败和取消的轮次保留展示，但不进入后续模型上下文；已完成的用户和助手消息沿用现有 Serving 历史机制重放。重启后根据冻结的 Execution 请求和事务提交的进度游标恢复活动 Response。取消先记录意图；Execution 确认终态前，试用带有 `cancelRequested: true` 并保持活动状态。

直接试用使用 `rankd -trial-timeout`（2 分钟）、`-trial-requests-per-minute`（60）和 `-trial-concurrency`（4）。同一配置租户内的直接试用共享这些调用限制。部署试用继承 Deployment 执行策略，但不会发送生产 Webhook 或进入外部调用历史。模型密钥留在 Execution Service。

## 支持的交互

浏览器支持文本对话、适配器进度、取消、重试、复制、配置检查和执行详情。缺少供应商用量或价格配置时，费用保持未知。适配器发出进度时才展示；部分 CLI 仅在完成后返回助手回答。试用继承 Harness 工具权限，不提供调用方函数实现或新的交互式审批协议。与公开 API 一样，这些业务集成需要相应调用方接入。
