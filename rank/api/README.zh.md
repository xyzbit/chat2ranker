# Rank 协议

[English](README.md) | 中文

本目录负责 Rank 产品与 Control DSH API 文档。通用执行载荷位于 `execution/backend/contract`，因此不会依赖 Rank 业务类型。

已实现的 HTTP API 提供：

- 实验、数据集版本、Agent 版本、运行与运行项标识；
- 不可变数据集与 Agent 版本的创建和选择；
- 带幂等命令标识的持久 Control DSH 命令；
- Rank 运行的创建、启动、取消与查询操作，其中 `trialCount` 范围为 1–20（默认 5）；
- 支持从单调游标重放的 `GET /api/runs/{id}/events` SSE；
- 冻结的 Evaluator 快照、结构化 Trial/评分项结果、稳定性与 pass^3 聚合、分离的失败计数与成本；
- 失败用例结果与授权 Artifact 读取。

Go HTTP Handler 负责 Rank 传输 DTO，浏览器客户端只维护对应 JSON 调用和 SSE 订阅。通用创建、观察、取消、查询、事件与终态结果契约保留在 `execution/backend/contract` 及其 Go Client 中。

## Evaluator 载荷

`DatasetVersion.rubric` 接受一个 Evaluator 文档。Rank 会将其标准化、分配版本标识，并冻结到每个 Run：

```json
{
  "name": "Citation quality",
  "deterministic": [
    { "id": "json", "name": "Valid JSON", "operator": "json_valid", "required": true }
  ],
  "rubric": [
    { "id": "grounded", "name": "Grounded claims", "description": "Every factual claim has traceable evidence", "weight": 2, "threshold": 0.8, "critical": true }
  ],
  "judge": { "harness": "dsh", "model": "deepseek-chat" },
  "passPolicy": { "rubricThreshold": 0.75 }
}
```

确定性算子包括 `equals`、`contains`、`not_contains`、`regex` 和 `json_valid`。用例也可以声明便捷期望字段 `exactOutput`、`outputContains`、`outputRegex` 或 `jsonValid`；Rank 会把它们转换为必需的确定性评分项。带有 `required: true` 的评分项会门控 Judge 执行。Rubric 是否通过由冻结的分数阈值推导，关键项失败会覆盖加权聚合结果。
