# Runner 适配器

[English](README.md) | 中文

每种受支持的 Harness 都是版本化 Execution Service 协议下的平级 Harness Adapter。

Adapter 接收一个不可变通用调用，在隔离进程或容器中运行，保存 Harness 原生 Trace，并返回一个标准化终态结果。Adapter 不能读取 Rank 业务表、推断实验策略或复用 Control DSH 进程。

公共 Go 接口与 Registry 位于 `execution/backend/harness`。内置实现包括用于确定性跨进程测试的 `mock` 与用于真实 DSH 评测的 `dsh`。Pi、Claude Code、Codex 与 Hermes 通过同一个无 Shell 命令 Adapter 注册，并在配置 JSON 参数数组后变为可用。原生 SDK Adapter 可以替换命令 Adapter，而无需修改 Rank。
