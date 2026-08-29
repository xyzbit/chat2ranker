# Rank 后端

[English](README.md) | 中文

本 Go Module 产出一个二进制程序：

- `rankd`：Rank API、版本冻结、用例调度、评分策略、结果聚合与业务持久化。

后端依赖 Rank Repository 接口和版本化 Execution Service Client，不导入 DSH 实现包、不启动 Harness 进程，也不调用 `ctx.agents.create()`。

`rankd` 冻结 DatasetVersion、AgentVersion 与 EvaluatorVersion，再把 Run 展开为 Case × Trial 记录。每个 Run 始终只绑定一个 Agent 版本；对比请求持久化一个 RunGroup，并在调度前以同一事务为每个选定 Agent 版本创建一个独立子 Run。每个 Trial 先提交候选调用并执行必需的确定性检查，仅在需要时提交独立的 Rubric Judge 调用；默认每个用例运行五次。Rank 分离质量失败、基础设施失败与评分失败，聚合有效 Trial 通过率、稳定用例、pass^3，以及候选/评分成本，并且只保存 Execution 与 Artifact 引用。

浏览器客户端追踪 `GET /api/runs/{id}/events`，不轮询 Worker 进程。本地持久化适配器使用 SQLite；PostgreSQL 可以实现同一套 Repository 接口，而无需进入 Rank 领域层或应用层。
