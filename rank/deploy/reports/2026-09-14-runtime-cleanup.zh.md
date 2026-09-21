# 运行时清理升级验收

[English](2026-09-14-runtime-cleanup.md) | 中文

[脱敏报告](2026-09-14-runtime-cleanup.json)记录了现有 `rank-dev` minikube 平台从 Execution PostgreSQL schema 1 到 schema 2 的维护升级。旧网关、Rank、中控和 Execution 进程停止后，顺序执行的 Job 迁移 Execution 与 Rank。平台随后恢复两个 Worker，并保留数据库、中控 PVC 和 S3 产物。此流程明确中断实验服务，不是无停机迁移。

完整平台验证通过了每日热点、两轮 OpenAI SDK 聊天、结构化历史、直接试用筛选、终态 SSE 回放、S3 证据读取、持久中控会话、Rank/中控 Pod 替换，以及请求在 Worker 缩到零、Rank 重启后扩容完成。已完成的 Execution 没有待清理状态。响应来自确定性本地模型服务；新闻来源是注入的测试数据，不是实时新闻。

针对真实 PostgreSQL 的测试另行验证了未知失败后保留模型及租户容量、拒绝过期确认、拒绝过早完成或取消，以及可重复的 schema 迁移。SQLite 与应用测试覆盖相同的清理状态语义。打包 Execution 生命周期快照在 SQLite 与 PostgreSQL/River 模式中通过，包括运行中待清理和完成后已清理状态。

本轮没有新增[此前平台报告](2026-09-14-platform.md)以外的浏览器验收证据，也不证明完整运行时隔离、本地拥有者丢失后的自动清理、生产身份分离、自动扩缩容或物理跨主机执行。这些要求仍保留在[生产提案](../../../.agents/notes/proposed/architecture/2026-09-14-rank-production-execution.md)中。
