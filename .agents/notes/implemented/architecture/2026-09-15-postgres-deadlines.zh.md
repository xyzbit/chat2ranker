# Agent Note: 为 PostgreSQL 依赖操作设置时限

Status: implemented

[English](2026-09-15-postgres-deadlines.md) | 中文

## 问题

数据库失联可能让 API 读取一直等待，直到客户端断开。连接超时不能限制连接池竞争或已建立连接上的 I/O。给整条 HTTP 请求设置短时限，又会中断正常的同步模型调用和 SSE 订阅。

## 决策

Rank 与 Execution 通过 `-database-timeout`（默认五秒）限制 PostgreSQL 连接获取、查询 I/O 和完整应用事务。连接获取和查询 I/O 各有一份预算；非事务操作可能耗用两份预算，一个请求也可能包含多个操作。调用方更早的截止时间始终有效。显式迁移使用同一配置限制完整事务；运维可以仅为迁移进程选择更长的值，无需修改接流量的 Pod。

共享 Go 辅助包 `pgtimeout` 使用 pgx 的连接获取与查询 tracing 钩子附加操作上下文，在获取结束或查询关闭时释放计时器。返回的行在关闭前仍受时限限制，但不会取消调用方的父上下文。建立连接也有明确超时。它不包装任意底层 pgconn 调用、COPY 或 batch API；新增这些路径时必须提供显式截止时间。当前应用 SQL 使用 Exec/Query/QueryRow，多语句应用操作及迁移还具有完整上下文时限。

Rank 保留 SQL Repository，使用 pgx 官方的 `OpenDBFromPool` 适配器。连接上限只由 pgx 管理；`database/sql` 不保留空闲连接，也不设置第二个有限连接池。增加第二个有限上限会引入另一处没有操作时限的等待。WithinTx 将有界上下文传给回调，每个回调查询使用事务剩余预算。关闭 Rank Store 时同时关闭 SQL 外观和所属连接池。Execution 凭据存储与 River SQL 共享已配置的 pgx 连接池。

未经业务包装的截止时间错误返回 HTTP 503 `dependency_timeout`，沿用管理或接入 API 各自的响应格式。已有应用错误保留自己的状态。凭据查询失败作为依赖错误传播；只有凭据不存在、已撤销或不匹配才返回密钥无效。连接被拒绝或服务端断开仍可能在超时前返回其他服务端错误。已经打开的 SSE 流无法再修改 HTTP 状态；数据库操作失败时流结束，消费者从最后提交的游标重连。流和模型调用没有新增五秒的整体寿命限制。

超时不证明事务未提交，也不授权重放模型或工具操作。调用方保留逻辑请求身份，遵循已有幂等与恢复规则。已接受任务由持久派发和尝试状态管理，独立于 HTTP 客户端寿命。本次不增加自动事务回调重试。

## 考虑过的替代方案

**只设置 PostgreSQL statement_timeout 或 connect_timeout。** 二者都不能覆盖所有客户端连接池等待与已建立连接的网络中断。

**统一设置短 HTTP TimeoutHandler。** 会缓冲响应并改变流式行为；用于同步接入时，还会终止正常的长调用。

**包装所有 Repository 方法或替换 SQL Repository。** 会增加重复策略或持久化重写。维护中的 pgx 钩子与 SQL 适配器配合显式事务上下文即可覆盖当前 SQL 路径。

## 影响

真实 PostgreSQL 测试覆盖连接池耗尽、慢查询、较短调用方时限、schema 写锁竞争及恢复后的正常访问。双向代理先保留真实鉴权连接，再丢弃流量且不主动关闭两端 socket；操作在调用方看门狗到期前返回。HTTP 测试验证两个 API 的明确依赖错误。这些检查不证明数据库高可用。

本地 Kubernetes 清单采用三秒操作预算，显式迁移 Job 使用 45 秒。[依赖实验](../../../../rank/deploy/k8s/README.md#dependency-interruption-experiment)要求数据库消失期间八秒内收到实际 HTTP 服务端错误；十秒客户端超时或传输断开都会使验证失败。[实验报告](../../../../rank/deploy/reports/2026-09-15-database-deadlines.md)记录有界故障响应及完整公开客户端回归。[生产方案](../../proposed/architecture/2026-09-14-rank-production-execution.md)维护剩余故障和部署要求。
