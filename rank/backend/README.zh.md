# Rank 后端

[English](README.md) | 中文

本 Go Module 将产出两个二进制程序：

- `rankd`：Rank API、版本冻结、调度、结果聚合与业务持久化。
- `rank-worker`：任务租约、Sandbox 生命周期、Runner 调用、事件转发与清理。

后端依赖 Runner 与存储接口，不导入 DSH 实现包，也不调用 `ctx.agents.create()`。

Runner 进程只接收不可变执行规格与作用域受限的凭据，不能访问 Rank 数据库。
