# 发布流程

[English](release-policy.md) | 中文

Dataset 是通用的版本化资产。Agent（智能体）资产可以关联默认开发集和默认回归集的 Dataset 资产 ID，所有 Agent 版本共用这些默认值。新评测尚未选择数据集时会带出默认开发集，已有的明确选择保持不变。发布草稿解析两个默认集的最新版本，之后固定这些具体选择，直到用户替换。

## 审阅与发布

部署导航打开可搜索列表，列表项进入可直接访问的详情，Agent 版本链接返回对应配置。Agent 详情只提供一个「去发布」链接，进入该具体版本最近一次发布所属的部署，包含历史发布记录。没有匹配时，创建表单预选 Agent 并建议可用名称。创建后进入发布确认，固定传入版本。

每个部署展示一个端点和一个当前发布版本。测试与生产通过不同名称的部署区分。创建后端点处于未发布状态；新建 Agent 版本不会改变已发布目标。发布使用内部 `production` 别名，并须经过下述确认。已有别名引用和历史仍可读取；普通调用方使用稳定部署名称，详见[服务 API](serving-api.md#deployment-reference)。

产品将这些对外服务目标称为端点。概览直接展示执行限制；发布修订和检查决策直接出现在历史中。已发布端点提供接入侧栏，先选择或创建密钥，再复制可执行的 cURL 和 JavaScript 命令。已有 Key 需要粘贴保存的明文，因为持久化只有哈希。命令包含所选密钥，「复制给 Agent」使用环境变量代替。文档链接打开本地文档中心。

部署弹窗和 Rank 对话使用同一个发布确认。开发评测使用最新的同口径已完成 Run 对比当前与候选 Agent 版本：DatasetVersion、冻结的评测器配置（含 Judge 模型）和重复次数均一致。缺失记录保持空白，不代表零分或通过。操作者可以选择任何 DatasetVersion，或在独立实验中重新运行版本对比。

回归默认开启。确认后创建持久化发布任务；开启回归时同时创建独立的回归 Run。任务冻结候选、当前别名版本、选中的 DatasetVersion、开发评测证据、发布规则、操作者和对话确认引用。调度器等待回归 Run 结束，验证结果，并在同一事务中追加发布记录和完成任务。刷新或重启后恢复已记录的任务。回归不改变原对话的数据集或 Agent 选择。

回归失败时别名保持不变。操作者可以查看失败检查、准备新尝试，或在失败详情中填写非空原因后明确继续发布。确认前关闭回归会记录为 `regression_skipped`，不会声称评测通过。取消操作在同一事务中持久化任务和 Run 的取消状态。回归期间别名或策略变化会产生冲突，需要重新审阅。

## 发布规则

部署设置保存数值发布规则，与 Dataset 默认值和不可变执行限制相互独立。未配置规则的任务使用最低通过率 100%、有效 Trial 覆盖率 100% 和 24 小时有效期。保存的规则允许通过率 0–100%、覆盖率 1–100%，以及正整数小时有效期。

证据必须对应候选 Agent 和选中的 DatasetVersion，完成所有计划 Trial，有完成时间且至少包含一个有效 Trial。通过率为 `passed / validTrials`，覆盖率为 `validTrials / scheduledTrials`。检查使用未取整数值：三次中两次通过不满足 67%。无效 Trial 降低覆盖率。未来时间戳和过期证据不能通过校验。跳过回归和覆盖失败在审计记录中明确区分。

## 操作者 API

这些端点属于受信任的本地操作者服务。外部应用使用 [Responses API](openapi.yaml)。向受信任的本地环境以外暴露控制面之前，需要实现操作者认证。

| 方法 | 路径 | 输入或结果 |
|---|---|---|
| GET | `/api/deployments?agentVersionId={id}` | 返回发布过该具体版本的部署，按匹配发布从新到旧排序；可选 `tenantId` 默认使用配置的租户 |
| GET / PUT | `/api/agent-families/{id}/evaluation-defaults` | Dataset 资产 ID；写入携带 `expectedRevision` |
| POST | `/api/deployments/{id}/release-draft` | 别名、候选及可选的开发集/回归集 DatasetVersion 替换；返回对比与预期版本 |
| POST | `/api/deployments/{id}/release-comparison` | 草稿选择；创建独立对比实验 |
| POST | `/api/deployments/{id}/release-tasks` | 已审阅选择、预期版本及证据 Run ID、`actor`、可选 `runRegression`；必须携带 `Idempotency-Key` |
| GET | `/api/release-tasks?deploymentId={id}` | 最新 100 个任务；也可用 `experimentId` 查询对话任务 |
| GET | `/api/release-tasks/{id}` | 冻结的审阅、回归 Run 引用、状态与决策 |
| POST | `/api/release-tasks/{id}/cancel` | 取消尚未发布的任务 |
| POST | `/api/release-tasks/{id}/override` | `reason`；仅允许被阻止的任务 |
| GET / PUT | `/api/deployments/{id}/release-policy` | 数值规则；写入携带 `expectedRevision` |
| GET | `/api/deployments/{id}/aliases/{alias}/decisions` | 支持游标分页的冻结决策 |

省略数据集替换值时解析默认值；显式空字符串表示清空该项选择。提交任务携带审阅返回的 `expectedReleaseRevisionId`、`expectedPolicyRevision`、`expectedCandidateRunId` 和 `expectedCurrentRunId`。审阅证据变化时返回 409。同一幂等键和相同输入返回原任务，包括已发布的任务；输入变化则返回 409。

直接使用现有 Run 证据的 `aliases/{alias}/preflight` 和 `aliases/{alias}/releases` 端点也保留。该路径的评测发布需要配置策略 DatasetVersion，或提供带原因的人工覆盖。两种路径都审计决策，并在事务中比较预期的别名和策略版本。

## 对话与回滚

知道名称和 Agent 后，Rank 可以创建 Deployment、设置 Agent 默认集、准备发布和读取发布状态。用户通过确认操作开始发布。刷新后持久任务仍出现在对应的对话事件位置。存在多个匹配部署时需要选择目标。

回滚选择以前的不可变 Agent 版本并创建另一个发布任务。新会话解析新别名；已有会话保持精确的 Agent 版本和发布版本。流量权重和自动回滚不属于此流程。

## 验证

`RANK_RELEASE_BROWSER=1 RANK_RELEASE_SDK=1 pnpm rank:test` 运行 Go 测试、免密的 Control Host 与 worker/Responses 组装快照、桌面/手机浏览器流程及官方 OpenAI SDK 案例。需要 Playwright CLI（命令行界面）技能和已安装的 [Responses 客户端依赖](../examples/responses-client/README.md)。临时服务与数据库将验证和用户数据隔离；截图保存在 `output/playwright/`。免密 worker 验证编排，真实提供商的质量与成本验收仍属于独立的[本地验收](local-acceptance.md)流程。
