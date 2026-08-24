# Agent Note: Rank 在隔离的 Harness Worker 之上拥有编排权

Status: implemented

[English](2026-08-19-rank-isolated-runner-plane.md) | 中文

## Problem

Rank 需要保留完整的 DSH 对话运行时，但不能让控制对话成为被测 Agent 的执行环境。候选用例之间、候选用例与评分器之间、以及它们与控制会话之间都不能共享上下文、文件、凭证或原生历史。同时，Rank 需要用统一业务记录比较不同 Harness，并保留各 Harness 的原生诊断信息。

## Decision

`apps/rank-web` 承载持久化 Control DSH Session 和 Rank 专用控制工具。这些工具调用版本化 Go API，不能启动被测进程。浏览器只能通过签名的结构化操作启动 Run。

Go `rankd` 进程拥有不可变的数据集与 Agent 快照、Run 状态、用例并发、评分策略、聚合和 Artifact 授权。它的应用层依赖 Rank Repository 接口和版本化 Execution Service Client，不依赖 DSH 包或进程启动器。

评估策略是挂在 DatasetVersion 上、并冻结到每个 Run 的不可变 `EvaluatorVersion`。一个 Run 展开为每个用例一条 `RunItem`，以及每个 Case × Trial 组合一条 `RunTrial`；默认每个用例独立运行五次。必需的确定性指标先于任何 LLM 评分执行。确定性检查未通过属于有效质量失败，不消耗 Judge Token。通过门控的候选结果再按单个 Rubric 评分项执行 Judge，并冻结权重、关键项标记、单项阈值、Judge Harness 与 Judge 模型。

每次候选调用和 Judge 调用都经过版本化 Execution Service HTTP 协议。独立的 `executiond` 进程先通过自己的 Repository 持久化 Execution，再由选定的 Executor 启动新的 `execution-worker`，并分配私有 workspace、私有 Harness Home 和 execution ID。Worker 解析公共 `harness.Adapter` Registry。本地 Executor 直接支持确定性的 mock 和仓库内已构建的 DSH CLI。Pi、Claude Code、Codex 与 Hermes 使用同一个无 Shell 参数数组 Adapter。Judge 是第二个独立 Execution，只接收用例标准和候选输出。

Rank 根据冻结的分数阈值推导 Rubric 是否通过，而不直接信任不受约束的 Judge 布尔值。Judge Prompt 把候选输出视为带分隔符的不可信证据。格式错误或 `unknown` Judge 输出属于评分失败，而不是质量失败；候选执行错误耗尽重试后属于基础设施失败。只有有效质量 Trial 进入通过率分母。Rank 另外公开稳定用例（所有计划 Trial 均有效且通过）、pass^3、候选成本、评分成本、异常计数和 `evaluationComplete`。

执行生命周期与 Harness 进度保存为单次执行的有序事件日志。Execution Service Client 从序号零开始重放日志并追踪 SSE，因此快速完成不会与订阅产生竞态。`rankd` 把候选与 Judge 事件映射到自己的有序 Run 事件日志；浏览器追踪 Rank SSE，并从最后一个已持久化游标重连。原生进程流不能绕过这两个数据所有者。

不可变 Agent 版本包含 Runner 类型、Preset、System Prompt、模型、工具标识和 Skill 引用。Rank 把这些字段冻结到每个 Execution Spec。DSH Adapter 把模型和 System Prompt 覆盖物化为单次调用本地 Patch，外部命令 Adapter 则接收显式参数占位符。

本地产品通过两个独立 Repository 和两个 SQLite 数据库分别保存 Rank 业务状态与通用 Execution 状态。Execution Service 拥有文件系统 Artifact Store。Worker 到达静止状态后删除 workspace；request、result、stdout、stderr、trace 和原生 Harness Home 继续保留，并通过 Execution Result 上的引用访问。Rank 只保存 Execution 与 Artifact 引用。生产部署可以提供 PostgreSQL Repository，以及 Docker、Kubernetes Job、Kata 或远程 Sandbox Executor，而不修改 Rank 领域代码或 Execution Service 协议。

## Alternatives considered

**在 Control DSH 内运行被测 DSH Agent。** 这种调用路径最短，但会与产品对话共享插件树、Session 持久化、环境和故障域，导致评测污染，并允许被测进程破坏控制状态。

**继续把 Go 进程内 DemoRunner 作为执行架构。** 它适合狭窄的应用层测试，但无法证明进程清理、凭证缩减、Harness Home 隔离、原生 Artifact 或独立 Judge 可见性。

**继续把执行生命周期放在 `rankd` 内。** 这样可以少启动一个本地服务，但会把 Rank 业务持久化与 Sandbox、Harness、Artifact 的生命周期和可用性耦合，也无法让其他产品复用执行平面。

**让 DSH 调度 Rank。** DSH 拥有对话和工具循环语义，不拥有版本冻结、幂等 Run、跨 Harness 策略或业务持久化。把编排权交给 DSH 会让产品状态耦合到一种 Harness，并使其他 Adapter 成为附属实现。

## Consequences

产品的本地进程图更大，启动前需要构建 DSH Libraries 和三个 Go 二进制。默认五次 Trial 会成倍增加候选与 Judge 工作量，因此确认卡必须在调度前展示 Trial 数量和成本预估。两个 SQLite 文件和两套持久事件日志也需要独立迁移、保留与备份策略。相应地，Control DSH 保留原生能力，Rank 仍是评测权威，确定性断言能够减少 Judge 开销，运行异常不会静默污染质量指标，Execution Service 可被 Rank 之外的产品复用，重连客户端能够恢复进度，每个候选与 Judge 都可独立追踪。本地进程隔离是第一个 Executor；更强的容器或集群隔离仍是同一协议背后的部署选择。
