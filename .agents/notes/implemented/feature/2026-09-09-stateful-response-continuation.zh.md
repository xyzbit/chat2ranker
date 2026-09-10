# Agent Note: 有状态 Response 续接

Status: implemented

[English](2026-09-09-stateful-response-continuation.md) | 中文

## 问题

对外服务 MVP 会为每个已受理 Response 冻结一个 AgentVersion，但彼此独立的 Response 无法保留多轮交互。如果每一轮都重新解析 `production`，发布后同一段用户会话可能跨越不同 Agent 版本；如果允许同一个前序 Response 同时产生两个续接，则会形成分叉历史或运行相互冲突的任务。

## 决策

每个新受理的 Response 都属于一个持久 Conversation。Conversation 属于租户范围、绑定一个 Deployment，并固定首个 Response 选中的 AgentVersion 与可选 ReleaseRevision。公开 Response 对象返回 Conversation 标识符和可为空的 `previous_response_id`。

`previous_response_id` 只能指向同一 Conversation 中最新且已经完成的 Response。续接请求必须通过同一租户和 Deployment 的鉴权。即使 Alias 已经移动，Alias 选择器仍沿用 Conversation 固定的版本，并返回 `deployment.resolution_kind: conversation`；精确选择器若指向其他 AgentVersion，则请求失败，而不会迁移 Conversation。

准入在同一个 SQLite 事务中插入 Response，并通过 compare-and-swap 推进 Conversation 当前指针。一个部分唯一索引保证每个 Conversation 最多只有一个处于 queued 或 in_progress 状态的 Response。系统先检查幂等重放，再校验当前指针，因此已经受理的续接在指针推进后重试时仍会返回原 Response。

执行输入从有序的持久 Response 链重建。每个已完成 Response 依次贡献其规范化用户输入和最终助手输出，最后追加本轮用户输入。如果前序 Response 尚未完成、已不再是当前指针、属于其他租户或 Deployment，或者是 Conversation 迁移前的历史记录，系统都会在启动 Execution 之前拒绝请求。

Conversation 和 Response 行使用整数物理主键进行关联，并在 API 暴露带前缀的 UUIDv7 标识符。Conversation 当前指针引用 Response 物理主键；Response 上的前序与 Conversation 关联保持可空，使已有 SQLite 数据库无需重写历史记录即可迁移。

## 验证

HTTP 集成覆盖先通过 `production` 创建首个 Response，再把该 Alias 推进到另一个 AgentVersion，并验证续接仍保持原版本和发布沿袭。测试同时固定历史重建、续接幂等重放、过期指针拒绝和精确版本不匹配拒绝。由 Rank、Execution Service 和 Worker 组成的应用快照会调用公开续接接口，并验证返回的关联与包含历史的执行输出。

## 曾考虑的替代方案

**每一轮都重新解析请求中的 Alias。** 否决，因为晋级可能在同一 Conversation 内静默改变行为，并使其历史无法归因到一个不可变 Agent 版本。

**在每个 Response 上保存完整对话记录。** 否决，因为重复历史会产生平方级增长，并形成多个可写副本。使用有序前序关联与 Conversation 当前指针，可以保留唯一的持久链。

**允许从同一前序并发分支。** 否决，因为当前公开 API 只暴露线性 Conversation，并没有分支选择或合并语义。单活写入者让顺序保持确定。

**把长生命周期运行状态放进 Execution Service。** 否决，因为 Execution 负责一次私有执行尝试，而 Conversation 身份、公开鉴权、版本沿袭和 Response 历史属于对外服务领域。

## 后果

客户端可以在不发生版本漂移的情况下续接已完成 Response，也可以安全重试已受理的续接。事务和唯一索引防止两个活跃写入者产生含糊历史。Conversation 迁移之前创建的 Response 仍可读取，但不能作为续接前序。当前会完整重建历史；显式留存、上下文压缩、Conversation 迁移和 Webhook 仍属于后续生命周期工作。
