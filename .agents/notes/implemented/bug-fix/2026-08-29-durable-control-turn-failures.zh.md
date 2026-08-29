# Agent Note：持久化 Control Turn 失败

状态：已实现

[English](2026-08-29-durable-control-turn-failures.md) | 中文

## 问题

Control DSH 可能先输出流式片段，再在生成 `assistant/message` 前失败。临时片段随后消失，刷新后看不到失败轮次，用户也没有可持久查看的原因和重试入口。

## 决策

Control Host 对成功和失败的 Turn 都会刷新并同步 DSH transcript。失败 Turn 会追加一条 ID 确定的 Rank Message JSONL 错误消息，其中包含简短的用户说明和脱敏后的技术信息。前端在对话中渲染该消息，默认折叠技术详情，并允许重试它前面的用户消息。流式片段仍属于临时状态，不会作为完成答案持久化。

## 考虑过的替代方案

**只保留输入框错误条。** 刷新后错误会丢失，而且错误与对应请求在视觉上分离。

**持久化模型的部分输出。** 在工具执行或最终总结前中止的前缀不是可信答案，也难以与完整内容区分。

## 结果

失败会保持可见和可操作，同时不会把未完成输出伪装成最终答案。每个失败的 DSH Turn 会在 Rank 中额外保存一条合成的 assistant 消息；确定性 ID 保证 transcript 重复同步仍然幂等。
