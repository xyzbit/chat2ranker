# Agent Note: 分离 Rank 控制进程与 Harness 执行进程

Status: proposed

[English](2026-08-18-rank-control-and-execution-planes.md) | 中文

## 问题

chat2ranker 使用 DSH 承载产品对话，同时需要评测由 DSH、Pi、Claude Code、Codex、Hermes 及未来 Harness 实现的 Agent。如果在 Control DSH 进程内创建被测 DSH Agent，它会共享其他 Harness 无法访问的 Cordis 服务、Session 持久化、工作区状态、凭据和插件全局状态。这样的结果比较了不同的执行模型，也允许被测代码影响产品控制 Session。

DSH Session 日志保存 Agent 执行历史，但不负责数据集版本、Agent 版本、实验事务、跨 Harness 调度、Judge 结果以及聚合后的成本与通过率记录。

## 提案

Control DSH 将承载持久化实验 Session、Rank Skill、Rank 工具和 A2UI。Go Rank 服务将负责数据集、Agent 配置、实验、运行、调度、评分与聚合。Control DSH 的 Rank 插件将通过版本化线协议调用该服务。

每种被测 Harness 都将实现同一 Runner 协议，并在隔离的进程、容器或集群任务内执行。DSH、Pi、Claude Code、Codex 与 Hermes 是平级 Runner 实现。被测 DSH 实例在 Runner 进程内创建自己的 Agent，不与 Control DSH 共享 Cordis Context、Session 存储、DSH Home、工作区、环境或凭据。

Rank Worker 将租用不可变执行规格，创建 Sandbox，转发标准化的有序事件，并保留 Harness 原生日志。Judge 任务使用独立执行，不复用 Control DSH Session 或被测 Agent 进程。

产品代码将隔离在 `rank/`、`packages/rank/` 和 `apps/rank-web/` 下。只有在产品证明缺少扩展点时，才修改 DSH 上游核心包。

## 考虑过的替代方案

**通过 Control DSH 的 `ctx.agents` 服务创建被测 DSH Agent。** 这种方案原型代码最少，但隔离方式不一致，并允许被测全局状态影响控制进程。

**由 DSH 负责 Rank 业务状态与调度。** 这种方案会把 Session 日志用于其职责之外，并把跨 Harness 实验语义耦合到一种 Runner 实现。

**不定义统一 Runner 协议，只启动任意 CLI 命令。** 这种方案保留进程隔离，但无法保证可比较的生命周期事件、取消、用量、产物或终态。

**把 Rank 保留为 Control DSH 进程内的 TypeScript 插件。** 执行仍可放在外部，但长时间调度、租约、数据库事务和 Worker 协调会继续耦合到对话宿主生命周期。

## 验收标准

- Control DSH 通过版本化进程 API 与 Rank 通信，且不能直接启动被测 Agent。
- `rankd` 在创建逐用例执行前冻结数据集版本和 Agent 版本。
- DSH 与至少一个确定性的 Mock Runner 通过同一协议一致性测试。
- 每个用例和 Judge 调用都在隔离进程或容器中运行，并使用作用域受限的工作区与凭据。
- Rank 独立于 DSH Session 日志持久化业务记录，同时保留指向原生执行日志的关联。
- 端到端测试通过真实进程入口证明对话操作、运行分发、事件流、评分、聚合和结果投影。

## 风险

进程与协议边界增加初始实现和部署工作。事件顺序、取消、Worker 丢失、产物发布和终态幂等需要显式约定与一致性测试。

产品 Fork 必须隔离 Rank 装配改动，避免同步 DSH 上游时反复发生大范围合并。若产品需求缺少 DSH 扩展点，仍可能需要范围狭窄且有文档记录的上游补丁。
