# 访问控制验收

[English](README.md) | 中文

在仓库根目录运行 `pnpm rank:test:access`，需要 Go 1.26.0 与 Node。案例构建 Rank、Execution 和 Worker，创建临时 SQLite 数据库与高熵管理员凭据，并在回环地址启动开启管理认证的服务。它使用确定性 Harness，不请求外部供应商。

案例为两个租户发布端点，调用公开 Responses API，并检查租户、端点与操作权限同样覆盖事件流、取消、审批及续聊。它验证对外 Key 不能管理 Rank，管理员 Key 不能作为对外 Key，发布记录使用已认证管理员，以及旧 Key 吊销后新对外 Key 仍能读取授权历史。

同一个 Rank 进程观察管理员令牌轮换，并拒绝损坏的凭据文件替换。独立启动检查拒绝未保护的非回环监听，以及缺少服务凭据或使用内置开发服务凭据的认证配置。结果必须匹配[快照](snapshot.json)。退出时删除进程及临时凭据，命令不打印令牌。

[管理配置](../../backend/README.md#management-access)定义部署凭据、TLS 来源与服务权限。[minikube 验证器](../../deploy/k8s/README.md#complete-platform-experiment)在 PostgreSQL/River 上重复对外权限检查，并通过网关测试投影文件吊销。本案例验证共享管理员工作区，不证明 Agent 或 Dataset 已支持团队隔离归属。

可运行快照还提交网页登录 JSON，验证 Cookie 可读写工作区而不能调用公开 API，并证明退出与凭据轮换使浏览器会话失效。HTTP 测试覆盖跨来源请求、无效登录输入、Cookie 属性与会话替换；会话存储测试覆盖过期与容量。
