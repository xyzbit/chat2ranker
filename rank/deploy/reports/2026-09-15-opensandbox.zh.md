# OpenSandbox 执行实验 — 2026-09-15

[English](2026-09-15-opensandbox.md) | 中文

[机器可读报告](2026-09-15-opensandbox.json)记录测试镜像与结果。OpenSandbox 服务端 0.2.3 和 execd 1.1.0 在 Docker Desktop 的 Linux 环境运行。主机上的 Execution 进程使用 Go 1.26.0 与固定版本的 Go SDK。完整 minikube 平台也运行远程适配器：API/Worker Pod 通过 Kubernetes Service 访问 Docker 生命周期服务，独立 Harness 容器使用专用 Docker 网络。PostgreSQL 与 S3 保留在 minikube 内。

| 检查 | 实际结果 |
| --- | --- |
| 打包 API、SQLite | 远程调用、持久化运行时句柄、持久事件、幂等重放、删除后的产物读取及队列中取消符合案例快照。 |
| 打包 API、PostgreSQL/River | 独立 API 与两个 Worker 进程通过相同快照。 |
| 完整 minikube 平台 | 网关端点调用、每日热点、多轮聊天、Agent 试用、S3 产物、中控历史与 Rank 重启通过。Worker Pod 缩到零再恢复后，同一排队响应以第一次尝试完成。 |
| 真实 DSH 进程 | 结构化历史进入确定性模型，回复记住姓名 Ada。运行时删除后仍可读取进度与产物。 |
| 持久化与故障注入 | SQLite/真实 PostgreSQL 拒绝过期拥有关系；创建响应丢失不重放，延迟发现可恢复清理，句柄保存失败阻止命令，异源代理没有收到带凭据的请求。 |
| 独立运行时寿命 | 已观察到脱离进程组且忽略 TERM 的任务。强制停止生命周期服务后，受保护的 PID 1 在 25 秒结束容器，早于服务端 90 秒 TTL。 |
| 容器身份 | UID 1000 无法读取 root PID 1 环境或向其发信号；命令看不到 execd 令牌或 Docker socket。 |

独立集成测试使用本地产物存储；完整平台验证了 Sandbox 删除与 Pod 替换后的 S3 读取。上述结果不证明物理主机吞吐、生产鉴权、磁盘与网络出口限制，或任意恶意代码隔离。Docker Sandbox 服务与 minikube 同处一台笔记本，它不是 Kubernetes 原生 Sandbox 控制器。环境搭建与可重复执行的命令见[实验教程](../k8s/README.md)，拥有关系语义见[决策记录](../../../.agents/notes/implemented/architecture/2026-09-15-execution-opensandbox.md)。
