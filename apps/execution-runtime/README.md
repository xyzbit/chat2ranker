# Execution history runner

English | [中文](README.zh.md)

This private DSH plugin drives one Execution invocation from structured conversation history. The Execution DSH adapter selects it for `Spec.messages`; prompt-only invocations retain the headless driver. Build the host runtime before using the adapter.

## Input and execution

`inputPath` identifies an invocation-private JSON file containing `executionId` and `messages`. Roles are `user`, `assistant` and `tool`; assistant `toolCalls` retain IDs, names and JSON-object arguments, and each tool result names its `toolCallId`. The parser rejects missing text, unsupported roles, duplicate calls, unmatched results and interrupted call/result sequences. Input ends in a user message or a completed set of tool results.

Prior messages become `history/message` events with original roles and call correlation, without fabricated native executions or usage. The last message wakes one Agent. When that input is a tool result, its first model-entry position precedes injected startup context, keeping the assistant call and result consecutive. Imported tool calls are never executed again. The runner flushes the session, prints the final native answer and requests launcher shutdown.

## Model request budget

The adapter also mounts `model-budget.mjs` with the Execution's frozen positive `maxRequests`. Its global `llm/stream` listener reserves synchronously before delegation, so parallel calls and auxiliary requests consume the same allowance as ordinary calls and retries. Exhaustion throws before provider dispatch and terminates the invocation with `execution_model_budget_exhausted`. Native session events retain attempted requests and the resulting failure; no extra model-visible message is injected. The invocation process owns the counter; it is never resumed by replaying a started Execution.

The listener controls calls through DSH's LLM service. It does not meter raw network operations by tools, survive an operator removing the plugin, or enforce monetary budgets. Execution rejects a requested budget when an adapter lacks this capability. See [budget ownership](../../.agents/notes/implemented/architecture/2026-09-15-execution-model-budget.md).

## Shared provider quotas

When Execution configures an account quota, the same listener requests a PostgreSQL-backed permit before every provider attempt, including retries. Input reservation uses UTF-8 serialized system/messages/tools bytes plus 32 bytes per message; output reservation uses the frozen model maximum. This is an estimate, not provider tokenization. Positive reported input, output and cache usage replace the estimate; unavailable or synthetic zero usage retains it until expiry. Reporting failure keeps the reservation.

The transient runtime configuration carries the callback URL, attempt-scoped identity, output limit and callback timeout (`executiond -model-quota-timeout`, default 5 seconds). The plugin deletes its environment entry before starting tools; request artifacts omit it. A grant expires within one second, and network latency counts against that interval. Waiting consumes the existing runtime deadline and Worker slot. This cooperative listener does not meter raw tool traffic or arbitrary plugins bypassing DSH's LLM service. See the [runnable quota example](../../rank/examples/model-quota/README.md).

## Verification

Run `node --test apps/execution-runtime/history-input.test.mjs` from the repository root. After `pnpm run build:lib:host`, run `EXECUTION_TEST_DSH=1 go test ./harness -run TestDSHStructuredHistory` from `execution/backend` for a real CLI/provider serialization smoke. The local HTTP fixture verifies multi-turn text, external tool results, additional user input and one-call usage; it does not validate a remote provider's policy or model quality.

The [shared SDK snapshot](../../examples/jsonrpc-agent/tests/history.snapshot.ts) verifies imported events through both SDK transports. Install the Python SDK test environment with `uv sync --project python/sdk --frozen`, then run `PYTHON="$PWD/python/sdk/.venv/bin/python" pnpm exec vitest run --config vitest.snapshot.config.ts examples/jsonrpc-agent/tests/history.snapshot.ts` from the repository root.

## Limitations

Each invocation creates a fresh runtime. History import does not restore process memory, filesystem changes, native CLI session state or provider-specific reasoning state. The adapter and worker own isolation, timeout and cancellation. The [production design](../../.agents/notes/proposed/architecture/2026-09-14-rank-production-execution.md) defines those responsibilities.

Set `EXECUTION_TEST_RUNTIME` to an extracted AgentKiln runtime directory to run the same history smoke against packaged files.
