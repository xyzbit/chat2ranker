# Rank 包

[English](README.md) | 中文

本组承载仅供 chat2ranker Control DSH 应用使用的 DSH 扩展。

规划中的包包括 `protocol`、`control-host` 和 `client-ui`。它们把 DSH 对话运行时连接到 Go Rank API，提供 Rank 工具与 A2UI，并投影持久化的 `rank/*` 控制会话事件。它们不得在 Control DSH 进程内执行被测 Harness。

每个包只有在首个消费方和可执行组合存在时才创建。所有包遵循仓库包规则，并依赖 DSH Service Definition，而不依赖具体提供方。
