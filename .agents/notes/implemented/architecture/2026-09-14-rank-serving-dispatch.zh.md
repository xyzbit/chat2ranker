# Agent Note: Rank 服务请求持久派发

Status: implemented

[English](2026-09-14-rank-serving-dispatch.md) | 中文

## 问题

Response 已保存不代表 Execution 已接受它。Rank 可能在提交前退出、在接受后丢失 HTTP 响应，或无法提交返回的 Execution ID。根据可变端点策略重建调用，会改变幂等请求内容。观察连接失败不能证明远端执行失败或已经停止。

## 决策

[服务准入](../../../../rank/backend/internal/app/serving.go)把完整的出站 Execution 请求与 Response、初始事件一起保存。请求包含由 Response 派生的稳定幂等键、Agent 配置、本次追加指令、会话输入、附件和超时。凭据仍通过模型连接 ID 引用。恢复提交这些已存请求字段，不重新读取当前端点策略。冻结请求保留 Agent 版本创建时固定的模型；Execution 验证该模型并执行全局模型额度。

单个 Rank 调度器观察每个活动 Response；纯 HTTP 副本接受并读取持久工作。提交、网络与持久化错误保留活动业务记录以便重新核对；远端终态记录决定完成结果。试用进度在同一事务提交文本、事件与 Execution 序号，忽略已经记录的序号。公开输出和费用与终态业务事件、Webhook outbox（发件箱）一起提交一次。Rank 退出时等待服务观察任务结束，不取消远端调用。

取消操作先记录意图，再联系 Execution，包括 Rank 尚未保存 Execution ID 的情况。核对流程通过幂等提交取回原始 ID，然后转发取消。远端终态尚未确定时，Response 保持活动状态；完成可以先于取消生效。取消是尽力而为，不承诺撤销外部操作，也不保证提交与取消之间完全没有执行。浏览器连接不拥有已接受的执行。

SQLite schema 21 与 PostgreSQL schema 2 增加冻结请求、取消标记和回放游标。已有完成历史仍可读取。启动拒绝缺少冻结请求的活动 Response，避免重建不同的工作。[后端参考](../../../../rank/backend/README.md)定义观察与重试配置；[服务 API](../../../../rank/docs/serving-api.md)定义取消响应语义。

## 考虑过的替代方案

**观察连接断开就标记调用失败。** 把传输结果当成执行结果，可能隐藏仍在发生的副作用。核对流程保留不确定性，直到 Execution 给出终态结果。

**每次重启都重建请求参数。** 端点配置或会话增长可能改变请求，使幂等失效。持久出站请求保留 Rank 接受派发时的参数。

**为服务请求另加工作流引擎。** 业务记录已经提供可恢复的活动工作索引。单拥有者观察任务与 Execution 幂等准入覆盖这个生命周期，无需引入另一个调度器。

## 后果

[打包服务快照](../../../../apps/rank-web/tests/serving-recovery.e2e.mjs)在提交前、真实 Execution 接受后但 HTTP 响应被截断时，以及已完成结果提交前，杀掉实际 Rank 进程。它在 SQLite 与 PostgreSQL Rank 上验证重启后的原始 Execution ID、Response 身份与唯一终态事件。该组装场景中的 Execution 使用 SQLite；独立的 Execution/River 验收覆盖其分布式 Worker 路径。数据库测试覆盖 ID 持久化前取消、端点策略变化、观察断开与重复试用进度。

此决策覆盖公开服务与 Agent 试用。[评测派发](2026-09-14-rank-evaluation-dispatch.md)负责候选/裁判恢复，并共享 Execution 观察协议。运行时隔离与安全自动扩缩容仍属于[生产提案](../../proposed/architecture/2026-09-14-rank-production-execution.md)。仍要求一个调度拥有者；[HTTP 副本](../bug-fix/2026-09-17-rank-api-rolling-drain.md)不拥有观察任务。
