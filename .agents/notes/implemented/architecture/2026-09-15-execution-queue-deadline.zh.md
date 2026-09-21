# Agent Note：Execution 排队截止时间固化

Status: implemented

[English](2026-09-15-execution-queue-deadline.md) | 中文

## 问题

执行超时只约束调用开始后的运行时间。等待 Worker、运行能力或模型额度的任务可能无限期保持已接受状态，并放大扩容需求。

## 决策

准入时按服务端 `-queue-timeout` 上限固定 `Execution.queueExpiresAt`，默认五分钟。`Spec.queueTimeoutMs` 可以缩短等待；零使用服务端上限，负数或溢出值在准入前拒绝。截止时间精度为毫秒，同键重提、API 替换及之后的配置变化都不延长它。运行中的调用仍使用独立的执行超时；排队截止时间限制进入 running 状态的时间，不限制完成时间。

API 协调器独立于 Worker 槽位处理过期队列，包括停用的队列。PostgreSQL 锁定过期记录，在同一事务中追加失败事件并取消 River 投递。SQLite 在同一事务中更新排队记录并追加事件。Worker 领取时独立检查截止时间，在预留额度和创建运行时之前拒绝过期工作；等待数据库锁前采样的旧时间不能放行已过期任务。PostgreSQL 协调器跳过被其他事务锁定的记录；跳过后仍处于 queued 的任务不能被确认投递完成。定期协调决定失败何时可见，领取检查阻止过期启动；数据库/API 停机可能使失败在恢复后才可见。绝对截止时间要求服务与数据库时钟同步。

过期执行保留为 `execution_queue_expired` 失败，attempt 为零，没有运行时分配。同一幂等键重提读取该结果；显式发起新的逻辑请求需要新键。这是执行失败，不是 Agent 质量分数。缺少固定排队时限的记录以 `execution_limits_missing` 失败，升级不会替旧排队记录生成新的等待窗口。PostgreSQL schema 6 要求显式维护迁移；SQLite 记录 schema 10。本地重启保留既有 queued 事实，不追加重复的排队事件。

容量接口将过期或缺少截止时间的排队记录计入 `expired`，从 ready 需求及虚拟模型/租户额度分配中排除。活跃运行时已有的额度预留不变。API 与 CLI 字段见[服务配置](../../../../execution/backend/README.md)。

## 考虑过的替代方案

**只在 Worker 收到投递时处理超时。** 停用或缺失的 Worker 会让调用方无限等待，因此 API 协调独立于消费容量。

**投递或 API 重启时重新计时。** 持续过载会让请求无限存活，准入必须拥有一个不可变的绝对截止时间。

**复用执行超时，或排队截止后取消运行中的任务。** 排队与执行的起点不同，混用会意外中断已经在准入窗口内启动的请求。

## 后果

SQLite 与真实 PostgreSQL 测试验证并发清理、过期领取、运行中/已取消结果保持不变、缺失时限，以及 River 取消失败时的事务回滚。应用测试验证时限上限、幂等重提和容量排除。[打包生命周期快照](../../../../rank/examples/execution-lifecycle/README.md)用不同默认值重启 API，在 SQLite 和 PostgreSQL/River 中保留相同的 attempt-zero 失败。[集群验证脚本](../../../../rank/deploy/k8s/verify-queue-deadline.py)验证 Worker 为零时替换 API、持久化 SSE，以及恢复 Worker 后新的远程执行。

[本地集群报告](../../../../rank/deploy/reports/2026-09-15-queue-deadline.json)记录了零 Worker 过期、API 替换、新远程调用及完整网关/聊天/重启回归。这一机制约束准入后的 Execution 排队等待，不定义整个 Rank 请求总截止时间、供应商 RPM/TPM 退避、Sandbox 宿主机资源准入或每种拥有者丢失时刻的恢复；这些仍是[生产设计](../../proposed/architecture/2026-09-14-rank-production-execution.md)的独立部分。
