# Agent Note: Rank 在隔离的 Harness Worker 之上拥有编排权

Status: implemented

[English](2026-08-19-rank-isolated-runner-plane.md) | 中文

## Problem

Rank 需要保留完整的 DSH 对话运行时，但不能让控制对话成为被测 Agent 的执行环境。候选用例之间、候选用例与评分器之间、以及它们与控制会话之间都不能共享上下文、文件、凭证或原生历史。同时，Rank 需要用统一业务记录比较不同 Harness，并保留各 Harness 的原生诊断信息。

## Decision

`apps/rank-web` 承载持久化 Control DSH Session 和 Rank 专用控制工具。这些工具调用版本化 Go API，不能启动被测进程。浏览器只能通过签名的结构化操作启动 Run。

Go `rankd` 进程拥有不可变的数据集与 Agent 快照、Run 状态、并发、取消、恢复、聚合和 Artifact 授权。它的应用层依赖领域 Repository 和 Runner 接口，不依赖 DSH 包。

每次候选执行和 Judge 执行都经过版本化 JSON Worker 协议，并获得新的 `rank-worker` 进程、私有 workspace、私有 Harness Home 和 execution ID。本地启动器直接支持确定性的 mock 和仓库内已构建的 DSH CLI。Pi、Claude Code、Codex 与 Hermes 使用同一个无 shell 参数数组适配器。Judge 是第二次独立执行，只接收用例标准和候选输出。

本地产品使用 SQLite 保存业务状态，使用文件系统 Artifact Store 保存执行数据。Worker 到达静止状态后删除 workspace；request、result、stdout、stderr、trace 和原生 Harness Home 继续保留，并通过 Case Result 上记录的 Artifact 引用访问。生产部署可以替换 Repository 和启动器，而不修改领域模型或 Worker 协议。

## Alternatives considered

**在 Control DSH 内运行被测 DSH Agent。** 这种调用路径最短，但会与产品对话共享插件树、Session 持久化、环境和故障域，导致评测污染，并允许被测进程破坏控制状态。

**继续把 Go 进程内 DemoRunner 作为执行架构。** 它适合狭窄的应用层测试，但无法证明进程清理、凭证缩减、Harness Home 隔离、原生 Artifact 或独立 Judge 可见性。

**让 DSH 调度 Rank。** DSH 拥有对话和工具循环语义，不拥有版本冻结、幂等 Run、跨 Harness 策略或业务持久化。把编排权交给 DSH 会让产品状态耦合到一种 Harness，并使其他 Runner 成为附属实现。

## Consequences

产品的本地进程图更大，启动前需要构建 DSH libraries 和两个 Go 二进制。相应地，Control DSH 保留原生能力，Rank 仍是评测权威，每个 Case 与 Judge 都可独立追踪，Harness 特有故障也可诊断而无需成为 Rank 的存储模型。本地进程隔离是第一个启动器；更强的容器或集群隔离仍是同一协议背后的部署选择。
