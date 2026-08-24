# 部署

[English](README.md) | 中文

本地开发运行 Control DSH、`rankd`、`executiond`、`execution-worker`、两个 SQLite Repository 与文件系统 Artifact Store。生产部署可以把各 Repository 替换为 PostgreSQL，并把每次调用分配到隔离容器、Kubernetes Job、Kata Runtime 或远程 Sandbox。

Control DSH、`rankd`、`executiond` 与 Harness 工作负载使用不同的持久化资源与凭据。原始 DSH Web 入口必须位于产品认证与反向代理之后；DSH trusted-host 检查不是认证层。
