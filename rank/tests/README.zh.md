# 跨进程测试

[English](README.md) | 中文

`contract/` 验证 Go、TypeScript 与 Runner 实现对协议字段和终态的理解一致。`e2e/` 通过真实进程入口启动 Control DSH、Rank 服务以及 Mock 或 DSH Runner。

单元测试与其 Go 或 TypeScript 包放在一起。本目录只承载跨进程或跨语言行为验证。
