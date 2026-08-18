# Rank Web

[English](README.md) | 中文

本目录承载 chat2ranker 的 Control DSH 产品组合与会话优先 UI。

应用只负责浏览器入口与 DSH 插件组合。实验状态、调度、结果和 Runner 生命周期由 [`rank/backend/`](../../rank/backend/README.md) 下的 Go Rank 服务负责。

该组合挂载 Rank Control Host 插件、持久化实验 Session 与 Rank 实验 Skill。React UI 渲染聊天、A2UI 准备与确认卡片、运行摘要、失败用例和受控 Artifact。被测 Agent 始终由 `rankd` 通过 `rank-worker` 启动，不会在 Control DSH 进程内运行。

在仓库根目录运行 `pnpm rank:dev` 即可启动完整本地组合。无密钥与真实 Provider 验收流程见 [`rank/docs/local-acceptance.md`](../../rank/docs/local-acceptance.md)。
