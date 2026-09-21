# 数据库权限实验 — 2026-09-15

[English](2026-09-15-database-permissions.md) | 中文

完整 minikube 平台通过独立、受限的 Rank 和 Execution 数据库账号完成公开客户端回归。[机器可读证据](2026-09-15-database-permissions.json)记录镜像标识、部署及验证器摘要与检查结果；凭据保留在仓库外。

| 检查 | 观察结果 |
| --- | --- |
| 维护迁移 | 通过独立迁移账号打开 Execution schema 5 和 Rank schema 3；迁移与授权完成前，应用写入者保持停止。 |
| 日常 DML | 真实密码鉴权的 TCP 账号可向拥有者创建的探测表插入、读取、更新和删除记录，序列默认值正常。 |
| 跨服务访问 | Execution 无法读取 Rank 会话及 API 凭据；Rank 无法读取 Execution 凭据及执行记录。 |
| 高权限操作 | 两个日常账号均被拒绝创建 schema、普通表及临时表，不能 ALTER/DROP/TRUNCATE、切换拥有者、创建角色或读取服务器文件。 |
| 迁移元数据 | 日常账号可读 Rank、Execution/River 版本表，写入被拒绝。 |
| 凭据暴露 | 迁移 Secret 仅含数据库 URL；运行中的中控没有数据库凭据及操作签名密钥。应用 Pod 声明不含迁移凭据或 Kubernetes API 令牌。 |
| 完整平台 | 热点请求、两轮 SDK 聊天、直接试用、持久中控会话、S3 读取、SSE 重放、Pod 替换及排队请求恢复全部通过。 |

第一次迁移因为账号缺少数据库 CREATE 而失败；现有 schema 初始化即使面对已有 schema，也检查这项权限。应用写入者未重启。只给迁移账号授予 CREATE 后，维护 Job 成功完成。日常账号仍不拥有 CREATE。

本次使用已有数据的实验数据库，不是空数据库迁移测试。它在一台笔记本和确定性模型服务下验证配置与 PostgreSQL 授权，不证明生产 TLS、身份、密钥轮换、租户行级授权、物理高可用或恶意代码隔离。复现方法见[实验教程](../k8s/README.md#database-permission-experiment)，权限语义由[决策记录](../../../.agents/notes/implemented/architecture/2026-09-15-rank-database-permissions.md)维护。
