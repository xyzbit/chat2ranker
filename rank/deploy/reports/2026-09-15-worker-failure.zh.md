# Worker 失败与幂等重提验证 — 2026-09-15

[English](2026-09-15-worker-failure.md) | 中文

本地和远程 Execution 都保留已知 Worker 失败的错误、输出、用量与结果产物。[验证证据](2026-09-15-worker-failure.json)包含源码哈希、精确镜像及 minikube 上八项完整平台检查的通过结果。平台使用 PostgreSQL/River、S3 兼容的 SeaweedFS、真实 DSH 进程和确定性本地模型服务。

打包案例请求一个不接受确定性 Harness 输出的 schema。清理后，公开 API 返回 `failed`，保留原始 schema 错误、`{"status":"ok"}`、已知用量及可读取的失败结果产物。重复提交相同请求返回相同 ID，尝试次数为 1。本地与 OpenSandbox 快照均通过 SQLite 和双 PostgreSQL/River Worker 路径。集群验证器使用 S3 重复此场景，随后验证热点收集、多轮聊天、试用、中控对话、SSE/产物读取、Pod 替换和队列恢复。

实验发现了一个 PostgreSQL 特有的重提问题：jsonb 会改写原始 schema 的格式，而应用按字节比较。比较逻辑使用 JSON 值，保留数组顺序和精确十进制值，把对象键顺序与等值十进制表示视为相同输入。定向测试区分超过浮点精度的相邻整数，并拒绝变更的数组或类型。

固定版本的 execd 用 `CommandExecError` 帧报告进程退出码 1，不发送 `execution_complete` 帧。适配器只有在完整读取流、Worker 身份匹配且失败响应一致时，才接受这种正常退出。测试拒绝损坏事件、矛盾终止、异常退出和截断的 HTTP 响应体，包括退出帧之后的中断。传输失败仍报告结果未知，此改动不授权重放。

定向 Go 测试、打包快照、协议 lint 与空白检查通过。文档检查通过 28 项中的 27 项；未修改的根目录 AGENTS 字数仍超限（2,398 对 1,950）。本实验不证明生产身份、跨主机容量、真实供应商行为或 RPM/TPM 限制。复现步骤见[生命周期案例](../../examples/execution-lifecycle/README.md)与[平台教程](../k8s/README.md)。
