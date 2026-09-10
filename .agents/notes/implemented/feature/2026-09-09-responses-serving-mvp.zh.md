# Agent Note: Responses 兼容的 Agent 对外服务 MVP

Status: implemented

[English](2026-09-09-responses-serving-mvp.md) | 中文

## Problem

发布注册表可以确定应由哪个不可变 Agent 版本接收流量，但外部调用方还需要租户授权的调用记录、稳定协议、可重连输出、准入限制与崩溃恢复。把公开流量直接发送给内部 Execution Service，会让调用方选择受信任的 harness、凭据、系统提示词、工具、工作区和 Executor 字段，也会把内部 Worker 事件暴露成应用协议。

## Decision

Rank 暴露 [Agent 对外服务 API 参考](../../../../rank/docs/serving-api.md)及其 [OpenAPI 3.1 文档](../../../../rank/docs/openapi.yaml)定义的受支持子集。`POST /v1/responses`、检索、取消、同步等待、后台派发、创建时流式响应、检索时 SSE 回放和[有状态续接扩展](2026-09-09-stateful-response-continuation.md)，在已实现语义一致时使用 OpenAI Responses 对象与事件命名。参考文档定义富输入、结构化输出与调用方 Function 扩展；未知字段及不支持的 token 上限或非持久化请求都会明确失败。

对外服务凭据只存储 SHA-256 密钥摘要和展示前缀。每个数据平面请求都由它的租户、操作 Scope 和 Deployment 访问列表授权。明文 `rnk_live_` 密钥只在创建凭据时返回一次。本地 `/api` 管理端点是运维平面集成点，不属于公开兼容文档。

请求受理时只解析一次 `deployment@version-or-alias`。持久化 Response 保存请求引用、Deployment、精确 AgentVersion、可选 ReleaseRevision、凭据、规范化文本输入、Metadata、幂等摘要、生命周期时间、Execution 标识符、终止输出、token 用量、成本和失败信息。精确选择器表示 Agent 版本号；它不会创建或引用 ReleaseRevision。受理后的 Alias 移动无法改变执行或幂等回放。

Rank 根据冻结的 AgentVersion 与 Deployment 策略构造私有 Execution 规格。公开输入不能覆盖模型连接、凭据引用、harness、preset、系统提示词、内建工具、Skills、工作区、Executor 或产物路径。Deployment 策略提供每分钟请求数、最大活跃 Response 数、执行超时和调用方指令权限。

运维界面保留 Rank 对话作为主要工作流，并通过一个桌面侧栏暴露手动控制平面页面。“新对话”是操作而不是“工作台”页面；页面导航位于最近会话列表上方，顶部搜索操作只在需要时展开过滤输入。移动端保留紧凑导航并从顶部打开最近会话。Experiment 仍是每个会话背后持久化的选择与 Run 分组，不再成为重复的一级页面。在对话中发布会生成 A2UI 确认卡片，对比 Alias 当前指向与候选 Agent 版本，保留人工覆盖原因或评测证据供复核，并链接到 Deployment 页面。Deployment 页面读取真实发布和策略状态，在没有服务数据时保持流量为空，且只在卡片无法承载复杂操作时打开详情界面。新增控制平面文案支持中文和英文，但不会翻译已持久化的会话内容。

准入检查与 Response 插入位于同一个 SQLite 事务中。限制的作用域是凭据与 Deployment，并通过复合索引覆盖请求时间窗口与活跃状态查询。只有规范化请求摘要匹配时，凭据范围内的幂等键才会返回原 Response；不同 Payload 会收到冲突。Execution 提交使用 `response:<response-id>` 作为私有幂等键。

公开事件在交付前持久化，并带有 Response 内单调递增序号。创建流与 `GET ...?stream=true` 只投影持久化 Responses 事件；`starting_after` 和 `Last-Event-ID` 从已提交序号之后恢复。Rank 启动时会派发持久化的排队中及执行中 Response，Execution 幂等性保证恢复时重新连接到同一次调用。

Schema 遵循发布注册表的双标识符决策：高频关系使用整数物理键，API 凭据与 Response 暴露带前缀的 UUIDv7 标识符。ReleaseRevision 物理键使用普通 `INTEGER PRIMARY KEY`，而不是 SQLite `AUTOINCREMENT`，因为 Repository 内部 Row ID 不要求永久禁止复用。

手动页面提供模型连接验证与默认模型绑定、不可变 Agent 版本对比与评测证据、Deployment 创建与标签内发布历史、按需读取事件的有界调用记录、脱敏凭据列表与撤销，以及失败 Webhook 投递重试。调用游标在公开 ID 背后使用物理整数排序；发布游标使用标签内的修订索引。列表不包含请求全文和附件字节。撤销会拒绝新的授权，但不会取消已经接收的任务。手动重投保留事件身份和 Payload，并要求端点处于启用状态。

Agent 草稿在打开模型设置并选用已验证连接时保留配置。连接元数据先保存，再验证；验证失败会刷新可见状态，不会显示为已验证。打开评测证据时，先选中 Run 所属实验，再展示其冻结快照。把对比版本选为主版本时，会清除自我对比。Deployment 展示的公开引用优先使用已发布的 production 标签，否则使用已发布的 latest 标签，避免把尚未发布的 production 引用显示为可调用。

可选的 Rankling 入口在管理页面旁打开已有会话，不创建另一份 Session 或助手。移动端使用限制焦点范围的模态会话；切换完整会话页面会保留已选实验。Codex v1/v2 小精灵导入会验证本地 PNG/WebP 尺寸，仅在浏览器 IndexedDB 中持久化。v2 渲染器使用标准工作与空闲动作行，以及面向指针的视线格；浏览器启用减少动画时冻结动作。会话渲染器会隔离已识别 Rank Message 中格式异常的块，而不是展示整个协议 Payload 或丢弃相邻有效块。

## Verification

Go 集成测试覆盖精确与 Alias 解析、持久化流事件、幂等回放与冲突、认证、Scope 拒绝、请求频率与并发拒绝、取消、严格的未支持字段错误，以及通过重建 Service 恢复活跃 Response。组装进程测试会启动 Rank、Execution Service 与 Worker 二进制，然后创建 Deployment、把版本晋级到 `production`、签发凭据、调用同步与流式 Response、回放幂等请求，并检索持久化结果。基于真实 DeepSeek 的本地调用会使用当前 Control 模型连接验证同一个公开端点。

## Alternatives considered

**暴露 Execution Service。**拒绝，因为它的调用方是受信任 Rank 代码，而且其请求包含特权运行时配置，没有租户、发布或公开生命周期语义。

**创建自定义调用协议。**拒绝，因为严格的 Responses 兼容子集能提供熟悉的请求、响应、状态和流式命名，同时仍允许不支持的行为确定性失败。

**每次重试都重新解析 Alias。**拒绝，因为已受理 Response 可能在发布后跨越 Agent 版本。持久化解析结果与 Execution 幂等性可让重试固定在同一精确版本。

**使用公开 UUID 作为物理外键。**拒绝，因为更宽的高频索引和 Join 没有带来协议收益。UUIDv7 继续作为公开标识符，整数继续作为 Repository 内部关系键。

## Consequences

外部应用可以调用已评测 Agent 版本，无需接触 Rank Run 或私有 Execution API。运维人员可以为同一个 Agent 家族暴露具有不同凭据和创建时策略的多个 Deployment，每个已受理 Alias 请求都会保留其发布来源。对话与确定性页面共享持久化产品状态。缺少价格来源时显示未知，而不是零费用。本地运维端点仍受信任且未经认证，不得公开暴露。产物签名访问、加权路由、PostgreSQL、分布式租约与生产运维认证不在本实现范围内；这些阶段由[更完整的生命周期提案](../../proposed/feature/2026-09-08-agent-lifecycle-serving.md)负责。
