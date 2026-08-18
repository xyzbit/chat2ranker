# Rank 协议

[English](README.md) | 中文

本目录将负责 Control DSH、`rankd`、`rank-worker` 和 Runner 进程之间的版本化线协议定义。

首个协议版本将定义：

- 实验、数据集版本、Agent 版本、运行与运行项标识；
- 单个用例的不可变 `ExecutionSpec` 输入；
- 消息、模型调用、工具调用、用量、产物、完成与失败的有序执行事件；
- 包含最终输出、退出状态、耗时、Token 用量、成本和原生日志位置的 `ExecutionResult`；
- Rank 运行的创建、启动、观察、取消与查询操作。

Go 与 TypeScript 代码将从这些定义生成。协议引入后不得维护手写传输 DTO。
