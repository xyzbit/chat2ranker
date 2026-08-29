# Agent Note: Model connections and native CLI adapters

Status: implemented

English | [中文](2026-08-25-model-connections-and-native-cli-adapters.zh.md)

## Problem

Agent versions named a Runner and model but could not reference a user-managed OpenAI-compatible endpoint. Codex, Claude Code, and Hermes appeared in the UI without first-party event, usage, or installation handling. Passing provider keys through Rank or durable execution records would also expose credentials outside the component that launches workers.

## Decision

Execution Service owns the model catalog, connection metadata, verification, credentials, and cost resolution. The SQLite Repository stores endpoint metadata, local per-model price overrides, and verification state, while a replaceable credential store writes the API key separately with private file permissions. Rank stores only `modelConnectionId` on immutable Agent versions and Run snapshots. LocalExecutor resolves the key immediately before starting a worker; execution-worker receives it transiently and writes a redacted request artifact.

Connection verification calls the provider's model catalog and one minimal request using Chat Completions, Responses, or native Anthropic Messages. A failed catalog refresh keeps the last discovered models; an endpoint without `/models` can still verify an explicit model through the minimal request. Agent creation accepts only verified connections and enforces Runner compatibility: Codex requires Responses, Hermes requires Chat Completions, DSH accepts all three, and Claude Code keeps its own authentication.

Codex, Claude Code, and Hermes have native CLI adapters. Codex parses JSONL lifecycle and usage events, Claude Code parses stream-json result, cost, and usage fields, and Hermes reads its usage report. Probes execute each installed CLI's version command, so a broken installation is distinct from a missing or unconfigured one. Native Codex and Claude Code executions reuse authentication but run with an isolated task workspace and without persistent CLI sessions. Default Codex runs use `--ignore-user-config`, Claude Code uses `--safe-mode`, and the frozen Agent model is passed on each invocation. A custom Codex connection instead receives a private generated `CODEX_HOME` and transient provider credential. Deployment argv overrides remain available for custom installations.

Cost resolution trusts an explicit provider-reported actual cost first, then calculates from a matching connection override, then from the built-in provider catalog. Every calculated result carries the effective price snapshot and is marked as estimated. A missing usage record or price leaves cost unknown. CLI-reported estimates are not treated as actual provider cost.

The experiment workspace contains a compact model-connection center with official DeepSeek, MiniMax, Zhipu GLM, OpenAI, Claude, and Kimi templates. MiniMax, Zhipu, and Kimi use their official mainland-China OpenAI-compatible endpoints by default; international endpoints remain configurable as custom connections. Provider selection narrows a searchable model list and inherits its endpoint, protocol, default USD price, source, refresh date, and caveats such as long-context tiers. Official CNY rates are converted to USD estimates at a fixed `CNY 7.00 = USD 1` and disclosed in each affected model's pricing note. This keeps experiment plots in one currency without representing normalized estimates as provider-reported bills. Catalog and connection-discovered models are merged with source labels; manual model ID is progressively disclosed, and saved credentials expose a resync action. Advanced local-price override, credential entry, and verification remain available without burdening the default path. The Agent editor presents Runner availability, compatible verified connections, and discovered model IDs, and can jump to the connection center without losing its draft. Rank conversation can create or update non-secret connection metadata and prices; API keys remain exclusive to the secure form.

## Consequences

Provider credentials stay out of Rank state, execution history, SSE, and artifacts. A connection can be reused by versioned Agents without duplicating its key, and a Run still freezes the connection ID and model selection. Local file credentials are suitable for single-machine use; a server deployment can replace the credential store with Vault or KMS without changing Rank or Adapter APIs. Known provider costs remain exact, catalog- and connection-derived values are visibly approximate, and missing price configuration remains explicit rather than presenting a misleading amount.

## Alternatives considered

- **Trust every CLI cost field.** Rejected because an estimated CLI cost can use the vendor's default price even when the CLI is routed to another provider or model.
- **Use one immutable global price table.** Rejected because gateways and negotiated prices differ. The built-in catalog is only a local default; a connection may override it.
- **Put every connection setting inside the Agent form or chat.** Rejected because it overloads Agent creation, while credentials must never enter the conversation transcript.
