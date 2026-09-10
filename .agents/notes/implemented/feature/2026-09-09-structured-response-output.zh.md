# Agent Note：严格的 Response 结构化输出

状态：已实现

[English](2026-09-09-structured-response-output.md) | 中文

## 问题

公开 Responses 端点此前只能返回普通文本。如果接受 `text.format` 却不执行约束，调用方虽然请求了 JSON Schema 输出，Harness 或 Worker 仍可能用任意文本完成调用，这会使兼容性声明失真。

## 决策

`POST /v1/responses` 接受 `text.format.type: json_schema`，并要求 1 至 64 字符的名称、可选说明、对象 Schema 和 `strict: true`。Rank 会规范化并校验 Schema，把它随 Response 持久化、纳入幂等摘要，并复制到不可变的私有 Execution Spec。

Execution 协议版本 2 承载结构化输出要求。Agent Harness 会收到明确的仅输出 JSON 指令及 Schema。Worker 只有在最终输出可解析为唯一一个 JSON 值并通过 Draft 2020-12 校验时才会完成执行。成功结果会在 `Result.structured` 中保留紧凑 JSON；无效输出会使 Execution 失败，进而使公开 Response 失败。

Schema 不得使用外部 `$ref`、`$dynamicRef` 或 `$recursiveRef`。这会阻止 Worker 从网络或文件系统加载引用，并保证可以仅依赖持久化 Execution Spec 重建校验。Judge Execution 不接受结构化输出，因为其 Verdict 解析器拥有独立结果格式。

## 验证

Contract 测试覆盖有效值、类型错误、额外属性、Markdown 代码围栏、无效 Schema、大小限制和外部引用。Service 测试证明无效 Spec 会在持久化前被拒绝。Worker 测试证明有效 Mock 输出可以完成，而违反 Schema 的输出会失败。Rank HTTP 测试覆盖严格请求解析、格式持久化、不可变 Execution 传递和 Response 投影。无密钥组装服务快照通过 Rank、Execution Service 与 Worker 覆盖结构化输出。

## 备选方案

**只在 Execution 完成后由 Rank 校验。**未采用，因为所有 Execution 消费方都应获得相同的成功保证，而 Worker 是首个持有完整 Harness 输出的组件。

**只依赖提示词指令。**未采用，因为模型即使收到 JSON 指令，也可能输出 Markdown 代码围栏、说明文字或违反 Schema 的值。

**允许远程 Schema 引用。**未采用，因为校验会因此依赖可变的外部内容以及 Worker 的网络或文件系统访问。

## 结果

调用方可以依赖已完成的 `json_schema` Response 符合持久化 Schema。普通文本仍为默认值。Webhook 仍属于独立的后续生命周期工作。
