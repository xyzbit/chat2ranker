# DSH Runner

[English](README.md) | 中文

DSH Runner 在分配到的 Sandbox 内启动被测 DSH 运行时。

Wrapper 创建用例专属 DSH Home 与工作区，加载冻结的 Agent 配置，在该 Runner 进程内创建被测 Agent，执行一个用例，并把原生 Session 日志与用量记录转换为 Rank 事件和产物。

Wrapper 可以通过 DSH CLI 或 SDK 驱动运行时。两种机制都必须位于 Runner 进程内，不能连接 Control DSH 的 Cordis Context。
