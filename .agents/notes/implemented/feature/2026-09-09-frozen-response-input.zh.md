# Agent Note：冻结 Response 的图片与文件输入

状态：已实现

[English](2026-09-09-frozen-response-input.md) | 中文

## 问题

公开 Responses 端点此前只接受文本。如果新增图片与文件字段却不冻结内容，重试、Conversation 续接与历史回放就会依赖可变的远程 URL。如果由 Execution Worker 下载调用方 URL，还会绕过对外服务准入限制，并在权限更高的生命周期阶段授予网络访问能力。

## 决策

`POST /v1/responses` 接受使用 Base64 Data URL 或公开 HTTPS `image_url` 的 `input_image`，也接受使用一个公开 HTTPS `file_url` 或 Base64 `file_data` 的 `input_file`。内联文件数据必须提供 `filename`；图片 `detail` 默认且仅支持 `auto`；在 Files 资源负责该标识符之前，系统会拒绝 `file_id`。

Rank 会在受理 Response 前冻结每项二进制输入。它通过不使用代理的 HTTPS 客户端解码内联数据或下载远程内容；该客户端校验每次重定向，并拒绝解析到私有、Loopback、Link-local 或其他非公开地址的主机。Rank 计算内容的 SHA-256 标识符，把字节与规范化媒体元数据随 Response 持久化；对于远程来源 URL，仅保留其摘要用于幂等判断。

Execution 协议版本 2 承载内容寻址的 `inputAttachments`。Service 会在持久化前校验条目数、解码后大小、媒体类型、文件名、支持的 detail 与内容摘要，Worker 内部还会再次校验。Worker 把每个附件写入私有工作区的 `.rank-inputs`，保留一份产物副本，从 `request.json` 移除附件字节与凭据，并告诉 Agent 应检查哪些不可变路径。

每个请求最多接受八个二进制条目，解码后总大小不超过 20 MiB。续接会重建此前的用户与助手消息，并按 Conversation 顺序提供所有冻结附件。因此，同一组限制适用于一次续接 Execution 所需的累计附件。

## 验证

Contract 测试覆盖校验和确定性 Worker 路径。Execution Service 与 Worker 测试覆盖无效摘要、私有路径写入、产物保留和请求记录脱敏。Rank HTTP 测试覆盖混合文本、图片与文件输入，SQLite 持久化，远程来源冻结，幂等回放不重复下载，URL 脱敏，不支持形式的拒绝，私有地址拒绝，以及保留附件的续接。组装后的对外服务快照通过 Rank、Execution Service 与真实 Worker 进程发送内联图片和文件输入。

## 备选方案

**把远程 URL 传给 Worker。**未采用，因为引用的字节可能在准入后发生变化，而且 Worker 网络策略会因此承担公开输入下载职责。

**只持久化对象存储引用。**暂缓采用，因为本地组合没有持久对象存储适配器。本实现由 Response Repository 保存冻结字节；Execution 约定保持内容寻址，因此后续改用对象存储时无需改变公开协议。

**把 `file_id` 作为任意字符串支持。**未采用，因为当前没有 Files 资源校验该标识符的所有权、冻结内容或定义留存策略。

**只保留最新一轮附件。**未采用，因为续接后的 Agent 必须收到其持久 Conversation 历史所描述的同一组模型可见输入。

## 结果

已受理 Response 及其每次续接都会使用不可变的图片和文件字节，即使调用方使用的是即将过期或内容可变的 URL。本地 SQLite 会随留存输入增长；远程来源使请求准入执行有界网络 I/O；较长 Conversation 可能达到累计附件限制。生产部署仍需要对象存储、留存策略、恶意软件与内容扫描，以及签名产物访问。
