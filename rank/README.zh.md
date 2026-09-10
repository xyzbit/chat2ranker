# Rank 产品源码

[English](README.md) | 中文

`rank/` 目录包含产品控制平面与执行平面集成，不包含第二份 DSH 源码。

| 目录 | 所有者 | 职责 |
|---|---|---|
| `api/` | Rank 协议 | Control 与产品 API 定义 |
| `backend/` | Go Rank 服务 | 实验、不可变版本、用例调度、评分、聚合与持久化 |
| `runners/` | Rank 集成 | Harness 预设与执行平面配置 |
| `assets/` | 产品配置 | Control Skill、Preset 源文件与版本化 Judge 默认配置 |
| `deploy/` | 运维 | 本地 Compose 与生产部署清单 |
| `tests/` | 跨进程验证 | 协议、集成与端到端测试 |
| `docs/` | 产品架构 | 系统结构、数据流、目录、图表与决策 |

产品运行时包含四个进程角色：

1. Control DSH 承载用户对话、Skill 与 A2UI。
2. `rankd` 拥有产品状态并提交不可变的候选与 Judge 规格。
3. `executiond` 拥有通用执行状态、持久进度事件、Executor 选择、取消与 Artifact。
4. `execution-worker` 解析一个公共 Harness Adapter，并在隔离本地进程或 Sandbox 中启动它。

被测 DSH 运行时是 Sandbox 内的 Runner 实例。它不与 Control DSH 共享 Cordis Context、Session 存储、DSH Home、工作区、环境或进程。

本地[开发启动器](../scripts/rank-dev.mjs)负责服务与 Vite 子进程，并统一停止它们。Vite 严格绑定 `127.0.0.1:4173`；端口被占用时启动失败，不会切换到其他 URL。使用控制台期间需要保持启动器运行。

运维控制台提供模型连接与默认模型、Agent 版本对比与评测证据、部署创建与发布历史、调用详情、API 凭据撤销，以及失败 Webhook 重投。可选的 Rankling 小精灵从管理页面打开同一会话，并支持保存在浏览器本地的 Codex v1/v2 小精灵导入。`pnpm --filter @xyzbit/chat2ranker-web test:operator-live` 使用已验证并持久化的 DeepSeek 连接与 Judge 绑定，对运行中的本地服务显式执行付费验收；它保留带标签的测试记录，并撤销验收时签发的服务密钥。脚本和会话不接收提供方密钥。

[对外服务发布注册表](../.agents/notes/implemented/architecture/2026-09-08-serving-release-registry-foundation.md)、[Responses 对外服务 MVP](../.agents/notes/implemented/feature/2026-09-09-responses-serving-mvp.md)、[有状态续接](../.agents/notes/implemented/feature/2026-09-09-stateful-response-continuation.md)、[严格结构化输出](../.agents/notes/implemented/feature/2026-09-09-structured-response-output.md)、[冻结式富输入](../.agents/notes/implemented/feature/2026-09-09-frozen-response-input.md)、[调用方 Function 往返](../.agents/notes/implemented/feature/2026-09-09-caller-function-round-trip.md)与[签名 Webhook Outbox](../.agents/notes/implemented/feature/2026-09-09-signed-webhook-outbox.md)支持受治理的文本、图片、文件、Function、审批和异步事件交互，同时不暴露 Rank Run 或私有 Execution Service。Web 控制平面以 Agent 对话为主，将发布版本对比与确认渲染为 A2UI，并提供支持中英文的 Deployment 页面，用于确定性检查、手动发布和 Webhook 端点管理。[Agent 对外服务 API 参考](docs/serving-api.md)定义经过认证的 `/v1/responses` 子集、Deployment 引用、持久化事件、版本固定的 Conversation 续接、富输入、严格输出、Function Call、审批、签名 Webhook、限制与本地开发约束；更完整的 [Agent 生命周期与对外服务提案](../.agents/notes/proposed/feature/2026-09-08-agent-lifecycle-serving.md)负责后续发布自动化和生态协议阶段。
