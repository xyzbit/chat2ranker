# Execution 历史运行器

[English](README.md) | 中文

这个私有 DSH 插件使用结构化会话历史驱动一次 Execution 调用。Execution 的 DSH 适配器在存在 `Spec.messages` 时选择它；仅有提示词的调用继续使用 headless 驱动。使用适配器前需要构建宿主运行时。

## 输入与执行

`inputPath` 指向调用私有的 JSON 文件，包含 `executionId` 和 `messages`。角色为 `user`、`assistant` 和 `tool`；助手的 `toolCalls` 保留 ID、名称及 JSON 对象参数，每个工具结果通过 `toolCallId` 关联调用。解析器拒绝缺失文本、不支持的角色、重复调用、不匹配结果及被打断的调用与结果序列。输入以用户消息或一组已配齐的工具结果结束。

先前消息成为 `history/message` 事件，保留原始角色与调用关联，不伪造原生执行或用量。最后一条消息唤醒一个 Agent。如果该输入是工具结果，它在首次进入模型时排在注入的启动上下文之前，使助手调用与结果保持相邻。导入的工具调用不会再次执行。运行器刷新会话持久化、输出最后一条原生回复，然后请求启动器关闭。

## 模型请求预算

适配器还挂载 `model-budget.mjs`，传入 Execution 冻结的正数 `maxRequests`。全局 `llm/stream` 监听器在委托前同步预留，因此并行调用和辅助请求与普通调用、重试共享额度。预算耗尽时在供应商派发前抛出异常，以 `execution_model_budget_exhausted` 结束调用。原生会话事件保留尝试请求及失败结果，不额外注入模型可见消息。计数器由本次调用进程拥有，不通过重放已启动 Execution 恢复。

监听器控制经过 DSH LLM 服务的调用，不计量工具的原始网络操作，不能在运维移除插件后继续生效，也不限制费用预算。适配器缺少该能力时，Execution 拒绝显式预算请求。依据见[预算责任](../../.agents/notes/implemented/architecture/2026-09-15-execution-model-budget.md)。

## 共享供应商限流

Execution 配置账号额度后，同一监听器在每次供应商尝试前申请由 PostgreSQL 管理的许可，包括重试。输入预留采用系统提示、消息和工具序列化后的 UTF-8 字节数，再为每条消息增加 32 字节；输出预留使用冻结的模型上限。这是估算，不是供应商分词结果。正数的输入、输出及缓存用量报告替换估算；用量缺失或合成的零用量保持预留直至过期。上报失败也保留预留。

临时运行配置携带回调地址、尝试专属身份、输出上限和回调超时（`executiond -model-quota-timeout`，默认 5 秒）。插件在启动工具前删除对应环境变量，请求产物不保存该配置。许可最多一秒有效，网络往返耗时计入此期限。等待仍占用运行时期限和 Worker 槽位。这项协作式控制不计量工具原生网络流量或绕过 DSH LLM 服务的任意插件。参见[可运行限流案例](../../rank/examples/model-quota/README.md)。

## 验证

在仓库根目录运行 `node --test apps/execution-runtime/history-input.test.mjs`。执行 `pnpm run build:lib:host` 后，在 `execution/backend` 运行 `EXECUTION_TEST_DSH=1 go test ./harness -run TestDSHStructuredHistory`，验证真实 CLI 与供应商协议序列化。本地 HTTP 测试服务验证多轮文本、外部工具结果、额外用户输入及单次调用用量；它不验证远程供应商策略或模型质量。

[共用 SDK 快照](../../examples/jsonrpc-agent/tests/history.snapshot.ts) 验证导入事件经过两个 SDK 传输的表现。先通过 `uv sync --project python/sdk --frozen` 安装 Python SDK 测试环境，再在仓库根目录运行 `PYTHON="$PWD/python/sdk/.venv/bin/python" pnpm exec vitest run --config vitest.snapshot.config.ts examples/jsonrpc-agent/tests/history.snapshot.ts`。

## 限制

每次调用创建新的运行时。导入历史不恢复进程内存、文件系统变更、原生 CLI 会话状态或供应商专有的推理状态。适配器与 Worker 负责隔离、超时和取消。[生产设计](../../.agents/notes/proposed/architecture/2026-09-14-rank-production-execution.md) 定义这些职责。

将 `EXECUTION_TEST_RUNTIME` 设为解压后的 AgentKiln runtime 目录，可针对打包文件运行同一历史冒烟测试。
