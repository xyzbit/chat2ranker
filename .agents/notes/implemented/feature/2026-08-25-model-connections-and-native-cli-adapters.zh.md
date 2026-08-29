# Agent Note：模型连接与原生 CLI Adapter

状态：已实现

[English](2026-08-25-model-connections-and-native-cli-adapters.md) | 中文

## 问题

Agent 版本可以指定 Runner 和模型，但不能引用用户管理的 OpenAI 兼容端点。Codex、Claude Code 和 Hermes 虽然出现在 UI 中，却没有第一方事件、用量或安装状态处理。若通过 Rank 或持久执行记录传递 Provider Key，还会让凭据离开真正启动 worker 的组件。

## 决策

Execution Service 负责模型目录、连接元数据、验证、凭据与成本解析。SQLite Repository 保存端点元数据、单模型本地价格覆盖和验证状态，可替换的凭据存储用私有文件权限单独保存 API Key。Rank 在不可变 Agent 版本和 Run 快照中只保存 `modelConnectionId`。LocalExecutor 仅在启动 worker 前解析 Key；execution-worker 临时接收它，并写入已经脱敏的 request 产物。

连接验证会调用 Provider 模型目录，并通过 Chat Completions、Responses 或原生 Anthropic Messages 执行一次最小请求。目录同步失败时保留上次发现模型；端点不支持 `/models` 时仍可通过最小请求验证显式模型。创建 Agent 时只接受已验证连接，并校验 Runner 兼容性：Codex 必须使用 Responses，Hermes 必须使用 Chat Completions，DSH 三者都支持，Claude Code 保持自己的登录机制。

Codex、Claude Code 与 Hermes 使用原生 CLI Adapter。Codex 解析 JSONL 生命周期与用量事件，Claude Code 解析 stream-json 的结果、成本与用量字段，Hermes 读取 usage report。Probe 会真正执行已安装 CLI 的版本命令，因此可以区分安装损坏、未安装与未配置。原生 Codex 与 Claude Code 执行会复用认证，但仍使用隔离的任务工作区且不持久化 CLI Session。默认 Codex 运行使用 `--ignore-user-config`，Claude Code 使用 `--safe-mode`，冻结的 Agent 模型会作为单次调用参数传入。绑定自定义连接的 Codex 则使用私有生成的 `CODEX_HOME` 和临时 Provider 凭据。部署 argv 覆盖仍可用于自定义安装。

成本解析首先信任 Provider 明确返回的实际费用，其次按匹配的连接覆盖价格计算，最后按内置 Provider 目录计算。每个计算结果都携带生效价格快照并标记为估算。缺少用量记录或价格时，成本保持未知。CLI 返回的估算值不会被当成 Provider 实际费用。

实验工作区包含精简的模型连接中心，提供 DeepSeek、MiniMax、智谱 GLM、OpenAI、Claude 和 Kimi 官方模板。MiniMax、智谱与 Kimi 使用中国区官方 OpenAI 兼容端点作为默认值；国际站端点仍可通过自定义连接配置。选择厂商后只展示该厂商可搜索模型，并继承端点、协议、美元默认价格、来源、更新时间和长上下文等计价说明。人民币官网价格按 `¥7.00 = $1` 固定折算为美元估算，并在模型计价说明中披露；这使实验图表保持单一币种，也不把折算值冒充 Provider 实际账单。平台目录与连接发现模型合并展示并标注来源；手动模型 ID 渐进展开；已有凭据提供重新同步操作。高级本地价格覆盖、凭据输入和验证仍然可用，但不会增加默认流程负担。Agent 编辑器展示 Runner 可用状态、兼容且已验证的连接和发现的模型 ID，并可在不丢失草稿的情况下跳转到连接中心。Rank 对话可以创建或更新非敏感连接元数据和价格；API Key 只能通过安全表单输入。

## 结果

Provider 凭据不会进入 Rank 状态、执行历史、SSE 或产物。版本化 Agent 可以复用连接而不复制 Key，Run 仍冻结连接 ID 与模型选择。本地文件凭据适用于单机使用；服务器部署可将凭据存储替换为 Vault 或 KMS，而不改变 Rank 或 Adapter API。已知 Provider 成本保持精确，按目录或连接计算的值会明显标注为近似；缺少计价配置时明确展示缺失状态，而不是显示误导金额。

## 考虑过的替代方案

- **信任所有 CLI 返回的成本字段。** 未采用，因为 CLI 的估算成本可能仍按厂商默认价格计算，而实际请求已经路由到其他 Provider 或模型。
- **使用一张不可修改的全局价格表。** 未采用，因为网关和协议价不同。内置目录只是本地默认值，连接可以覆盖价格。
- **把所有连接配置都塞进 Agent 表单或对话。** 未采用，因为这会加重 Agent 创建负担，并且凭据绝不能进入对话记录。
