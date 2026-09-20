# Agent Note: Agent 全生命周期平台与 Responses 兼容对外服务

Status: proposed

[English](2026-09-08-agent-lifecycle-serving.md) | 中文

## Problem

AgentKiln 可以定义不可变的 Agent 版本、在隔离 Worker 中运行这些版本、评测重复 Trial、保留原生运行产物，并归集 token 用量和成本。产品 API 目前止于实验和评测 Run，因此通过评测的 Agent 无法在没有另一套部署系统的情况下成为供外部应用稳定调用的端点。

如果直接暴露内部 Execution Service，调用方将能够控制 harness、模型连接、系统提示词、工具、Skill、工作区相关元数据和产物路径。它的单次 `prompt` 到 `output` 请求也没有定义租户授权、发布别名、会话连续性、公开流事件、请求留存或成熟客户端协议的兼容性。

因此，产品需要新增对外服务控制平面与公开数据平面，在复用不可变 Agent 版本和隔离执行能力的同时，保持外部调用、Execution 尝试与评测 Run 三者相互独立。

## Proposal

将 AgentKiln 定位为覆盖准备、评测、发布、服务和观测五项相连能力的 Agent 全生命周期平台。现有实验产品继续承担创作和评测应用；新的对外服务领域负责 Deployment、发布别名、公开 Response、Conversation、租户策略和生产审计记录。

公开数据平面将以 [OpenAI Responses](https://platform.openai.com/docs/api-reference/responses/create) 兼容子集作为主要应用协议。核心 Task、产物和推送通知语义实现后，可以增加 [A2A](https://github.com/a2aproject/A2A/blob/main/docs/specification.md) 适配器，把相同 Deployment 投影为可发现的远程 Agent。[MCP](https://modelcontextprotocol.io/specification/2025-06-18/server/index) 继续用于向 Agent 暴露工具和资源，而不是作为调用托管 Agent 的主要协议。

内部 Execution Service 继续保持私有。对外服务网关把 Agent 引用解析为一个不可变 AgentVersion，根据可信平台状态构造完整的内部执行规格，持久化已受理的 Response，并且只把精确快照提交给 Execution Service。公开调用方不能覆盖 harness、模型连接、系统提示词、内置工具、Skill、密钥引用、工作区或 Executor。

已经实现的[发布注册表基础](../../implemented/architecture/2026-09-08-serving-release-registry-foundation.md)、[Responses 对外服务 MVP](../../implemented/feature/2026-09-09-responses-serving-mvp.md)、[有状态续接](../../implemented/feature/2026-09-09-stateful-response-continuation.md)、[严格结构化输出](../../implemented/feature/2026-09-09-structured-response-output.md)、[冻结式富输入](../../implemented/feature/2026-09-09-frozen-response-input.md)与[调用方 Function 往返](../../implemented/feature/2026-09-09-caller-function-round-trip.md)提供本地 Deployment、发布历史、富输入调用、认证、策略、持久化、恢复、版本固定的多轮交互、受 Schema 约束的输出、Function 与审批能力。其余需求描述更完整的生命周期工作。

## Product requirements

| ID | Requirement | Required behavior |
|---|---|---|
| R1 | Agent 生命周期 | AgentVersion 具有明确的 `draft`、`published` 和 `retired` 状态；只有已发布且可运行的版本才能接收发布别名。 |
| R2 | 稳定部署 | 租户范围内的 Deployment 指向一个 Agent Family，并在不改变不可变版本的情况下拥有 `production`、`staging` 和自定义别名。 |
| R3 | 精确解析 | 网关在受理请求时只解析一次别名，并在执行前记录请求引用、精确 AgentVersion 和发布修订。 |
| R4 | 公开调用 | 客户端可通过一个经过身份验证且由 OpenAPI 3.1 描述的 API 创建、流式接收、查询和取消 Response。 |
| R5 | 会话连续性 | 除非调用方显式新建 Conversation 或请求受支持的迁移，否则续接请求保持原始 AgentVersion。 |
| R6 | 隔离 | 每次调用都使用现有 Execution Service、隔离 Worker、私有工作区、限定范围的凭证，以及 Deployment 自己的工具和网络策略。 |
| R7 | 发布证据 | 晋级记录本次决策使用的精确评测 Run、DatasetVersion、EvaluatorVersion、指标、操作者、原因和策略结果。 |
| R8 | 治理 | API Key、租户范围、Deployment 范围、限流、并发、预算、留存、产物访问和审计导出可以分别配置。 |
| R9 | 可观测性 | 每个 Response 都能关联租户、Deployment、请求别名、发布修订、AgentVersion、Conversation、Execution 尝试、用量、成本和最终状态。 |
| R10 | 反馈 | 获得授权的生产样本可以进入评审队列并成为新的版本化测试集用例，但不能改变历史 Run 或调用记录。 |

### Quality and security requirements

- 幂等键的作用域是租户和端点。使用同一幂等键提交不同的规范化请求时必须失败，禁止返回或启动无关任务。
- 别名晋级针对当前发布修订执行 CAS（compare-and-swap）。回滚通过新增修订重新指向较早的精确 AgentVersion，禁止重写历史。
- 未知或不受支持的 Responses 字段返回稳定的 `unsupported_parameter` 错误。兼容端点禁止静默忽略请求的行为。
- 模型提供方凭证保留在 Execution Service 凭证存储中。公开请求、Response 记录、事件、日志和产物禁止包含原始凭证。
- 公开流只包含受支持的 Responses 事件。原生 Worker 事件和特权 Trace 保留在受 Deployment 权限控制的可观测 API 后面。
- 产物下载使用租户范围授权或短期签名 URL。现有内部 `executionId + path` 读取接口不是公开产物 API。
- 准入层在启动 Worker 之前执行请求大小、速率、并发和成本控制。过载返回明确且可重试的失败，禁止创建无界队列。
- 每项模型可见输入、工具结果和续接项都能从持久的 Conversation 与 Response 记录中重建。

### Initial non-goals

- 兼容端点不暴露任意模型提供方调用，也不允许调用方在调用时改变 AgentVersion。
- `latest` 不表示最新草稿，创建版本也不会移动 `production`。
- 评测 Run 不是生产 Response，Execution 记录也不是公开生命周期对象。
- 首个版本不要求逐字节支持 OpenAI Responses 的所有字段、内置工具和事件。
- 多 Agent 对比继续属于 Rank RunGroup。公开服务的每个 Response 只调用一个已解析 Agent Deployment。

## Version and release semantics

公开 `model` 字段承载 Agent 引用，因为成熟的 Responses 客户端要求使用这个选择字段。租户把 `<handle>@v17` 解析为精确不可变版本。`latest`、`production` 和其他名称都是 DeploymentAlias，其当前指针标识一条仅追加的 ReleaseRevision。发布 AgentVersion 会推进系统维护的 `latest` 别名，但绝不移动 `production`。Handle 和别名均属于租户范围。

已受理 Response 的 `model` 返回解析后的精确引用。Response Header 同时公开请求引用；通过别名解析时还会公开对应发布修订，使标准 SDK 保持兼容，同时让运维人员可以证明路由决策。精确版本引用不会虚构发布修订。重试复用已持久化的精确版本。即使别名已经向前移动，`previous_response_id` 也继续使用同一 Conversation 和版本；显式 Conversation 迁移是一项独立的审计操作。

晋级到 `production` 需要发布策略结果。首个实现可以允许已授权的人工覆盖，但必须填写原因，而且记录必须区分策略通过与人工覆盖。自动策略可以要求具名评测套件、最低有效 Trial 覆盖率、通过率或可靠性阈值、最高成本或延迟，以及相对于 AgentVersion 内容摘要的新鲜度。

## Public protocol

已经实现的兼容端点支持 `model`，文本、图片和文件 `input`，调用方 Function `tools`、受 Scope 控制的审批、`stream`、`background`、`metadata`、受策略控制的 `instructions`、持久化 `store: true`、`previous_response_id` 续接和严格的 `text.format` JSON Schema 输出。只有当持久记录可以重建相应语义，并且 Deployment 策略可以执行限制时，才会加入 `max_output_tokens`。

```json
{
  "model": "customer-support@production",
  "input": [
    {
      "role": "user",
      "content": [
        { "type": "input_text", "text": "Review refund request 123" }
      ]
    }
  ],
  "stream": true,
  "metadata": { "order_id": "123" }
}
```

| Operation | Purpose |
|---|---|
| `POST /v1/responses` | 解析 Deployment、持久化已受理的 Response，并启动或排队执行。 |
| `GET /v1/responses/{id}` | 返回当前持久 Response 投影和最终输出。 |
| `POST /v1/responses/{id}/cancel` | 请求取消，但不删除审计历史。 |
| `GET /v1/responses/{id}?stream=true` | 标准创建流断开后，从游标恢复受 Deployment 权限控制的事件流。 |

兼容流把持久事件映射为受支持的 Responses 事件名，例如 `response.created`、输出项新增、文本增量、工具调用项和最终完成或失败。DSH commentary、原生 CLI 消息、Worker 重试和沙箱诊断继续作为内部可观测数据，除非标准输出项能够无损表达其语义。

## Core flow

[打开 Agent 发布与对外服务流程图](../../../../rank/docs/diagrams/agent-serving-lifecycle.html)。

1. Rank 评测一个精确 AgentVersion，并存储 EvaluationEvidence；该记录标识冻结的 DatasetVersion、EvaluatorVersion、Run、指标和内容摘要。
2. Release Controller 评估策略，追加 ReleaseRevision，并通过比较交换推进 DeploymentAlias 当前指针。公开信任边界拒绝客户端直接访问 Execution。
3. 对外服务网关验证调用方身份，应用租户和 Deployment 限制，并从已提交的发布注册表解析请求别名。
4. 网关在分发前以原子方式持久化 Response 的请求摘要、请求引用、发布修订、精确 AgentVersion 和幂等键。
5. Execution Service 持久化内部 Execution，并使用服务端构造的配置和限定范围的凭证启动隔离 Worker。
6. 持久 Execution 事件更新 Response 投影。网关只发出兼容的公开事件，并记录用量、成本、产物和最终状态。

## Data model

[打开 Agent 对外服务实体模型](../../../../rank/docs/diagrams/agent-serving-data-model.html)。

| Entity | Ownership and invariants |
|---|---|
| AgentFamily | 租户范围的 Handle 和描述身份；拥有不可变版本。 |
| AgentVersion | 冻结的 harness、模型引用、系统提示词、工具、Skill 和策略摘要；生命周期状态变化不会修改其执行规格。 |
| EvaluationEvidence | 引用一个 AgentVersion 的精确 Run、DatasetVersion、EvaluatorVersion、指标、新鲜度摘要和策略结果。 |
| Deployment | 可公开调用的名称、所属 AgentFamily、留存、配额、工具、网络和授权策略。 |
| DeploymentAlias | `latest`、`staging`、`production` 或其他路由通道；当前修订指针是已提交的读取投影。 |
| ReleaseRevision | 仅追加的别名变更，包含精确 AgentVersion、操作者、证据、原因、预期前序修订和生效时间；回滚也追加新修订。 |
| Conversation | 固定到一个 Deployment 和精确 AgentVersion 的持久续接上下文；每个新受理的 Response 都有一个。 |
| Response | 公开生命周期记录，包含请求摘要、请求别名、解析版本与发布修订、状态、输出项、用量、成本和 Conversation 引用。 |
| Execution | 私有尝试生命周期和原生产物；一个 Response 可以引用多次尝试，但重试保持同一个 AgentVersion。 |

## Core technical decisions

### Control plane and data plane

Agent 创作、评测、发布策略、别名晋级、API Key 管理和审计查询属于控制平面。请求准入、别名读取、Response 持久化、流式传输和执行提交组成生产数据平面。首个实现可以把这些角色部署在一个二进制进程中，但包和 Repository 依赖必须保持职责分离，使生产流量不依赖加载实验工作区。

### Registry ownership

生产服务开始依赖 AgentFamily 和 AgentVersion 后，这两个概念不能继续只属于 Rank。需要把它们的 Repository 与应用层职责提取到产品 Agent Registry，供 Rank 和 Release Controller 共同使用。Rank 继续拥有 DatasetVersion、EvaluatorVersion、Experiment、Run、RunGroup 和 Trial 策略。

### Identifiers and pagination

物理表使用自增 `BIGINT` 主键完成关联、索引和基于游标的翻页。公开资源同时持有唯一 UUIDv7，只有该不透明标识符可以跨越 API、事件、产物和服务边界。SQLite 使用 `INTEGER PRIMARY KEY`，PostgreSQL Adapter 使用 Identity 列。现有 Rank 字符串标识符在 Agent Registry 职责提取时迁移，不增加原地兼容层。

列表 API 在稳定且有索引的 `(created_at, id)` 等字段组合上使用 Keyset Pagination。有序事件流和发布历史使用范围内单调递增的整数序号。随机 UUID 顺序和深度 `OFFSET` 都不是分页机制。

### Trusted execution construction

对外服务网关只接受公开输入项和 Deployment 选择器。网关加载精确 AgentVersion 与 Deployment 策略，然后构造内部执行规格。这样可以阻止公开调用方选择凭证、削弱隔离、改变系统提示词、挂载未声明工具或读取任意产物。

### Durable event projection

Response 是公开状态和输出项的真源；Execution 是私有 Worker 生命周期的真源。投影器把有序 Execution 事件折叠为有序 Response 事件，并持久化游标。重连从该游标开始；即使没有活跃 Worker 或流，也能重建最终 Response。

### Conversation ownership

Conversation 拥有有序输入和输出项历史，并固定到一个精确 AgentVersion。每个 Conversation 只能有一个活跃写入者。每个新受理的 Response 都有 Conversation；省略 `previous_response_id` 会新建一个，传入时则通过前一个 Response 定位，并拒绝跨租户或不兼容 Deployment 的续接。

### Evaluation-governed release

发布策略消费不可变 EvaluationEvidence，而不是查询可变实验选择。策略评估与别名追加必须在同一个事务中完成，或通过 Outbox 实现幂等提交。策略失败会产生可审计决策，但不能移动别名；已授权的人工覆盖必须显式记录，不能伪装成策略通过。

## Delivery phases

| Phase | Scope | Main tasks | Exit condition |
|---|---|---|---|
| 0. 领域基础 | 将可复用 Agent 生命周期状态与 Rank 评测状态分离。 | 增加 AgentVersion 生命周期和摘要；提取 Agent Registry 职责；增加 Deployment、DeploymentAlias、ReleaseRevision、Response、API 凭证和审计 Repository；定义品牌化公开标识符、内部 Identity 主键、Keyset Cursor 和迁移。 | Rank 继续通过新的 Registry 创建和评测精确 Agent 版本，同时不存在对外服务端点。 |
| 1. 安全服务 MVP | 通过 Responses 兼容子集提供纯文本、无状态 Agent 调用。 | 增加网关身份验证与 Scope；解析精确版本、`latest` 和 `production`；创建幂等 Response；支持同步、SSE、后台运行、查询与取消；配置配额；签名产物；支持带证据和覆盖记录的人工晋级。 | 外部 OpenAI 兼容客户端可以调用生产别名，而且每个已受理请求都能证明其精确版本、发布修订、Execution 尝试、用量和成本。 |
| 2. 有状态 Agent 交互 | 支持持久多轮以及更丰富的输入和输出。 | 增加 Conversation 和 `previous_response_id`；图片与文件输入项；结构化输出；调用方 Function Tool 与工具结果续接；需要审批状态；Webhook 投递。 | 断线客户端可以恢复一个 Conversation，并在无版本漂移、无持久事件丢失的情况下完成工具或审批往返。 |
| 3. 自动发布运维 | 把评测策略连接到受控流量变更。 | 增加具名发布策略；自动晋级到 Staging；校验策略新鲜度；增加 canary 目标与流量权重；健康回滚；线上采样和评审转测试集；发布仪表盘与告警。 | 一个版本可以根据已记录策略从评测进入 canary 和生产，回滚也能保留完整历史。 |
| 4. 生态协议 | 向 Agent 生态暴露相同 Deployment。 | 增加 A2A Agent Card 和 Task 适配器；推送通知；TypeScript 与 Python SDK 辅助接口；一致性 fixture；企业身份与审计导出适配器。 | Responses 与 A2A 调用解析到同一个发布修订，并产生等价的 Response、Execution、用量与审计版本链。 |

每个阶段都要更新公开 OpenAPI 文档、包和产品 README、架构参考，以及适用的 TypeScript 与 Python 客户端投影，同时补充单元测试、集成测试和一个覆盖用户可见行为的免密组装 transcript 或协议 snapshot。公开协议 fixture 必须覆盖取消、重连、幂等冲突、活跃请求期间的别名移动、授权拒绝、配额拒绝、Worker 重试和产物访问。

## Alternatives considered

**直接暴露 Execution Service。** 这样可以复用现有 HTTP 端点，但会暴露特权配置和内部生命周期术语，缺少租户与发布语义，并使外部客户端耦合到为可信 Rank 调用方设计的请求。

**把生产调用作为另一种 Rank Run。** Run 会冻结 DatasetVersion、EvaluatorVersion、Trial 和质量聚合。生产 Response 具有不同的授权、延迟、Conversation、留存、幂等与输出语义；合并二者会同时削弱两类记录。

**只使用 A2A。** A2A 为 Agent 间协作提供发现、Task、产物、流与推送通知语义，但普通应用更常需要直接请求 API 和现有模型客户端集成。A2A 应继续作为相同对外服务领域之上的适配器。

**使用 MCP 作为对外服务协议。** MCP 向模型宿主暴露工具、资源和提示词。把完整的已部署 Agent 当作工具，会把其发布、Conversation、Task、用量和生产生命周期隐藏在一个更窄的抽象中。

**首先实现自定义公开协议。** 自定义 schema 可以精确映射内部 DSH 事件，但会增加客户端工作量，并诱使内部 Worker 细节泄漏到公开 API。严格的 Responses 兼容子集能够提供成熟封装，同时对不受支持的行为明确失败。

**每次重试或轮次都重新解析 `production`。** 这会让一个已受理请求或 Conversation 在晋级后跨越版本。因此，解析结果在受理时冻结，并由后续请求继承。

## Acceptance criteria

- 根产品文档把 AgentKiln 定位为 Agent 全生命周期平台，同时区分已经交付的评测行为和规划中的对外服务平面。
- 控制平面模型支持不可变 Agent 版本、明确发布状态、仅追加发布修订、评测证据和可审计的人工覆盖。
- 公开请求不能选择内部凭证、harness 配置、系统提示词、内置工具、Skill、工作区、Executor 或原始产物路径。
- 精确版本、`latest`、`production`、重试、回滚和续接测试证明每个已受理 Response 始终固定到一个 AgentVersion；通过别名解析时还固定到一个发布修订。
- 文档规定的 Responses 子集可以通过 OpenAI 兼容客户端工作，并提供稳定的不受支持参数错误和可重连的有序事件。
- 租户隔离、API Key Scope、幂等冲突、配额、成本归属、签名产物访问、密钥脱敏、取消与崩溃恢复均有集成覆盖。
- 晋级策略消费不可变评测证据，阻止陈旧或失败证据，记录人工覆盖并保留完整别名历史。
- Response 与评测 Run 可以分别查询，同时共享精确 AgentVersion 和 Execution 引用。

## Risks

OpenAI 兼容 SDK 可能假设存在声明子集之外的字段或流事件。网关必须发布兼容矩阵，拒绝不受支持的参数，并对受支持 SDK 版本运行一致性 fixture。

在对外服务产生用户价值之前提取 Agent Registry 职责，会改变 Rank 持久化和 API 依赖。阶段 0 必须保持现有 Rank 行为，并避免产生 AgentFamily 或 AgentVersion 的第二个可写副本。

长生命周期 Conversation 会带来 Worker 所有权、留存、上下文增长和 Deployment 升级压力。该设计把 Conversation 固定到一个精确版本，并要求一个活跃写入者，但在阶段 2 之前仍需明确压缩与迁移限制。

工具可能执行不可逆的外部操作。Deployment 策略、限定范围的凭证、沙箱与网络限制、审批事件和审计记录能够降低风险，但不能让所有工具天然安全；运维人员仍需为其发布的工具负责。

对外服务流量会改变可用性与成本预期。公开部署前，生产适配器需要持久 Repository、对象存储、队列背压、Worker Lease、限流和运维 SLO；本地 SQLite 与文件系统组合仍然是开发配置。
