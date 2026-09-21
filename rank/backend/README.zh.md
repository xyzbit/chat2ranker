# Rank 后端

[English](README.md) | 中文

[调用反馈](../docs/response-feedback.md)支持请求搜索、排查详情，以及将审核内容保存为不可变数据集版本。用例保留来源引用，追加操作不改变已有 Run 快照。

本 Go Module 产出一个二进制程序：

- `rankd`：Rank API、版本冻结、用例调度、评分策略、结果聚合与业务持久化。

后端依赖 Rank Repository 接口和版本化 Execution Service Client，不导入 DSH 实现包、不启动 Harness 进程，也不调用 `ctx.agents.create()`。

`rankd` 冻结 DatasetVersion、AgentVersion 与 EvaluatorVersion，再把 Run 展开为 Case × Trial 记录。每个 Run 始终只绑定一个 Agent 版本；对比请求持久化一个 RunGroup，并在调度前以同一事务为每个选定 Agent 版本创建一个独立子 Run。每个 Trial 先提交候选调用并执行必需的确定性检查，仅在需要时提交独立的 Rubric Judge 调用；默认每个用例运行五次。Rank 分离质量失败、基础设施失败与评分失败，聚合有效 Trial 通过率、稳定用例、pass^3，以及候选/评分成本，并且只保存 Execution 与 Artifact 引用。

浏览器客户端追踪 `GET /api/runs/{id}/events`，不轮询 Worker 进程。共享 [SQL 存储](internal/sqlstore/store.go)支持本地开发使用 SQLite、部署使用 PostgreSQL，分别提供明确迁移，共用可移植业务查询。

Deployment 发布使用持久化的[发布任务](../docs/release-policy.md)，支持可选回归 Run 和事务内别名切换。SQLite 迁移 18–19 保存规则、决策、Agent 资产默认数据集和可恢复任务，与不可变 Agent 版本及执行限制独立。

新部署在显式发布前保持未发布状态，新建 Agent 版本不会改变其目标。外部调用方可直接使用部署名称；[服务引用](../docs/serving-api.md#deployment-reference)定义发布解析和显式预览。

端点无需策略开关即可接受追加指令和调用方函数。追加指令仅对本次请求附加到冻结的系统提示词，续聊不继承。调用方函数仍须通过 Schema 校验并由调用方执行。API Key 创建时仅返回一次明文，持久化只保存哈希；列表不能恢复密钥。

Response 取消先记录持久意图，再联系 Execution；[服务 API](../docs/serving-api.md#cancellation)定义待确认与终态响应。

[对话配置](../docs/conversation-configuration.md)记录不含密钥的类型化表单操作。测试集修改创建不可变版本；端点创建接受明确的运行限制，发布门槛修改与 API 共享事务和验证规则。

按 Agent 版本查找部署时，在指定租户内查询不可变发布历史，包含已被后续发布替换的版本。结果按各部署最新的匹配发布排序。

[Agent 试用](../docs/agent-playground.md)复用持久化 Response，支持直接试用版本和已部署会话。SQLite 迁移 20 保留现有记录，允许运营端 Response 不绑定调用凭据，未发布版本试用不绑定 Deployment。

`rankd` 需要 Go 1.26.0，并向配置的 Execution API 转发 `EXECUTION_SERVICE_TOKEN`，包括 SSE 请求。Rank 与 Execution 各自拥有独立数据库 schema（模式）。PostgreSQL 持久化不会让 Rank 编排器自动具备安全多副本能力。[服务派发](../../.agents/notes/implemented/architecture/2026-09-14-rank-serving-dispatch.md)恢复公开 Response 与 Agent 试用；[评测派发](../../.agents/notes/implemented/architecture/2026-09-14-rank-evaluation-dispatch.md)恢复候选/裁判调用。

使用模型连接的 Agent 创建和版本编辑会固定所选模型；省略时采用已验证连接的默认值。表单与 Rank 对话命令共享解析和协议校验。连接默认值修改后，已有版本保持原模型；未明确模型的旧版本需要保存新的明确版本后才能评测或服务。参见[固定模型选择](../../.agents/notes/implemented/architecture/2026-09-14-rank-frozen-model-selection.md)。

Serving 与试用续聊为支持的 Harness 冻结原生消息角色及调用与结果 ID；不支持的适配器在创建 Response 前拒绝。见[结构化历史决策](../../.agents/notes/implemented/architecture/2026-09-14-rank-structured-history.md)。

## 管理访问

将 `-access-file` 或 `RANK_ACCESS_FILE` 设置为管理员凭据文件。非回环监听没有该文件时拒绝启动；仅回环的本地开发可省略。配置该文件后，所有存储模式都必须显式设置至少 24 个字符的 `RANK_ACTION_SECRET`、`RANK_CONTROL_TOKEN` 和 `EXECUTION_SERVICE_TOKEN`，内置开发密钥会被拒绝。文件为 JSON，包含 `version: 1` 和 `operators` 数组。每项包含稳定的 `subject`（ASCII 字母、数字、`@._-`，最多 128 个字符）、`tokenSha256`（独立生成随机令牌的 SHA-256 小写十六进制摘要）和可选 RFC 3339 `expiresAt`。每个令牌至少生成 32 个随机字节；这里的 SHA-256 用于高熵令牌，不用于人工密码。明文令牌保存在仓库和模型对话之外。文件只保存摘要，轮换期间同一主体可以有多个有效令牌。

浏览器通过 Rank 网页登录，账号为 subject，密码为发放的随机令牌。非浏览器客户端仍可使用 `Authorization: Bearer <token>` 或 Basic；未认证请求返回 JSON，不触发浏览器原生认证弹窗。`GET /api/identity` 返回已认证主体和角色。原子替换文件后，每次新授权都会读取；文件缺失或无效时返回 503，不保留旧权限。空管理员列表吊销全部管理员。Kubernetes 投影文件异步更新。吊销影响后续请求，不影响已接受任务或已建立的流。

部署在 TLS 网关之后，并转发原始 Authorization 头。网关改变协议或主机名时，将 `RANK_MANAGEMENT_ORIGIN` 或 `-management-origin` 设置为公开来源，例如 `https://rank.example.com`，并在 control-host 设置相同来源。带有不同浏览器 Origin 的变更请求被拒绝。健康探测保持无认证；网关可以进一步限制访问。CORS 本身不授予管理权限。

管理员管理共享安装实例及其对外服务租户。对外 API Key 保留租户/端点/操作权限，不能进入管理接口。独立中控令牌只获得明确的中控服务路由权限；该服务写入连接时拒绝模型秘密。直接发布与发布任务创建使用管理员主体。见[访问决策](../../.agents/notes/implemented/architecture/2026-09-15-rank-management-access.md)和[打包验收](../examples/access-control/README.md)。企业登录与团队隔离的 Agent/Dataset 归属属于独立工作。

`POST /api/auth/login` 接受同源 JSON `{ "username": "<subject>", "password": "<token>" }`，签发作用于 `/` 的不透明 HttpOnly、SameSite=Strict Cookie。使用 TLS 或 HTTPS `-management-origin` 时 Cookie 带 Secure。`POST /api/auth/logout` 撤销服务端会话并清除 Cookie。登录、退出及通过 Cookie 认证的变更请求要求 Origin 精确匹配。身份与登录响应不缓存；公开 Responses 路由不接受该 Cookie。

浏览器会话经过 `-session-ttl` 后过期（默认 `8h`，最小 `1s`），每个安装实例最多保留 `-session-capacity` 个活跃会话（默认 `1024`，最小 `1`）。Rank 在数据库中保存 Cookie 与凭据摘要，并对每次请求复核实时管理员文件。固定过期时间、轮换和退出对不同 API 进程及重启均生效。登录清理过期记录；容量不足返回 503，不驱逐其他浏览器。数据库不保存原始 Cookie 或管理员凭据。进程内提供者供独立 HTTP fixture 使用。简短人工密码、注册和密码找回需要专用身份服务。

## PostgreSQL 配置

私下设置 `RANK_DATABASE_URL`，通过 `-postgres-schema`（默认 `rank`，或使用 `RANK_POSTGRES_SCHEMA`）及 `-database-connections`（默认 8）选择配置。使用迁移角色运行 `rankd -migrate`，再启动服务器。启动检查 schema 版本，拒绝缺失或不支持的版本。PostgreSQL schema v4 初始化空数据库或显式升级 v1/v2/v3，不导入 SQLite 数据。没有 `RANK_DATABASE_URL` 时，`-db` 选择 SQLite，打开时执行迁移。

`-database-timeout` 默认 `5s`，限制 PostgreSQL 连接池获取、查询 I/O 和应用事务；调用方更早的时限优先，同步接入与 SSE 保留各自寿命。原始依赖超时返回 HTTP 503 `dependency_timeout`；超时不证明写入已回滚，客户端必须保留请求身份。较长的显式迁移可使用 `-migrate -database-timeout=45s`。详见[数据库时限语义](../../.agents/notes/implemented/architecture/2026-09-15-postgres-deadlines.md)。

PostgreSQL 服务器模式要求显式设置 `RANK_ACTION_SECRET`、`RANK_CONTROL_TOKEN` 和 `EXECUTION_SERVICE_TOKEN`，每个至少 24 个字符，并通过 `-webhook-secret-dir` 指定持久目录。这些内部密钥不能替代 Rank 和 control-host 的管理员认证。模型凭据仍由 Execution 保存。健康响应报告所选存储驱动，不暴露数据库配置。

Rank 保留一个负责恢复及模型观察的调度进程。PostgreSQL 用 schema 专属的咨询事务锁串行执行短业务写事务；模型和 Execution 调用位于锁外。HTTP API 副本共享该数据库，持久化接受工作但不启动观察任务。见 [API 滚动更新](../../.agents/notes/implemented/bug-fix/2026-09-17-rank-api-rolling-drain.md)及[存储决策](../../.agents/notes/implemented/architecture/2026-09-14-rank-postgres-storage.md)。

## 进程模式与 HTTP 排空

`-mode=all` 在开发环境中同时运行 HTTP API 与调度器。PostgreSQL 部署使用 `-mode=api` 运行可独立替换的 HTTP 实例，使用且仅使用一个 `-mode=dispatcher` 处理服务/评测观察、发布任务与 Webhook 投递。调度器仅暴露 `/api/health`，按 `-dispatch-retry-interval` 发现持久化准入，不重置已在观察的工作。调度器替换仍遵守单拥有者规则。数据库迁移和 control-host 替换各自遵守维护要求。

`-shutdown-timeout` 默认 `30s`，必须为正。SIGTERM 关闭 HTTP 监听并等待已接收请求；超时关闭剩余连接，不取消已持久化的 Execution 任务。客户端必须将没有终态事件的流视为未完成，使用原 Response ID 查询或回放。`-event-poll-interval`（默认 `1s`，必须为正）限制共享响应事件的轮询间隔；进程内通知可更早唤醒读取。它不改变模型执行超时。Kubernetes Pod 宽限期必须覆盖 preStop、HTTP 排空及清理；实验配置为 5 秒 preStop、30 秒排空、45 秒 Pod 宽限期。

## 派发恢复

已接受的 Response 与评测调用保留原始出站 Execution 请求。观察错误等待 `-dispatch-retry-interval` 后重试，默认 1 秒；`-execution-observe-timeout` 限定每次事件观察时长，默认 15 秒，以便重新处理待取消意图。两个值都必须为正。退出等待本地服务与评测观察任务结束，不取消远端执行。缺少冻结请求的旧活动 Response 会阻止启动；升级前需要使用拥有它们的版本完成或显式处置。启动也会拒绝缺少持久派发格式的活动远端 Run。已有完成历史仍可读取。

评测准入在派发候选前冻结系统裁判连接/模型、租户路由和超时。候选请求与 Run/Trial 在同一准入事务保存。传输恢复复用已存请求，不自动重跑失败的候选或裁判。Run 取消在核对远端结果期间暴露 `cancelRequested: true`，`cancelled` 才是终态。尚未提交的候选意图不启动 Worker 即可取消。界面在取消待确认时展示“正在停止…”。

产物预览响应保留 Execution 的 `encoding`（`utf-8` 或 `base64`）及字节前缀 `truncated` 标记。消费者需解码 base64 获取二进制字节，不能把截断内容当作完整文件。

## 验证

`go test ./...` 验证 SQLite 行为。设置私有 `RANK_TEST_DATABASE_URL` 后，同一组应用/HTTP 测试使用隔离的 PostgreSQL schema，包括重新打开已保存历史；PostgreSQL 专项测试覆盖多个连接池竞争、回滚、迁移版本拒绝和事件顺序。SQLite 特有迁移仍在 SQLite 上测试。清理只删除测试拥有的 schema。

在仓库根目录运行 `node --test --test-name-pattern='Serving executes Responses' apps/rank-web/tests/stage5-e2e.test.mjs`，可执行打包 Rank/Execution/Worker 的服务与发布快照。设置 `RANK_TEST_DATABASE_URL` 后，Rank 使用 PostgreSQL，显式迁移随机 schema，并在重启后核对历史及 outbox（发件箱）记录。此场景中的 Execution 使用 SQLite；独立的 [Execution 生命周期案例](../examples/execution-lifecycle/README.md)验证 River Worker。服务恢复快照还会在提交前，以及 Execution 接受后但 HTTP 响应被丢弃时杀掉 Rank，验证原始身份和唯一终态事件。评测快照还会丢弃候选/裁判提交响应并杀掉 Rank，验证原始执行身份和唯一结果/费用投影。这些测试不证明完整 Kubernetes 平台。
