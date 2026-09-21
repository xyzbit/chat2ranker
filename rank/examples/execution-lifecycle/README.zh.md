# Execution 生命周期案例

[English](README.md) | 中文

这个无密钥案例构建并启动实际交付的 Execution 二进制，通过真实进程适配器调用确定性 CLI（命令行界面）。它使用[同一份快照](snapshot.json)检查提交、幂等回放、有序 SSE（Server-Sent Events）、按序号重连、产物读取、队列中取消、执行中取消与运行时清理预留。这是执行协议案例，不是模型质量演示。

## 本地运行

使用 Go 1.26.0 和 Node 24 或更新版本，在仓库根目录运行：

```sh
node rank/examples/execution-lifecycle/verify.mjs
```

验证器拥有临时 SQLite 数据库、私有服务令牌、二进制与工作目录，退出时停止自己的进程并移除这些本地文件。等待中取消场景使用停用的评测队列，实际调用使用在线队列。fixture（测试前置数据）会启动子进程，使打包进程测试能独立验证后代进程确实停止。

## 使用 PostgreSQL 和 S3

通过私有 shell 环境提供 `EXECUTION_TEST_DATABASE_URL`，数据库角色需要创建与删除 schema 的权限。相同命令会启动独立 API 和两个 River Worker，结束后只删除自己随机命名的 `snapshot_*` schema。不要把凭据放入命令行参数或提交到仓库的文件。

验证远程产物存储时，还需提供 `host:port` 格式的 `EXECUTION_TEST_S3_ENDPOINT`、`EXECUTION_TEST_S3_BUCKET`、`EXECUTION_TEST_S3_ACCESS_KEY` 和 `EXECUTION_TEST_S3_SECRET_KEY`。存储桶必须已存在。本地验证器显式使用 HTTP 访问 S3，上传产物后删除本地暂存目录，重启 API，再通过鉴权 API 核对同样的文件字节。上传对象保留在可丢弃测试桶中供检查，桶的保留策略由实验环境运维者负责。

服务配置与生命周期限制见 [Execution README](../../../execution/backend/README.md)；独立的 minikube 与生产验收工作见[生产设计](../../../.agents/notes/proposed/architecture/2026-09-14-rank-production-execution.md)。案例通过不代表已经验证 Rank 对外服务、模型质量或完整运行时沙箱隔离。

## 使用 OpenSandbox

通过[沙箱实验](../../deploy/k8s/README.md)构建受保护的运行时镜像并启动生命周期服务。以私有环境提供 `EXECUTION_TEST_SANDBOX_URL`（含 `/v1`）、`EXECUTION_TEST_SANDBOX_API_KEY` 与不可变的 `EXECUTION_TEST_SANDBOX_IMAGE`，然后运行：

```sh
node rank/examples/execution-lifecycle/verify-remote.mjs
```

[远程快照](remote.snapshot.json)覆盖实际打包的 Execution 二进制、远程确定性 Harness、冻结 CPU/内存计划、持久化运行时拥有关系、事件、空 metadata 的重复请求、Sandbox 删除后的产物读取，以及不分配环境的队列中取消。提供 `EXECUTION_TEST_DATABASE_URL` 后，相同验证器启动一个 API 和两个 River Worker，结束时删除自己随机命名的 `remote_*` schema。案例使用同一主机上多个进程共享的本地产物存储，不证明跨主机产物访问。实验另外运行真实 DSH 结构化历史测试，以及独立的生命周期服务失效实验。

容量快照检查鉴权后的聚合计数，使用停用的评测队列验证：当前 Worker 池无法处理的等待工作不会被计为可执行需求。

排队过期场景向停用队列提交请求，用更长的默认值重启 API，验证原始截止时间、attempt 为零的失败及两个持久生命周期事件。SQLite 和 PostgreSQL/River 使用同一快照。

两种模式还会提交一个确定性的结构化输出失败请求。相同请求在重提后仍保留失败状态和单次尝试，包括经过 PostgreSQL JSON 规范化之后。清理后仍可读取记录的输出、已知用量及失败结果产物。此场景验证 Worker 失败处理，不调用模型供应商。
