# Agent Note: Rank API 滚动替换与有界 HTTP 排空

Status: implemented

[English](2026-09-17-rank-api-rolling-drain.md) | 中文

## Problem

单副本 `Recreate` Rank API 在替换时使 Nginx 没有可连接的上游。五秒 HTTP 排空会中断较慢响应。直接复制合并进程还会产生重复恢复拥有者，并丢失进程内登录会话。

## Decision

[Rank 进程模式](../../../../rank/backend/README.md#process-modes-and-http-draining)将共享数据库的 HTTP 准入与唯一调度器分开。API 实例不启动服务或评测观察任务，取消路径也遵守这一规则。调度器周期性发现持久工作，不重置自己正在处理的 Run。调度器自身仍以 `Recreate` 替换；仅替换 HTTP 时，已接收的 Execution 任务及其观察器继续运行。

[登录存储](../../../../rank/backend/internal/sqlstore/sessions.go)在 SQLite schema 23 或 PostgreSQL schema 4 中保存 Cookie 和凭据摘要。事务串行执行轮换、过期清理和安装实例级容量检查。每次请求复核实时凭据文件；共享退出保持可撤销，不存原始凭据，也不引入 Redis。内存实现保留为独立测试提供者。

实验 API Deployment 配置零不可用副本、一个额外副本、最短就绪与 preStop 各五秒、可配置的 30 秒 HTTP 排空以及 45 秒 Pod 终止宽限期。HTTP 超时关闭剩余连接，不取消持久远程任务。消费者必须区分流结束与 Response 完成，使用原 ID/游标继续读取。共享事件轮询限制通知者位于另一进程时的读取延迟。

此决策部分替代[平台实验](../architecture/2026-09-14-rank-platform-lab.md)和[管理访问](../architecture/2026-09-15-rank-management-access.md)中的单进程细节；其打包、维护、授权和撤销理由继续有效。[服务恢复](../architecture/2026-09-14-rank-serving-dispatch.md)仍保留唯一观察拥有者。

## Alternatives considered

**仅延长退出等待。** 有助于已接收请求，但 `Recreate` 期间仍没有 API 监听者。

**复制合并进程。** 启动恢复会重置活跃评测状态，并假定唯一拥有者。HTTP 扩容不应顺带改变执行所有权模型。

**增加 Redis 或替换为新的身份服务。** 现有数据库已能提供共享过期、容量和撤销，不需要另一个服务或认证协议。

## Consequences

[可运行滚动快照](../../../../rank/examples/api-rolling/README.md)覆盖评测后发布、真实 DSH Worker、慢请求与超过等待上限的流、持续网关读取、登录保留、共享退出、一次 Execution 尝试及持久 SSE 回放。后端测试覆盖超过五秒的 HTTP 完成、有界关闭，以及 SQLite/PostgreSQL 上的共享会话容量、轮换、撤销和持久摘要。

首次 schema 迁移采用维护路径，可能中断服务。HTTP 滚动保证不覆盖调度器/中控替换、不兼容 schema 变更、额外副本资源不足或物理节点丢失。调度器仍是单拥有者组件，不是新增的分布式调度系统。
