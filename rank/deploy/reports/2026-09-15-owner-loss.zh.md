# 远程 Worker 拥有者丢失实验 — 2026-09-15

[English](2026-09-15-owner-loss.md) | 中文

[实测结果](2026-09-15-owner-loss.json)记录同一笔记本上单节点 minikube 与独立 Docker Sandbox 服务的镜像及结果。确定性模型让真实 DSH Bash 工具先尝试被禁止的写入，再调用一个记录操作后延迟返回的本地 HTTP 服务。验证器核对拥有者容器身份后，向其进程发送 SIGKILL，并要求退出码为 137。

| 场景 | 实际结果 |
| --- | --- |
| 工具操作发出后强制停止 Worker | 一次外部操作、一次写入拒绝、一次执行尝试；终态为 `execution_outcome_unknown`。确认运行时删除后清除清理预留。 |
| 同样故障后再请求取消 | 保留相同的未知结果；取消不会宣称外部操作已撤销。 |
| 结果提交后强制停止 Worker | 相同请求重提后仍保留成功结果，只有一次尝试和一个运行事件。 |

两个被中断的 HTTP 操作均已断开，均未重复。[组装应用快照](../k8s/fixtures/owner-loss.snapshot.json)记录 API 结果与外部观察到的工具事实，不包含 ID、耗时或凭据。同一组镜像通过完整平台回归，覆盖端点调用、每日热点、结构化聊天、试用、中控历史、S3 读取、SSE 重放及重启/队列恢复。

ARM64 镜像包含固定版本的 Landlock 程序和 Bubblewrap 0.8.0-2+deb12u1。本机 Docker Desktop 内核 `6.10.14-linuxkit` 不执行 Landlock 限制；DSH 既有 Bubblewrap 实现执行未改变的只读策略。宿主机原本使用无约束 Docker seccomp 配置，本实验未修改该配置。本机探测通过不代表其他内核、seccomp 或用户命名空间配置受支持。这些检查不证明任意恶意代码隔离、生产身份、磁盘/出口限制或物理主机容量。复现命令与前提见[实验教程](../k8s/README.md#worker-owner-loss-experiment)。
