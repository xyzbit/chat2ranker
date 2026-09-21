# Agent Note: 运行时清理保留 Execution 容量

Status: implemented

[English](2026-09-14-execution-runtime-cleanup.md) | 中文

## 问题

调用失败后，未确认的运行时可能仍然存活。只统计运行中与取消中记录，会在拥有者丢失时释放模型与租户容量，即使旧调用仍在占用资源。已记录的超时或 River 终态都不能证明运行时已经停止。

## 决策

Execution 在尝试进入运行中时持久化 `runtimeCleanupPending`，随后才调用 Executor。应用只在 Executor 返回且确认停止后清除此状态；包装 `ErrRuntimeStopUnconfirmed` 的错误会保留它。确认受尝试代次约束并保持幂等，也允许在业务已失败后记录。确认不修改业务结果或 River 投递状态。核对器报告未知结果时保留清理义务，重新投递也不虚构停止证据。

PostgreSQL 准入将运行中、取消中及仍待清理的终态记录计入模型与租户容量。Repository 拒绝尚未清理的完成或取消，失败则保留清理状态。PostgreSQL schema 6 与 SQLite schema 10 为已有活跃尝试，以及报告未知结果或未确认停止的失败记录保留清理义务。重复执行已完成的迁移不会覆盖后续清理确认。

[OpenSandbox 适配器](2026-09-15-execution-opensandbox.md)使用此状态核对远程清理。本地 Executor 仍仅适用于可信负载：进程组处理不能证明任意脱离后代进程的隔离。本地拥有者丢失可能让容量长期保留。没有自动到期释放或公开强制释放操作；运维需要调查保留容量，不能假设旧运行时已停止后继续增加容量。

## 考虑过的替代方案

**业务失败或超时就释放容量。** 未知任务仍然存活时，新调用也会获准进入。运行时独立强制执行的寿命可以提供停止证据，单纯的应用定时器不能。

**清理成功前始终让业务响应保持运行中。** 它混淆了未知结果与资源待清理状态，让调用方持续等待而得不到有用结果。独立持久化清理状态可以同时保留两项事实。

## 影响

真实 PostgreSQL 测试验证未知运行时在确认停止前持续阻止模型及租户准入。SQLite/PostgreSQL Repository 测试拒绝过期确认及过早的成功、取消。应用测试区分正常退出后的失败与停止未确认，迁移测试覆盖已有持久记录及重复升级。[打包生命周期案例](../../../../rank/examples/execution-lifecycle/README.md)在 SQLite 与 PostgreSQL/River 模式中记录运行时待清理，以及正常完成、取消后已清理的快照。

[minikube 升级报告](../../../../rank/deploy/reports/2026-09-14-runtime-cleanup.md)记录了 schema 升级及对应镜像通过完整平台回归的证据。
