# 部署

[English](README.md) | 中文

本地开发将使用 Compose 运行 Control DSH、`rankd`、`rank-worker`、PostgreSQL 与产物存储。生产部署为每个用例分配独立 Sandbox，并确保每个 DSH Session 存储只有一个写入者。

Control DSH、`rankd` 与 Runner 工作负载使用不同的持久化资源与凭据。原始 DSH Web 入口必须位于产品认证与反向代理之后；DSH trusted-host 检查不是认证层。
