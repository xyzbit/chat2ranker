# 调用排查与评测

[English](response-feedback.md) | 中文

本教程从应用返回的 Response ID 开始，生成一条经过审核的评测用例。可以先运行[接入案例](../examples/responses-client/README.md)获得调用记录。

## 1. 定位调用

打开调用记录，按 Response ID 或端点名称搜索，使用状态和端点进一步筛选。详情地址为 `#/responses?responseId=<id>`，刷新不会丢失位置。Agent 链接打开该调用使用的不可变版本；上一轮与整个会话链接用于检查历史上下文。

检查输入输出、追加指令、调用方工具、失败代码和持久化事件。Token 与耗时对应本次调用。费用缺失代表没有定价信息，不代表免费。管理详情不包含附件二进制数据和凭据材料。

## 2. 保存审核后的用例

调用结束后点击「加入数据集」。选择现有数据集或填写新名称，填写用例标题，编辑输入去除敏感信息，并独立填写期望结果。实际输出不作为参考答案。

用例保存独立文本。续聊用例需要将必要历史事实整理进输入；追加指令、附件与调用方工具不会自动重放，应明确补充必要指令和工具结果。保存前勾选审核确认。这是人工确认，不是自动匿名化。

新数据集从版本 1 开始。追加用例会保留已有评测器、评分规则与用例并创建下一版本。选择过时版本返回 `dataset_version_changed`，需刷新并重新选择最新版本。同一数据集已收录该 Response 时返回 `response_already_captured`。用例保留来源 Response、端点、Agent 版本和采集时间。旧数据集版本和 Run 快照保持不变。

## 3. 评测与复用

保存后点击「去评测」，Rank 展示带有来源 Agent 版本和新数据集版本的运行确认卡片；启动前检查重复策略与 Judge 配置，页面跳转不会开始执行。Judge 连接失败属于评测基础设施问题，不能据此判定候选答案错误。

当该通用数据集适合后续检查时，可将其设为 Agent 的开发或回归默认值。评测和发布自动带出默认值，也允许替换。已保存用例可跳回来源调用查看。

## 管理 API

`GET /api/responses` 支持 `query`、`conversationId`、`deploymentId`、`status` 和 `before`。`GET /api/responses/{id}` 返回排查详情。`POST /api/responses/{id}/dataset-cases` 接收 `tenantId`、`datasetName` 或 `baseDatasetVersionId` 二选一，以及 `title`、`input`、`expected`、`reviewed: true`，返回新建的 DatasetVersion。这些属于本地管理 API，不属于调用 Key 的权限范围。远程部署需要独立管理认证，参见[Serving API](serving-api.md)。
