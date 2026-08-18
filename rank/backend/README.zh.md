# Rank 后端

[English](README.md) | 中文

本 Go Module 产出两个二进制程序：

- `rankd`：Rank API、版本冻结、调度、结果聚合与业务持久化。
- `rank-worker`：根据版本化 JSON 请求执行一次隔离的候选 Agent 或 Judge 任务，并负责输出限额、进程树取消、Artifact 收集与清理。

后端依赖 Runner 与存储接口，不导入 DSH 实现包，也不调用 `ctx.agents.create()`。

每次候选 Agent 与 Judge 执行都会由 `rankd` 启动新的 `rank-worker` 进程。Runner 进程只接收不可变执行规格、私有工作目录、私有 Harness Home 与裁剪后的环境变量，不能访问 Rank 数据库。第一版本地采用子进程隔离；后续可将同一 JSON 协议搬到容器或 Kubernetes Job，而无需改变 Rank 领域模型。
