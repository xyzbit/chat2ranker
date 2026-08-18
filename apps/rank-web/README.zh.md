# Rank Web

[English](README.md) | 中文

本目录将承载 chat2ranker 的 Control DSH 产品组合。

应用只负责浏览器入口与 DSH 插件组合。实验状态、调度、结果和 Runner 生命周期由 [`rank/backend/`](../../rank/backend/README.md) 下的 Go Rank 服务负责。

初始组合将挂载 Rank Control Host 插件、Rank Client UI 插件、持久化控制 Session 预设和 Rank 实验 Skill。它不得在 Control DSH 进程内启动被测 Agent。
