# 部署

[English](README.md) | 中文

本地开发运行 Control DSH、`rankd`、`executiond`、`execution-worker`、SQLite 业务数据库与文件系统产物。Execution 还支持 PostgreSQL/River、独立 API/Worker 进程及 S3 兼容产物；配置与失败语义由[其 README](../../execution/backend/README.md)定义。[Rank 业务存储](../backend/README.md)支持 SQLite 或显式迁移的 PostgreSQL schema；其编排器与控制会话仍采用单拥有者部署。

Control DSH、Rank、Execution 与 harness 负载使用独立的持久资源和凭据。产品 API 需要置于鉴权与反向代理之后，DSH 可信主机检查不是身份鉴权。Execution 服务令牌保护内部调用，但不鉴权产品用户。

[可运行生命周期案例](../examples/execution-lifecycle/README.md)在本地验证打包后的 Execution 进程。[生产设计](../../.agents/notes/proposed/architecture/2026-09-14-rank-production-execution.md)约束剩余的沙箱与扩缩容工作，其中的[本地 Kubernetes 实验](../../.agents/notes/proposed/architecture/2026-09-14-rank-production-execution.md#local-kubernetes-implementation-experiment)定义 minikube 验收。仅启动集群不能证明应用部署或跨主机容量。

[实验教程](k8s/README.md)提供完整的 minikube 启动路径。[Execution 报告](reports/2026-09-14-execution.md)记录进程/S3 与 fixture 扩容证据；[平台报告](reports/2026-09-14-platform.md)记录 DSH 端点、试用、浏览器及重启恢复结果。
