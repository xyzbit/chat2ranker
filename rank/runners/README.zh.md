# Runner 适配器

[English](README.md) | 中文

每种受支持的 Harness 都是同一版本化执行协议下的平级 Runner 实现。

Runner 接收一个不可变用例规格，在隔离进程或容器中运行，发送标准化有序事件，保存 Harness 原生日志，并返回一个终态结果。Runner 不能读取 Rank 业务表，也不能复用 Control DSH 进程。

首批实现包括用于确定性跨进程测试的 `mock` 与用于真实 DSH 评测的 `dsh`。Pi、Claude Code、Codex、Hermes 等适配器只有在存在可执行消费方和一致性测试时才新增。
