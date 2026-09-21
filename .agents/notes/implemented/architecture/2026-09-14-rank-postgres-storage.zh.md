# Agent Note: Rank PostgreSQL 业务存储

Status: implemented

[English](2026-09-14-rank-postgres-storage.md) | 中文

## 问题

Rank 需要不依赖平台进程或本地 SQLite 文件的持久业务记录。增加 Execution 队列并不保存 Rank 会话、发布或 Webhook 投递记录。为 PostgreSQL 复制每个 Repository 查询，会产生需要分别修复的两套业务实现。

## 方案

[SQL 存储](../../../../rank/backend/internal/sqlstore/store.go)拥有共享业务查询和独立的 SQLite/PostgreSQL 迁移。查询使用编号参数、可移植冲突处理及空值安全比较。整数列和比较条件中的布尔参数显式转换为 0/1，包括试用历史的仅直连筛选。PostgreSQL 使用自动生成的 bigint 身份，保留公共标识，并为历史查询使用的 JSON 字段建立索引。外键在所有表创建后安装，因此会话/回复、别名/版本之间虽然存在循环引用，关系仍受约束。

PostgreSQL 迁移显式运行，处于事务中，并由所拥有 schema（模式）的迁移锁串行化。API 接流量前核对支持的精确版本，拒绝缺失及未来版本。SQLite 保留开发迁移路径。PostgreSQL 从空数据库启动，不隐式导入 SQLite 记录。

Rank 的短业务写事务共用 schema 专属的 PostgreSQL 咨询事务锁。准入计数、会话头更新、事件序号和发布比较交换在多个数据库连接间保持原子性。模型与 Execution 调用位于事务外。此实现保留首期单编排器部署，不承诺高写吞吐，也不代表后台编排器可以安全复制。

PostgreSQL 服务器启动要求显式内部 action/control/Execution 密钥及持久 Webhook 密钥目录。模型凭据仍属于 Execution。数据库 URL 通过环境输入，不进入命令参数或健康响应。[后端参考](../../../../rank/backend/README.md)定义参数、启动及测试配置。

## 考虑过的替代方案

**复制整个 Repository。** 将业务查询维护工作翻倍，并容易让两个数据库产生不同领域行为。共享查询使迁移和连接差异保持明确。

**引入运行时 SQLite 到 PostgreSQL 的 SQL 翻译器或 ORM。** 给已有的简短 database/sql 查询增加解释或重新映射。显式可移植 SQL 与专用 DDL（数据定义语言）避免了这套机制。

**立即增加细粒度分布式编排器拥有权。** 它本身不能解决持久派发或原始输入恢复。首期串行写事务以平台写吞吐换取简单的准入与事务安全；缩小锁范围需要实测竞争及独立的并发证据。

## 后果

真实 PostgreSQL 测试覆盖多个连接池竞争、准入回滚、会话头冲突、发布比较交换、事件顺序，以及迁移/版本拒绝。同一组应用与 HTTP 测试可使用 SQLite 或隔离的 PostgreSQL schema。试用历史测试覆盖 Agent 家族、精确版本、直连、租户及游标筛选；打包试用快照显式核对所选数据库，并使用对应的内部控制凭据。打包服务/发布快照在 PostgreSQL Rank 与 SQLite Execution 组合下通过，包括重启 Rank 后读取已存回复和 Webhook outbox（发件箱）记录。这些是确定性 Harness 测试，不是真实模型容量测量。

数据库持久化本身不代表 Rank 到 Execution 派发已经完成。[服务派发](2026-09-14-rank-serving-dispatch.md)为 Response 与试用提供冻结请求恢复。[评测派发](2026-09-14-rank-evaluation-dispatch.md)覆盖候选/裁判调用恢复。[结构化历史](2026-09-14-rank-structured-history.md)与[完整平台实验](2026-09-14-rank-platform-lab.md)提供 DSH 续聊和 Kubernetes 恢复证据。运行时隔离仍属于[生产提案](../../proposed/architecture/2026-09-14-rank-production-execution.md)。
