# Agent Note: 使用 River 持久投递 Execution

Status: implemented

[English](2026-09-14-execution-river-delivery.md) | 中文

## 问题

Execution API 副本与独立 Worker 需要持久任务投递，同时不能重复执行结果不明的模型或工具调用。SQLite 的进程内通知不能跨主机分发任务。队列任务被取消不代表运行时已经停止；只留在 Worker 上的文件也会随 Worker 替换而消失。

## 决策

幂等重提比较 JSON 值，而不是序列化字节。PostgreSQL jsonb 可能重排原始 schema 或工具参数对象，并改写十进制数表示。对象空白/键顺序和等值十进制表示不改变请求身份；数组顺序、字符串/数字类型及不同整数值仍有区别。十进制比较调整系数与指数文本，不按指数展开数值，也不通过浮点数舍入。首次查询与并发插入冲突使用同一比较。

Execution 使用 River 开源版 v0.47.0 与 PostgreSQL 进行分布式投递，工具链为 Go 1.26.0。应用拥有不可变执行输入、有序进度、尝试与终态。River 参数只包含执行 ID。记录创建、初始事件与入队共用事务，结果发布与 River 确认也共用事务。这实现了[生产提案](../../proposed/architecture/2026-09-14-rank-production-execution.md)的一部分，其余平台与隔离工作仍由该提案约束。

运行时与清理限制独立于请求输入记录，避免默认值变化破坏幂等重试，或缩短已有拥有者的恢复窗口。当前尝试只有在执行中才能发布进度。执行中取消保留额度直至执行返回；无法确认运行时停止的强制退出显式失败，并保留工作目录。投递重试绝不重新启动已记录为运行中的调用。拥有者丢失后，在记录的寿命结束后返回未知结果；小型核对器收敛已进入 River 终态却缺少应用结果的记录。

River 控制每进程的在线与评测并发。PostgreSQL 串行化应用的小范围运行准入事务，把执行中、取消中及终态但[运行时仍待清理](2026-09-14-execution-runtime-cleanup.md)的记录作为模型和租户的额度预留，不实现另一套队列或租约机制。SQLite 保留生命周期语义一致的有界单进程池。优雅退出先停止领取，再排空执行；本地等待中记录留给下一位拥有者。

SIGTERM 停止 River 领取，并等待配置的退出宽限期。排空超时后，StopAndCancel 使用独立的清理加持久化预算中断活跃投递。Worker 正常退出和调用成功是两件事：远程工具调用被中断后，即使确认 Sandbox 已不存在，业务结果仍可能未知。Kubernetes Pod 宽限期必须覆盖这一过程，并保留运行余量。

应用 schema 变更先提交，随后执行 River 按版本提交的迁移，两者持有同一个专用 PostgreSQL 会话锁。PostgreSQL 枚举新增值必须提交后，后续迁移才能使用。只有两组迁移均成功才写入就绪标记。迁移失败需要再次显式执行迁移，不能启动仅部分迁移的服务。

Execution 使用部署拥有的 AES-GCM 主密钥加密 PostgreSQL 模型凭据。服务令牌鉴权私有 API，凭据解析还需要控制令牌。S3 兼容产物存储先上传以摘要寻址的证据，再发布引用；API 读取核对这些引用。上传后数据库回滚可能留下无引用对象，自动回收不在本次实现内。

## 考虑过的替代方案

**自建数据库任务队列。** 需要保留 River 已经负责的投递恢复、维护和退出代码。无论选哪一种，应用的尝试与副作用规则都不可省略。

**每次调用都使用 Temporal 或 Kafka。** 两者都不能解决运行时隔离或外部副作用不确定性。已接受范围需要有界投递与 PostgreSQL 事务，持久工作流编排和独立消费事件总线暂不足以证明额外服务的必要性。

**把队列取消或重试当成运行时权威。** 队列状态不能证明脱离的进程或远程工具调用已经停止。确认取消与保守的未知结果保留了这个区别。

**产物只保存在 Worker 磁盘。** 会把 API 读取与保留证据绑定到某个 Worker。S3 适配器分离文件和 Worker 的生命周期，同时保留本地文件系统选项。

## 影响

[可运行无密钥案例](../../../../rank/examples/execution-lifecycle/README.md)通过打包的 API/Worker 进程检查 SQLite 与 PostgreSQL/River 的一致输出，包括 SSE（Server-Sent Events）回放和取消。真实 PostgreSQL 测试覆盖回滚、幂等、并发额度预留与状态核对。真实 S3 检查覆盖存储字节、删除和签名访问；S3 完整案例在读取保留证据前删除本地产物并重启 API。

该 [Kubernetes 实验](../../../../rank/deploy/k8s/README.md)在可独立替换的 API/Worker Pod 中运行同一个可信 fixture，并使用持久 PostgreSQL 与 S3 依赖。[验收报告](../../../../rank/deploy/reports/2026-09-14-execution.md)记录了证据保留、取消、拥有者丢失结果、滚动更新以及一个/两个 Worker 的批次耗时。这些测量不证明真实模型容量。

[活跃排空验证器](../../../../rank/deploy/k8s/README.md#active-worker-drain-experiment)通过真实 DSH 工具验证活跃任务下的二缩一及滚动替换。它把持久拥有权与 watch 记录的 Pod UID、容器退出码关联，确保后续在线请求不会落到正在排空的拥有者，并核对产物字节、一次尝试/操作及确认运行时删除。[报告](../../../../rank/deploy/reports/2026-09-15-active-drain.md)区分正常排空和宽限期到期后明确的未知结果中断。

[Rank PostgreSQL 存储](2026-09-14-rank-postgres-storage.md)是独立的实现。这些 Execution 检查不证明 Rank 派发可持久恢复、结构化多轮历史、完整运行时隔离、异构 Worker 能力路由、自动扩缩容或跨主机高可用。本地执行器仍是可信开发运行时。[生产提案](../../proposed/architecture/2026-09-14-rank-production-execution.md)在剩余验收要求满足前保持 proposed。
