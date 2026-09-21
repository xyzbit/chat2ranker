# Agent Note: Rank 与 Execution 数据库权限

Status: implemented

[English](2026-09-15-rank-database-permissions.md) | 中文

## 问题

两个服务共用数据库拥有者时，拆分 schema 并不能隔离服务。被入侵的运行进程可以读取另一服务的凭据或篡改迁移元数据；中控与迁移 Job 也可能继承无关的应用密钥。

## 决策

专用 Kubernetes 实验配置四个非超级用户登录账号：`rank_migrator`、`rank_runtime`、`execution_migrator` 和 `execution_runtime`。每个迁移账号只拥有本服务的 schema 和对象。迁移入口在初始化 schema 时检查数据库 `CREATE` 权限，因此迁移账号获得这项权限。日常账号仅获得数据库 `CONNECT`、schema `USAGE`、表 SELECT/INSERT/UPDATE/DELETE、序列 USAGE/SELECT 和所需函数执行权限。它们不能创建临时表、修改结构、切换到拥有者账号或访问另一服务的 schema。迁移版本表只允许 SELECT。

[配置程序](../../../../rank/deploy/k8s/database_roles.py)在维护期间、应用生产者和消费者停止后，通过专用 PostgreSQL 管理员执行。它只移交两个应用 schema 中的表、序列、函数和声明类型，绝不全局重新分配管理员的对象。检测到非预期角色成员关系时拒绝继续。拥有者的默认权限覆盖后续迁移创建的对象；迁移结束后、应用启动前重新限制元数据权限。专用数据库中移除 PUBLIC 的数据库、schema 和应用对象授权。此脚本不用于安装到已有的共享生产集群。

密码保存在仅文件拥有者可读写的本地文件中并保持稳定。配置过程通过标准输入传入 SCRAM 校验值，抑制可能包含凭据的 SQL 错误输出。初始化管理员只供 PostgreSQL 使用，不进入应用 Pod。每个迁移 Secret 仅包含本服务拥有者的数据库 URL。Rank 中控只接收中控与 Execution 服务令牌，不接收数据库 URL 或操作签名密钥。应用 Pod 的声明配置不挂载迁移密钥，也不接收 Kubernetes API 令牌。这是配置隔离，不是限制集群管理员的准入策略。

## 考虑过的替代方案

**共用数据库拥有者。** 普通应用进程仍能跨服务读写和执行 DDL，独立 schema 无法防止这些操作。

**由日常账号执行迁移。** 需要给长时间运行的服务授予结构创建及版本写入权限。显式维护 Job 已经提供了短期使用这些权限的位置。

**拆分数据库集群。** 后续可以提高故障与管理隔离，但本地实验用独立 schema 和受限凭据即可，无须新增数据库服务。

## 影响

[验证器](../../../../rank/deploy/k8s/verify-database-roles.py)使用四个真实账号通过 TCP 鉴权。它验证拥有者建表、日常 DML 与序列默认权限、跨服务会话及凭据读取拒绝、元数据保护，以及 DDL、创建角色、切换拥有者和读取服务器文件被拒绝。它只检查运行中中控环境变量的名称与迁移 Secret 的字段名，不输出值。组装后的检查具有确定性快照；完整平台验证器另行通过受限数据库连接验证已发布端点、DSH 对话、S3 和进程替换。

实验仍在私网中使用未启用 PostgreSQL TLS 的连接和单个初始化管理员。生产身份、密钥轮换、租户行级授权、数据库及网络策略、备份和不可信运行时隔离需要独立验收。本决策落实[生产方案](../../proposed/architecture/2026-09-14-rank-production-execution.md)中的数据库角色分离，不代表整个方案已完成。
