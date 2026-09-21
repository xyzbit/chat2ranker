# Agent Note: Structured conversation history

Status: implemented

English | [中文](2026-09-14-rank-structured-history.zh.md)

## Problem

Flattening a conversation into one user prompt loses role and tool-call semantics. Importing that history as native Harness executions would instead invent turns, tool activity and usage. A deployment cannot claim equivalent multi-turn behavior across adapters that accept different inputs.

## Decision

[Execution input](../../../../execution/backend/contract/messages.go) accepts exactly one of `prompt` and `messages`. Structured history is an Agent capability advertised by `structuredHistory`; unsupported Rank continuations reject before Response admission, and Execution and runtime dispatch check the capability again. DSH and the demonstration adapter support it. A configured command opts in through an explicit `{input}` JSON-file argument. Native Codex, Claude Code and Hermes adapters retain prompt-only behavior until their history integration is implemented.

[Rank](../../../../rank/backend/internal/app/serving.go) freezes ordered user, assistant and tool messages from the pinned conversation. Assistant calls carry original IDs, names and object arguments; tool outputs retain matching IDs. Additional user text after a result remains a separate user message. Pending calls require consecutive results before other messages. Attachments retain their frozen references and materialized input files; text markers do not imply native multimodal model input.

The [DSH history runner](../../../../apps/execution-runtime/README.md) validates its private input file, imports prior messages as `history/message`, and wakes one Agent with the final input. Imported assistant recall uses plugin provenance. The event contributes model context without claiming a native turn, model invocation, tool execution or token usage. A tool result entering the first step precedes injected startup context, preserving adjacent call/result messages. Compaction counts tool blocks from derived messages, including imported messages and current tool input. Session references include imported human/assistant text and exclude tool outputs and injected context.

`history/message` is required-on-read. Older Harness builds refuse the unknown event; the pre-release session format remains `0`. Execution protocol version `2` is retained because a structured request omits `prompt`, which older Execution validators reject rather than flatten. API, Worker and runtime images must be deployed together; unsupported versions fail closed, without a translation fallback.

## Alternatives considered

**Embed role-tagged JSON in one prompt.** Easy to pass to every CLI, but it does not create supplier-native assistant/tool messages or enforce tool correlation.

**Fabricate completed Harness turns from remote messages.** Misrepresents foreign history as local execution and contaminates usage, lifecycle invariants and recovery decisions.

**Resume every adapter's native session.** Requires adapter-specific durable state, isolation and provider replay rules. Structured history is useful without claiming that stronger recovery guarantee.

## Consequences

The real built DSH CLI smoke sends three scenarios to a local OpenAI-compatible HTTP service: user/assistant/user, caller tool continuation and tool output with additional user input. It checks provider roles, call IDs, adjacency and single-invocation usage. The [TypeScript/Python SDK snapshot](../../../../examples/jsonrpc-agent/tests/history.snapshot.ts) records imported events, the model-visible conversation and only the newly produced answer. Core tests cover immutable replay, malformed imports, between-turn invariants, reference projection and compaction pairing. Packaged Rank trials check continuation, cancellation/restart and unsupported-adapter rejection before admission.

The portable input does not restore a process, workspace, provider reasoning state or arbitrary native CLI session. External function selection still uses Rank's existing reviewed function-output envelope; this change preserves completed call/result history and does not add native external tool declarations to every Harness. The [platform lab](2026-09-14-rank-platform-lab.md) covers DSH continuation through the cluster. Whole-runtime sandbox isolation remains in the [production proposal](../../proposed/architecture/2026-09-14-rank-production-execution.md).
