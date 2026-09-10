# Agent Note：签名 Webhook Outbox

状态：已实现

[English](2026-09-09-signed-webhook-outbox.md) | 中文

## 问题

后台或已经断开连接的 Responses 调用方需要可靠接收状态变化，而不必持续保持 SSE 连接或轮询。如果允许每个公开请求临时传入回调地址，就会绕过 Deployment 治理并形成直接的 SSRF 攻击面。如果在提交 Response 后再发送回调，进程也可能在两次写入之间停止并永久丢失通知。

## 决策

Webhook 端点是租户持有的控制平面资源，作用域包含一个或多个 Deployment 和显式事件白名单。支持的事件包括 `response.completed`、`response.failed`、`response.cancelled` 与 `response.requires_action`。每个端点必须使用公网 HTTPS 地址。端点的 Standard Webhooks `whsec_` HMAC 密钥只返回一次，并通过凭据存储端口保留；SQLite 只保存不透明的密钥引用。

订阅的 Response 状态变化会为每个匹配端点追加一个冻结的薄事件，并与 Response 投影和持久 Response 事件在同一个 SQLite 事务中提交。载荷只包含事件标识符、事件时间和类型以及 Response 标识符。接收方使用受限的对外服务凭据读取 Response，不会隐式接收输出或输入数据。

Dispatcher 使用带过期时间的租约领取到期记录，并在多次尝试间保持事件标识符不变。它使用 HMAC-SHA256 对 `webhook-id.webhook-timestamp.raw_body` 的精确字节签名，并发送 Standard Webhooks Header。只有 HTTP `2xx` 会完成投递。Rank 不跟随重定向；网络错误和其他状态会持久化错误及下次尝试时间。重试延迟跨越多日、支持部署配置，并遵守更长的 `Retry-After`。HTTP `410 Gone` 会停用端点。

出站请求禁用环境代理、强制 HTTPS、在拨号前解析全部目的地址，并拒绝私有、Loopback、Link-local 和其他非公网 IP。重启后的 Dispatcher 只会重新领取过期租约，因此进程停止不会搁置投递，两个 Dispatcher 通常也不会发送同一条活跃领取记录。接收方仍必须按事件标识符去重，因为接收方提交后、返回确认前发生网络故障时可能出现至少一次投递。

## 验证

Rank 测试覆盖一次性密钥投影、文件权限与仅创建写入、端点和事件校验、事务 Outbox 创建、精确签名校验、失败尝试调度、成功重试、通过新 Service 实例恢复过期租约，以及拒绝私有目的地址。HTTP 测试会创建 Deployment 作用域端点、完成一个 Response 并观察其持久 Outbox 记录。Web 构建覆盖中英文端点管理界面。

## 备选方案

**允许每个 Response 传入 `webhook_url`。**未采用，因为目的地址属于 Deployment 策略和凭据状态，而不是不可信的单次调用输入。

**把 HMAC 密钥存入 Rank 数据库。**未采用，因为普通 Repository 备份和投影不能包含可复用的签名凭据。

**在 Response 事务之后直接发送而不使用 Outbox。**未采用，因为进程故障可能永久丢失通知。

**跟随 HTTP 重定向。**未采用，因为重定向会增加 SSRF 和凭据泄漏风险，而且 Standard Webhooks 把 `3xx` 视为投递失败。

## 结果

外部系统可以接收持久、已签名且受 Deployment 约束的 Response 通知，并在 Rank 重启后继续恢复。本地适配器使用 SQLite 租约与私有文件系统密钥存储。手工重放、签名密钥轮换、端点故障告警、PostgreSQL 队列所有权和专用出站代理仍属于生产化工作。
