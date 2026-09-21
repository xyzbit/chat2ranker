# 模型请求预算验证 — 2026-09-15

[English](2026-09-15-model-budget.md) | 中文

[验收记录](2026-09-15-model-budget.json)包含精确镜像、源码摘要和九项通过的 minikube 平台检查。DSH 执行的模型请求上限为一次，收到 HTTP 429 后以 `execution_model_budget_exhausted` 停止，确定性供应商日志中只有一次请求。重复提交保留 attempt 1 与冻结上限，运行时清理完成。

远程命令流可能在终态帧之后发送传输层 `ping`。Executor 忽略这种保活，继续拒绝结束后的输出、矛盾终态、格式错误数据及截断的 HTTP 响应。真实 DSH 集成确认失败结果在清理后仍保留产物。[预算决策](../../../.agents/notes/implemented/architecture/2026-09-15-execution-model-budget.md)定义计数、能力协商与已知用量保留规则。

完整平台通过热点收集、结构化多轮聊天、Agent 试用、中控历史、S3 证据、事件重放、Pod 替换和排队任务恢复。远程打包快照在 SQLite 及 PostgreSQL/River 双 Worker 模式下通过。同一 Sandbox 镜像通过可写存储限制、网络出口与受保护寿命实验，包括脱离的工作运行期间停止生命周期服务。

Executor 测试和契约 lint 通过。文档检查通过 28 项中的 27 项；根目录 `AGENTS.md` 仍有既有字数超限：2,398 词，高于 1,950 词上限。实验使用真实 DSH 进程和本地确定性模型服务，不证明真实供应商行为、共享 RPM/TPM 限制、生产身份、跨主机容量或高可用。[生产方案](../../../.agents/notes/proposed/architecture/2026-09-14-rank-production-execution.zh.md#后续实施顺序)定义后续实施顺序。
