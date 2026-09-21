# 管理访问验证 — 2026-09-15

[English](2026-09-15-management-access.md) | 中文

[验收记录](2026-09-15-management-access.json)包含精确镜像、源码摘要和十项通过的 minikube 完整平台检查。Rank 与 control-host 要求管理员授权；网关转发该身份并隐藏内部命令。临时管理员在 Secret 投影后可用，删除其条目后被拒绝，原管理员仍可访问。

共享的 [HTTP 场景](../../examples/access-control/scenario.mjs)验证读取、事件流、取消、审批与续聊的租户及端点隔离。缺少操作权限返回 403；吊销的对外 Key 返回 401；替换 Key 保留授权历史。对外 Key 不能进入管理端，管理员令牌不能调用公开 Responses。即使提交内容伪造操作者，直接发布仍记录已认证主体。直接私网检查中，中控服务读取初始化数据返回 200，访问 API Key 管理返回 403，无认证访问 control-host 会话返回 401。

[打包快照](../../examples/access-control/README.md)还验证管理员重叠轮换、无效文件拒绝和非回环启动保护。包括 SQLite 在内的认证配置在监听前拒绝缺失或已知的开发服务凭据。针对性 Go 测试、三项 control-host 测试与 `pnpm run lint` 通过。完整集群保留热点、聊天、试用、模型预算、S3 证据、SSE 重放，以及 Pod 替换和 Worker 缩到零后的恢复。文档检查通过 28 项中的 27 项，根目录 `AGENTS.md` 的既有字数超限仍存在。

这提供共享工作区管理员访问，不提供团队隔离的 Agent/Dataset 归属或企业 SSO。吊销在文件传播后影响后续请求；已接受任务和已建立流保留其文档定义的寿命。内部服务令牌轮换、完整审计、生产 TLS/工作负载身份、真实供应商配额与物理双主机容量仍是[生产方案](../../../.agents/notes/proposed/architecture/2026-09-14-rank-production-execution.md)中的独立工作。
