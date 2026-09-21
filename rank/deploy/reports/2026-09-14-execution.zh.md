# Execution 验收证据 — 2026-09-14

[English](2026-09-14-execution.md) | 中文

本报告覆盖基于 `c078fbd287e6cfe3de6a220ffa293fe6347a4e01` 的工作区，包括尚未提交的实现变更，不是生产就绪证明。源码树中还包含此前的产品工作，本轮验收不包含提交或推送。

## 环境

macOS arm64、Go 1.26.0、Node 24.14.1、私有测试容器中的 PostgreSQL 15.15、River 开源版 v0.47.0、MinIO Go S3 客户端 v7.3.0，以及原生 SeaweedFS v4.47。密钥保存在私有本地文件中，不写入报告。Docker 构建目标为 Linux arm64。

| 镜像 | 本地内容标签 | Docker 镜像标识 |
| --- | --- | --- |
| execution | `rank-execution-lab:7e423a6b8bd50681` | `sha256:bcac3a0ba33c0b751930bebee7bf876aaabc8de08497dc7b309a9d2f2c7e9037` |
| objects | `rank-object-lab:92ddf30de78b2113` | `sha256:0371de08621980bb63c930570038b0aac417d20129e9b74aef85308402315fcc` |
| postgres | `rank-postgres-lab:b3968e348b48f119` | `sha256:b3968e348b48f1198cc6de6611d055dbad91cd561b7990c406c3fc28d7095b21` |

这些镜像通过了容器入口冒烟检查和以下 Kubernetes Execution 实验。

## 已验证结果

| 检查 | 观察结果 |
| --- | --- |
| Execution 与 Rank Go 测试套件 | 通过；需要显式 PostgreSQL/S3 环境变量的测试在普通套件中跳过。 |
| 真实 PostgreSQL Repository 测试 | 通过：事务回滚、并发幂等准入、有序且校验尝试代次的进度、取消、加密凭据、共享额度和终态投递核对。 |
| 打包 API 与两个独立 Worker 进程 | 通过：两个 Worker 均执行请求，终态结果与 River 确认一致，队列中/执行中取消以及拥有者丢失的未知结果均可收敛。 |
| SQLite 生命周期与竞态检查 | 通过：有界在线/评测池、队列准入、退出清理及等待中任务保留。无法确认的强制终止会保留工作目录。 |
| 可运行生命周期快照 | SQLite 通过；PostgreSQL/River 与两个 Worker 通过；PostgreSQL/River 加 S3 在删除本地产物并重启 API 后通过。 |
| 对 SeaweedFS 的 S3 适配器检查 | 通过：写入/读取/删除、签名下载及本地路径穿越保护。 |
| 带服务鉴权的 control-host 完整测试 | 通过：持久控制会话和 Rank 工具在启用私有 Execution 令牌后继续工作。 |
| 文档与格式 | Lint 和 diff 空白检查通过。文档检查 27 项通过，1 项为原有根目录 `AGENTS.md` 字数超限（2398 个词，上限 1950）。更新后的双语配对与相对链接通过。 |
| Kubernetes Execution 实验 | 通过：迁移与部署、队列保留/取消、幂等提交、SSE 重放、运行中取消、服务 Pod 替换后的证据读取、Worker 丢失后的未知结果，以及滚动更新中的 8 个已接受请求。 |

## Kubernetes 测量

[实验记录](2026-09-14-kubernetes.json)来自单节点 minikube 1.39.0、Kubernetes 1.37.0、containerd 2.3.4 和 Linux arm64。Docker 对节点容器限制为 4 CPU、4 GiB；Kubernetes 上报的可分配资源反映 Docker 虚拟机的 8 CPU、约 7.65 GiB，不能据此宣称实验获得了这些容量。每个 Worker 限制 1 CPU、512 MiB，配置一个在线槽位和一个评测槽位；本轮负载只使用在线队列。

| Worker 副本 | 请求数 | 批次耗时 | 实际峰值并发 | 排队 p95 |
| --- | --- | --- | --- | --- |
| 1 | 24 | 10.812 秒 | 1 | 9.855 秒 |
| 2 | 24 | 3.875 秒 | 2 | 3.414 秒 |

这是一次确定性短 CLI 负载的观察，不是稳定基准或线性扩容承诺；未包含真实模型、真实跨主机负载或自动扩缩容。两个副本均记录了执行拥有者。强制移除运行中的 Worker 后，执行保持一次尝试并报告 `execution_outcome_unknown`，没有重放。API、Worker、PostgreSQL 和对象服务 Pod 均被替换，原有事件和 stdout 产物仍可读取。

Docker Hub 下载超时通过 Google 官方镜像缓存导入原定 kindnet 版本解决。PostgreSQL 的摘要归档缺少可运行标签，通过给锁定源镜像添加专用实验标签解决。系统 Python 3.9 无法解析 Go 的纳秒时间戳，验收改用文档要求的 Python 3.12，并增加启动前版本检查。

专用集群整体停止并重新启动后，只读验证找回了同一个已完成执行、有序 SSE 历史及通过 SHA-256 核对的 stdout；未知结果仍保持失败且尝试次数为一。依赖初始化期间 Execution 容器发生重启，随后恢复。这验证的是本地重启后的数据保留，不是服务无中断可用性。

## 剩余验收

本报告只覆盖较早的 Execution fixture 镜像。独立的[完整平台报告](2026-09-14-platform.zh.md)记录真实 DSH 每日热点/聊天客户端、Rank PostgreSQL 派发、结构化历史及中控会话恢复。[实验教程](../k8s/README.zh.md)提供两组实验。

完整运行时隔离、有效扩缩容指标、有界自动扩缩容以及物理跨主机证据仍属于[生产提案](../../../.agents/notes/proposed/architecture/2026-09-14-rank-production-execution.md)。当前测试运行确定性 CLI（命令行界面）调用，不测量真实模型质量、供应商预算或生产流量容量。
