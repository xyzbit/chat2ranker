# Agent 对外服务 API

[English](serving-api.md) | 中文

本参考文档定义已实现的本地 Agent 对外服务数据平面。机器可读约定见 [OpenAPI 3.1](openapi.yaml)；更完整的生命周期设计仍记录在 [Agent 生命周期对外服务提案](../../.agents/notes/proposed/feature/2026-09-08-agent-lifecycle-serving.md)中。

## Deployment 引用

`model` 字段采用 `<deployment>@<selector>`。`v3` 这样的选择器解析为该 Deployment 所属 Agent 家族的第 3 个不可变 Agent 版本；`latest`、`staging` 与 `production` 解析到当前 DeploymentAlias 指针。请求受理时会持久化精确 AgentVersion；如果使用 Alias，还会持久化对应 ReleaseRevision，因此后续发布不会改变已受理或回放的 Response。

`research-public@v3` 中的版本是 Agent 版本，不是 ReleaseRevision 序号。ReleaseRevision 是一次 Alias 移动的审计标识符，只有使用 Alias 时才会出现在返回值的 `deployment.release_revision_id` 中。

## 认证与准入

每个数据平面请求都使用 `Authorization: Bearer rnk_live_...`。凭据显式持有 `responses:create`、`responses:read`、`responses:cancel` 与 `responses:approve` scope，并包含可访问的 Deployment 标识符列表。跨租户或超出访问范围的资源会在暴露其存在性可能泄漏其他租户数据时返回 `404`。

Deployment 策略负责限制每分钟请求数、活跃 Response 并发数、执行超时，以及是否允许调用方传入 `instructions`。准入检查与 Response 创建位于同一个 SQLite 事务中，因此被拒请求不会启动 Execution，也不会占用未记录的排队名额。

`Idempotency-Key` 的作用域是 API 凭据。使用相同规范化请求重复调用时返回原 Response，并设置 `X-Idempotent-Replayed: true`；同一键配合不同内容会返回 `409 idempotency_conflict`。

每个已受理 Response 都属于一个持久化 Conversation；该 Conversation 固定到一个 Deployment、AgentVersion 和可选的初始 ReleaseRevision。Conversation 指针每次只允许一个写入方；并发或过期续接会返回 `409`，且不会启动 Execution。

## 操作

| 方法 | 路径 | Scope | 行为 |
|---|---|---|---|
| `POST` | `/v1/responses` | `responses:create` | 创建同步、后台或流式 Response。 |
| `GET` | `/v1/responses/{id}` | `responses:read` | 读取持久化 Response 投影。 |
| `GET` | `/v1/responses/{id}?stream=true` | `responses:read` | 回放并继续接收持久化事件。 |
| `POST` | `/v1/responses/{id}/cancel` | `responses:cancel` | 取消未终止 Response，不删除历史。 |
| `POST` | `/v1/responses/{id}/approval` | `responses:approve` | 批准或拒绝待处理的调用方 Function，Rank 不会执行该函数。 |

本地控制平面还通过 `/api` 创建 Deployment、推进 Alias、签发凭据并管理 Webhook 端点。这些运维端点不属于公开兼容 API；非 Loopback 部署必须把它们置于产品的运维认证和授权层之后。

## 请求子集

| 字段 | 支持情况 |
|---|---|
| `model` | 必填 Deployment 引用。 |
| `input` | 非空字符串，或由 `user` 消息构成的数组；消息可以包含 `input_text`、`input_image` 与 `input_file` 条目。 |
| `stream` | 返回持久化的 Responses 兼容 SSE 事件。 |
| `background` | 持久化并派发后立即返回，不等待终止状态。 |
| `metadata` | 最多 16 个字符串条目；键最长 64 个字符，值最长 512 个字符。 |
| `instructions` | 仅在 Deployment 策略开启时接受，并追加到版本化系统提示词。 |
| `store` | 省略或设为 `true`；已受理 Response 始终持久化。 |
| `previous_response_id` | 从同一 Conversation 中最新且已完成的 Response 继续。 |
| `text.format` | 普通 `text`，或带名称及 Draft 2020-12 Schema 的严格 `json_schema`。 |
| `tools` | Deployment 策略允许时，最多接受 16 个严格的调用方 Function 定义。 |
| `max_output_tokens` | 当前版本拒绝。 |
| 其他字段 | 明确拒绝，不会静默忽略。 |

调用方不能选择模型连接、提供方凭据、harness、preset、系统提示词、内建工具、Skills、工作区、Executor 或产物路径。Rank 会根据已固定的 AgentVersion 与 Deployment 策略构造这些字段，再提交给私有 Execution。

## 示例

```sh
curl http://127.0.0.1:8787/v1/responses \
  -H "Authorization: Bearer $RANK_SERVING_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: example-1" \
  -d '{"model":"research-public@production","input":"Summarize this deployment.","stream":false}'
```

已完成对象包含标准 Response 字段，还通过 `deployment` 扩展返回请求引用、解析类型、精确 AgentVersion 和可选 ReleaseRevision。`cost.known` 用于区分已经计量或配置的金额与缺少定价数据的情况。

结构化输出需要把 `text.format.type` 设为 `json_schema`，提供 1 至 64 字符的 `name`、对象 `schema` 和 `strict: true`。外部 `$ref`、`$dynamicRef` 与 `$recursiveRef` 会被拒绝。Rank 会把规范化格式持久化到 Response 和私有 Execution Spec；Worker 只有在最终输出是唯一一个且通过 Schema 校验的 JSON 值时才会完成执行。Harness 输出无效时 Response 会失败，不会返回违反 Schema 的文本。

## 图片与文件输入

`input_image.image_url` 接受 Base64 Data URL 或公开 HTTPS URL；`detail` 可以省略，目前只接受 `auto`。`input_file` 接受 `file_url` 或 `file_data` 两者之一。`file_data` 可以是原始 Base64 或 Base64 Data URL，并且必须提供 `filename`；本 API 没有 Files 资源，因此会拒绝 `file_id`。

Rank 会在受理 Response 前解码内联内容或下载每个远程来源，计算 SHA-256 内容标识符，并把冻结后的字节随 Response 持久化。带签名的来源 URL 会参与幂等摘要，但不会被持久化。远程下载禁用代理、最多允许三次重定向、全程要求 HTTPS，并拒绝解析到私有、Loopback、Link-local 或其他非公开地址的主机。

每个请求最多接受八个二进制条目，解码后总大小不超过 20 MiB。续接会把此前冻结的全部附件带入本轮 Execution，因此同一组限制适用于 Conversation 的累计附件。JSON 请求体上限为 32 MiB。Execution 会把附件写入私有工作区的 `.rank-inputs` 目录，并把副本作为私有产物保留；公开 API 不暴露原始 Worker 路径或产物 URL。

## 调用方 Function 与审批

每个 `tools` 条目采用 Responses Function 形式，包含 `type`、`name`、可选 `description`、对象 `parameters` 与 `strict: true`。Deployment 必须设置 `allowCallerTools`。Rank 会持久化规范化定义，并把它们加入不可变的模型可见执行提示词。Agent 选择 Function 后，Rank 会根据冻结定义校验函数名与参数对象，并返回 `function_call` 输出条目；Rank 绝不执行调用方代码。

续接该 Response 时，需要发送一个顶层 `function_call_output` 条目；其中 `call_id` 必须匹配前一轮调用，`output` 必须是非空字符串。续接继承相同工具定义，并拒绝替换它们。Conversation 会按顺序记录调用及其结果，因此重启或重试仍能重建同一次往返。

Function 定义可以设置 `require_approval: true`。首个 Response 会停在 `requires_action`，并带有 `approval_status: pending`。持有 `responses:approve` 的凭据调用 `/v1/responses/{id}/approval`；批准会完成 Response 并允许提交匹配的工具结果，拒绝会取消 Response。Rank 会把决定记录为持久事件，但不会把审批视为由自身执行函数的授权。

## 会话续接

将一个已完成 Response 的标识符作为 `previous_response_id` 传入即可追加一轮。Rank 在同一准入事务中持久化前序关系并推进 Conversation 指针，从有序记录重建用户与助手历史，再把历史、本轮用户输入以及 Conversation 中冻结的图片与文件提交给 Execution Service。

```sh
curl http://127.0.0.1:8787/v1/responses \
  -H "Authorization: Bearer $RANK_SERVING_API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: example-2" \
  -d '{"model":"research-public@production","input":"What should I do next?","previous_response_id":"resp_..."}'
```

请求中的 `model` 必须指向同一个 Deployment。续接时 Alias 的后续移动不参与版本选择：新 Response 继承 Conversation 固定的 AgentVersion 和 ReleaseRevision，并返回 `deployment.resolution_kind: conversation`。精确选择器如果指定其他 AgentVersion，会返回 `400 conversation_version_mismatch`；前序 Response 尚未结束时返回 `409 previous_response_not_completed`；前序 Response 已不是 Conversation 当前指针时返回 `409 conversation_head_changed`。

## 流与恢复

创建时设置 `stream: true`，或调用 `GET /v1/responses/{id}?stream=true`。每条 SSE 记录包含整数 `id`、Responses 事件名和 JSON `sequence_number`。使用 `starting_after=<id>` 或 `Last-Event-ID` 重连时，只会发送后续持久化事件。

成功路径依次发送 `response.created`、`response.in_progress`、输出条目与内容部分生命周期事件、一个或多个 `response.output_text.delta`，以及 `response.completed`。失败发送 `response.failed`，取消发送 `response.cancelled`；两种终止事件都携带持久化 Response 投影。Worker 日志、重试、沙箱诊断、原始凭据和内部 Execution 事件不会进入公开流。

Rank 重启后会从 Repository 恢复排队中和执行中的 Response。提交使用由 Response 派生的幂等键，因此恢复时会重新连接到同一个 Execution，不会启动第二次调用。

## 签名 Webhook

Webhook 在控制平面按 Deployment 配置，不由单次 `/v1/responses` 请求临时指定。`POST /api/webhook-endpoints` 接受租户、名称、公开 HTTPS 地址、一个或多个 Deployment 标识符，以及 `response.completed`、`response.failed`、`response.cancelled`、`response.requires_action` 的子集。返回的 `whsec_...` 签名密钥只展示一次。Rank 在 SQLite 中只保存它的凭据存储引用；本地进程 Provider 把密钥写入权限为 `0700` 的私有目录，文件权限为 `0600`。

每个事件都是薄载荷，接收方使用自己的受限 API 凭据获取当前 Response：

```json
{"id":"evt_...","object":"event","created_at":1788940800,"type":"response.completed","data":{"id":"resp_...","object":"response"}}
```

投递遵循 Standard Webhooks。Rank 使用 HMAC-SHA256 对 `webhook-id.webhook-timestamp.raw_body` 签名，并发送 `webhook-id`、`webhook-timestamp` 与 `webhook-signature: v1,<base64>`。接收方必须在解析 JSON 前校验原始请求字节、拒绝过期时间戳，并按 `webhook-id` 去重。

Response 状态变更、持久事件和每个匹配端点的一条 Outbox 记录会在同一个 SQLite 事务中提交。只有 HTTP `2xx` 代表成功；Rank 不跟随重定向。网络错误和非 `2xx` 响应按可配置的跨多日计划重试，并遵守更长的 `Retry-After`；HTTP `410 Gone` 会停用端点。每次投递使用持久租约，因此另一 Dispatcher 或重启后的 Rank 进程只有在租约过期后才会重新接管。`/api/webhook-endpoints/{id}/deliveries` 会返回状态、尝试次数、下次尝试时间与最近错误，但不返回载荷或签名材料。

Webhook 出站仅允许 HTTPS，禁用环境代理，在拨号前解析并检查目的地址，拒绝私有、Loopback、Link-local 和其他非公网 IP。生产环境还应通过过滤代理或子网策略隔离出站网络。

## 限制

本实现使用本地 SQLite Repository，并直接调用 Execution Service。Webhook 投递使用单数据库租约，而不是分布式队列。Conversation 迁移、Webhook 手工重放与密钥轮换、公开产物签名、加权 Canary 路由、PostgreSQL 持久化、分布式租约和生产运维认证仍属于生命周期提案的后续阶段。
