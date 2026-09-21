# Execution 后端

[English](README.md) | 中文

该 Go 模块是可复用的执行控制平面，生成：

- `executiond`：版本化 HTTP API、幂等执行记录、生命周期控制、Repository 访问、Executor 选择与 Artifact 授权。
- `execution-worker`：在私有 workspace 和 Harness Home 中执行一次不可变 Harness Adapter 调用。

公开的 `contract` 和 `client` 包定义 Rank 集成接口，`harness` 和 `artifacts` 定义适配器 API。领域与应用代码依赖 Repository 和 Executor 接口。SQLite 使用有界本地 Worker 池；PostgreSQL 使用 River 开源版 v0.47.0，支持独立的 API 与 Worker 角色。两者使用选定的 Executor。OpenSandbox 适配器远程运行打包的 DSH 或确定性 Harness，并持久化拥有关系、执行有时限的清理。

Harness 集成实现公共 `harness.Adapter` 接口，并通过稳定 Runner 类型注册。内置 Registry 包含确定性 Demo Adapter、第一方 DSH Adapter、可解析原生事件的 Claude Code、Codex 与 Hermes Adapter，以及 Pi 的无 Shell 部署 Adapter。Codex 消费临时的 `exec --json` 运行，Claude Code 消费不持久化 Session 的 `stream-json` 运行，Hermes 消费 usage report。原生 Codex 与 Claude Code 运行会复用启动 executiond 用户的 CLI 认证，同时保持每个任务的工作区与 Harness Home 隔离。默认 Codex 运行忽略用户配置，Claude Code 使用安全模式，两者都把冻结的 Agent 模型作为单次调用参数；绑定模型连接的 Codex 则生成私有的逐任务 Provider 配置。

`/v1/model-catalog` 为 DeepSeek、MiniMax、智谱 GLM、OpenAI、Claude 和 Kimi 提供官方连接默认值：端点、协议、可选模型 ID、美元/百万 Token 默认价格、计价说明、来源链接与更新时间。MiniMax、智谱和 Kimi 默认使用中国区官方 OpenAI 兼容端点；国际站仍可作为自定义连接使用。人民币官网价格按目录固定汇率 `¥7.00 = $1` 归一化为美元估算，并在每个模型的计价说明中披露，保证跨 Provider 图表使用同一币种。Chat Completions、Responses 与 Anthropic Messages 连接分别通过原生模型目录和最小推理请求验证。模型目录同步成功时替换连接发现列表；同步失败时保留上次成功结果；Provider 不支持 `/models` 时仍可通过最小请求验证显式填写的模型。连接可以覆盖任一继承价格，但不会修改本地目录。成本解析仍按 Provider 实际返回、连接覆盖、目录默认的顺序执行；缺少用量或价格时保持未知。

模型连接通过 `/v1/model-connections` 管理，`/v1/model-catalog` 提供内置 Provider 端点、模型与默认价格元数据。连接可以覆盖自身模型的价格。元数据保存在 execution Repository，API Key 单独保存在本地凭据目录或加密的 PostgreSQL 凭据表，且不会序列化进执行规格、事件或产物。验证会在可用时同步 `/models`，并按所选协议执行一次最小请求。DSH 支持三种协议，Hermes 支持 Chat Completions，Codex 支持 Responses，Claude Code 使用自己的 CLI 登录。

执行成本优先使用 Provider 返回的实际费用，其次使用匹配的连接价格，最后使用内置目录价格。计算所得成本会冻结价格快照并标记为估算。缺少用量或匹配价格时，成本保持未知，不会套用其他 Provider 的价格或生成虚假估算。

`GET /v1/executions/{id}/events` 以 SSE 暴露持久化的单次执行事件日志。每条事件包含单调递增序号、尝试次数、类型、状态、可选数据与时间戳。客户端重连时通过 `Last-Event-ID` 或 `?after=` 重放已持久化的生命周期与 Harness 进度，再继续追踪实时事件。

命令 stdout 进度保留输出的空白和文本片段。Codex 和 Claude Code 适配器将原始协议输出保留在产物中，仅以 `harness.output` 发出解析后的助手回答；消费者不得把协议 JSON 展示为对话文本。进度时机由适配器决定，可能返回完整回答而非逐 Token 增量。

`Spec.modelConnectionId` 要求提供非空且无首尾空白的 `Spec.model`。Rank 在保存 Agent 版本时解析连接默认值；直接调用 Execution 的客户端需要在提交前解析默认值。即使连接默认值发生变化，Worker 也使用已记录名称。缺少模型或包含首尾空白的选择在准入前被拒绝。

DSH 适配器启动 Node 时传入 `--expose-internals`，满足打包 headless profile 中 CLI 配置监视服务的要求。提示词与结构化历史两种调用路径使用同一个启动参数。

结构化 Agent 输入在 `prompt` 与 `messages` 中只接受一种。`/v1/runtimes/{harness}` 的 `structuredHistory` 能力标识可接收有序用户、助手和工具消息的适配器。不支持的历史在接纳前拒绝；DSH 历史使用[私有历史运行器](../../apps/execution-runtime/README.md)。命令适配器通过 `{input}` 声明支持。Codex、Claude Code 和 Hermes 仅支持提示词。

## 持久执行模式

需要 Go 1.26.0。未设置 `EXECUTION_DATABASE_URL` 时，`executiond -role all` 使用 SQLite；启用 PostgreSQL 后，`-role api` 接收请求并提供持久事件，`-role worker` 消费 River 任务，`-role all` 合并两种角色。接收流量前显式运行 `-migrate`。应用 DDL 和全部 River 迁移成功后才记录 schema 就绪状态。PostgreSQL 枚举迁移在同一迁移锁下按版本提交，不支持将所有 River 迁移包在一个事务中。

`-database-timeout` 默认 `5s`，限制 PostgreSQL 连接池获取、查询 I/O 和应用事务；调用方更早的时限优先，同步接入与 SSE 保留各自寿命。原始依赖超时返回 HTTP 503 `dependency_timeout`；超时不证明写入已回滚，客户端必须保留请求身份。较长的显式迁移可使用 `-migrate -database-timeout=45s`。详见[数据库时限语义](../../.agents/notes/implemented/architecture/2026-09-15-postgres-deadlines.md)。

PostgreSQL 启动需要至少 24 个字符的 `EXECUTION_SERVICE_TOKEN`，以及 `EXECUTION_CREDENTIAL_KEY`，即经过 base64 编码的 32 字节 AES-GCM 密钥。主密钥属于部署状态，需要随数据库备份保管，并提供给 API 与 Worker。配置服务令牌后，除 `/v1/health` 外的所有 HTTP 路由均要求服务 Bearer 令牌；`/v1/capacity` 也接受可选的只读 `EXECUTION_CAPACITY_TOKEN`。内部系统模型凭据解析还要求 `RANK_CONTROL_TOKEN`；Rank 和 control-host 使用 `EXECUTION_SERVICE_TOKEN` 调用这些接口。

幂等键标识一份不可变输入。JSON 对象格式与等值十进制表示不影响身份；数组顺序与不同值仍有区别。Execution 记录、首条事件和 River 任务在同一事务中提交，任务参数只包含执行 ID。运行时限制在提交时解析，独立于幂等比较使用的输入保存。Worker 启动时记录自身标识与实际发行版本。结果发布与 River 确认共用事务。只有当前执行中尝试可以发布持久进度，取消中或终态尝试不能发布迟到进度。

排队等待独立于执行时长：`-queue-timeout` 默认 `5m`，至少为 `1ms`；`Spec.queueTimeoutMs` 可以缩短上限。准入时以毫秒精度持久化 `queueExpiresAt`，重提与重启不延长它。API 协调器为过期队列记录 attempt 为零的 `execution_queue_expired`，Worker 领取检查则阻止过期启动。运行中的任务继续使用独立执行超时。过期队列在容量中计入 `expired`，不请求更多 Worker。缺少截止时间的记录以 `execution_limits_missing` 失败，升级不会给排队记录新的等待窗口。详见[排队截止语义](../../.agents/notes/implemented/architecture/2026-09-15-execution-queue-deadline.md)。

队列中取消会阻止运行时启动。执行中取消先进入 `cancelling`，运行时停止返回后才能进入 `cancelled`。无法确认的强制终止记录为失败，并保留工作目录。已启动执行失去拥有者后，在其记录的寿命结束后报告 `execution_outcome_unknown`；投递重试不授权重新调用模型或工具。终态投递核对器收敛 River 已结束、应用却未发布结果的记录。本地重启同样拒绝重放已启动调用。 已启动的尝试通过 `runtimeCleanupPending` 表示待清理；未知失败保留模型及租户容量，直到受尝试代次约束的停止确认。PostgreSQL schema 7 和 SQLite schema 10 在升级时保留此状态。本地适配器没有丢失拥有者后的自动清理恢复，见[清理责任](../../.agents/notes/implemented/architecture/2026-09-14-execution-runtime-cleanup.md)。

`-online-slots` 和 `-evaluation-slots` 限制每个 Worker 进程；带有 `metadata.responseId` 的请求使用在线队列。`-max-queued` 限制准入数量，队列满时返回 HTTP 429。PostgreSQL 的 `-max-model-concurrent` 和 `-max-tenant-concurrent` 还约束跨副本的执行中与取消中记录。受额度限制的任务在启动运行时前延后投递。这些是并发限制，不是供应商每分钟 token 限制。部署需要一致地解析模型名称与可信租户元数据。

`GET /v1/capacity` 返回同构 Worker 配置下的在线/评测聚合需求，除服务令牌外也接受可选的只读 `EXECUTION_CAPACITY_TOKEN`。`active` 统计持久化 running/cancelling 记录，`ready` 在满足运行条件的排队工作之间虚拟分配剩余全局额度；能力/投递/额度/资源阻塞和终态 `cleanupPending` 分别计数。启用的队列返回 `demand = active + ready`、`slotsPerWorker` 和 `oldestReadyWaitSeconds`。这是下一次轮询的需求估计，不是实时槽位测量或供应商限流检测。API/Worker 后端、能力、槽位和限额必须一致。见[容量语义](../../.agents/notes/implemented/architecture/2026-09-15-execution-capacity.md)和[有界扩缩容实验](../../rank/deploy/k8s/README.md)。

`-worker-timeout`、`-cleanup-grace`、`-persistence-timeout`、`-poll-interval`、`-rescue-after`、`-reconcile-interval` 和 `-shutdown-grace` 控制执行、恢复与排空。River 恢复延迟必须长于执行、清理与持久化限制之和。SIGTERM 停止领取新任务，并排空至退出截止时间，随后取消活跃执行并等待有界清理。API 与 Worker 发行版本需要保持一致的运行时限制。本地执行器用于可信负载；进程组不是沙箱，也不保证约束任意脱离进程组的后代。

Go 客户端的 `RunWithEvents` 返回观察错误，不取消已接受的工作；显式取消使用 `Cancel`。具名 SSE 失败帧属于错误。事件流在观察到终态后、关闭之前再次读取事件，避免空查询与完成提交的竞态隐藏最后一条事件。

## 模型请求预算

第一方 DSH 适配器的 `-max-model-requests` 默认限制每次 Execution 最多 32 次模型流调用尝试。`Spec.maxModelRequests` 可缩小此上限；零表示采用服务默认值。准入将解析后的值独立于请求身份冻结，重提或修改默认值都不会重置预算。SQLite schema 10、PostgreSQL schema 7 持久化该值，Worker 协议 3 将其传递到打包运行时。显式迁移 PostgreSQL 后一并升级 API 与 Worker。缺少已解析预算的旧排队 DSH 任务在启动前失败，不重新获得额度。

运行时的 `llm/stream` 监听器在派发前预留一次调用。普通请求、重试、压缩和其他 DSH 辅助模型请求共用一个进程内计数器。耗尽后抛出 `execution_model_budget_exhausted`，不重试整次调用。不支持的适配器（包括自定义命令覆盖）不声明 `modelRequestBudget` 能力，并拒绝显式预算请求；缺少该能力的 Worker 不能领取带预算任务。这属于 DSH 应用限制，不是 HTTP 网关、token/费用额度、租户 RPM/TPM 配额，也不限制原生工具自行发起的网络调用。依据见[决策记录](../../.agents/notes/implemented/architecture/2026-09-15-execution-model-budget.md)。

## 共享模型账号额度

PostgreSQL 模型额度组关联消耗同一供应商账号额度的已验证连接。API 与 Worker 需要独立回调签名密钥，以及运行时可访问的许可地址。DSH 在每次实际供应商请求前预留额度；不支持的 Harness 和 SQLite 拒绝带额度的工作。RPM/TPM 组配置、估算规则、输出上限、超时参数和可运行快照见[限流案例](../../rank/examples/model-quota/README.md)。运行时回调仅接受调用专属身份，不接受普通服务令牌。使用 schema 7 和 Worker 协议 3 时，一并升级 API、Worker 与 Sandbox 运行时。

## OpenSandbox 运行时

设置 `-executor=opensandbox`、`-sandbox-endpoint`（包含 `/v1` 的 HTTP(S) 地址）、`-sandbox-scope`（稳定的部署拥有范围）及 `-sandbox-image`（不可变的 SHA-256 镜像），通过私有环境提供 `EXECUTION_SANDBOX_API_KEY`。API 与 Worker 必须选择兼容的执行后端；不匹配时等待，不回退到本地。镜像必须包含打包的 DSH Worker、UID/GID 为 1000 的 `node` 用户，以及[沙箱镜像](../../rank/deploy/k8s/sandbox.Dockerfile)中的受保护初始化程序。

`-sandbox-cpu` 接受正数核数或毫核；`-sandbox-memory` 接受正整数字节或 `512Mi` 等单位。`-sandbox-request-timeout` 限制管理与文件请求时间，`-max-artifacts` 限制保留文件数量。每次尝试将远程分配记录在 `runtime` 中；`externalHandle` 仍标识 Worker 拥有者。清理确认拥有关系与资源删除后才释放容量。创建响应丢失不自动重试；延迟发现可找回资源，但空搜索不能证明不存在迟到创建。

必须配置 `-sandbox-pool`、`-sandbox-capacity-cpu` 与 `-sandbox-capacity-memory`，通过 Execution 数据库共享独占的服务预算。启动时核对已存配置；`-configure-sandbox-pool` 显式更新后退出。预算变更后，需要同步更新 API/Worker 启动参数。修改目标必须排空排队、活跃及未确认工作。准入冻结包含镜像和资源限制的 `runtimePlan`；CPU 必须能表示为整数毫核。资源等待保持 attempt 为零，并在容量中记为 `resourceBlocked`。见[资源准入](../../.agents/notes/implemented/architecture/2026-09-15-execution-runtime-pool.md)。

将 `-sandbox-egress-policy` 设置为 JSON 对象，例如 `{"allow":[{"protocol":"tcp","cidr":"192.0.2.10/32","port":443}]}`，或提供 `EXECUTION_SANDBOX_EGRESS_POLICY`。空配置禁止新的外连，端口零允许该协议的全部端口。不可变运行计划保存规范化规则，配置不兼容的 Worker 不能领取任务。受保护镜像要求明确策略，在启动 execd 前安装 IPv4/IPv6 内核过滤器，随后移除网络管理能力。DNS 需要明确允许解析器。部署前提与限制见[出口决策](../../.agents/notes/implemented/architecture/2026-09-15-sandbox-egress.md)。

适配器支持 DSH 和确定性 Harness，不请求卷或 sidecar，要求同源且带鉴权的命令代理，并独立于生命周期服务执行受保护的容器截止时间。专用实验已验证有界可写存储与传输层出口控制；任意恶意代码隔离和完整生产策略仍未验证；服务部署不能注入主机挂载或未请求的 sidecar。配置、拥有关系与安全依据见[决策记录](../../.agents/notes/implemented/architecture/2026-09-15-execution-opensandbox.md)；可运行的部署及验收步骤见[本地实验](../../rank/deploy/k8s/README.md)。

## 产物存储

文件系统产物是轻量模式的默认值。设置 `EXECUTION_S3_ENDPOINT`、`EXECUTION_S3_BUCKET`、`EXECUTION_S3_ACCESS_KEY` 和 `EXECUTION_S3_SECRET_KEY` 可接入已有 S3 兼容桶；`-s3-region` 配置签名区域，`-s3-tls=false` 显式允许本地 HTTP 实验。Worker 文件先暂存本地，按执行 ID 与内容摘要组成的 key 上传，记录 SHA-256 和大小后才发布结果。`-max-artifact-bytes` 限制每个文件的大小。上传失败使执行失败，不重放模型调用。有效的 Worker 失败响应也在终态失败前发布已观察输出、用量与产物；仅有非零退出码不能把已知 Worker 错误改报为结果未知。已观察到的回复、用量和每个已确认产物引用按进度持久化；运行中或失败的执行也可能存在 `result`，只有 `status` 决定是否成功。详见[失败证据](../../.agents/notes/implemented/architecture/2026-09-15-execution-failure-evidence.md)。成功上传但未被引用的对象需要运维保留策略，尚未实现自动回收。

API 根据执行记录中的产物引用鉴权，并通过配置的存储读取。文件系统读取拒绝目录穿越与符号链接逃逸。S3 适配器支持供鉴权消费者使用、最长 15 分钟的签名 URL；HTTP 产物路由目前代理内容，不提供签名链接路由。S3 不保存进程内存，也不会把 Worker 工作目录变成共享存储。

无密钥 SQLite、PostgreSQL/River 与 S3 验证见[可运行生命周期案例](../../rank/examples/execution-lifecycle/README.md)；拥有权与恢复理由见[投递决策](../../.agents/notes/implemented/architecture/2026-09-14-execution-river-delivery.md)。

产物响应包含 `encoding`（`utf-8` 或 `base64`）及 `truncated`；一 MiB 预览上限作用于编码前的字节。校验二进制摘要前先解码内容。[失败证据决策](../../.agents/notes/implemented/architecture/2026-09-15-execution-failure-evidence.md)定义部分结果与产物授权语义。

## 本地连接发现

`POST /v1/local-model-connections/discover` 读取 Execution 主机上的可选配置，不访问 Provider，也不保存连接。Codex 扫描读取 `$CODEX_HOME` 或 `~/.codex` 下的用户级 `config.toml`：显式模型、所选 Provider、端点覆盖、环境变量凭据引用、直接 token 或 `auth.json` 中的 API Key；参见 [Codex 配置参考](https://developers.openai.com/codex/config-reference/)。文件必须是普通文件且不超过 1 MiB。来源失败会以不含凭据的问题说明返回，不影响其他可用候选。Profile 组合、项目设置、命令认证、订阅/OAuth 凭据和额外请求头需要手动配置。

DSH 扫描读取 `$DSH_HOME/settings.yaml` 或 `~/.dsh/settings.yaml` 中的 `llm-pi-ai.providers`、`llm-deepseek` 和 `agent-default-model`。每个 Provider 生成一个候选，使用已保存的默认模型或首个配置模型。显式协议优先；OpenAI、Anthropic、MiniMax、MiniMax CN 和 DeepSeek 也支持 DSH 的协议默认值。Pi-ai 端点需显式配置；第一方 DeepSeek 路由使用配置的端点、`DEEPSEEK_BASE_URL` 或官方端点。凭据依次从继承的环境变量、`.credentials.yaml`、DSH 主目录的 `.env` 解析。支持平铺凭据映射和版本 1 的 `refs`；不导入插件拥有的 `records` 或 grant。其他隐式目录值需要手动配置。

`POST /v1/local-model-connections/{id}/import` 接受可选的 `model` 覆盖值，重新读取候选，只将 Key 保存到凭据库。协议、端点、模型和凭据相同的连接会直接复用，包括手动创建的连接。并发导入串行处理。来源端点或模型元数据变化会使候选 ID 失效；Key 或所选模型变化会创建独立连接。导入不验证连接，也不绑定系统模型。重新发现会返回已有连接 ID，不更新已保存配置。
