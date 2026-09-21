# Agent Note: Agent playground

Status: implemented

English | [中文](2026-09-11-agent-playground.zh.md)

## Problem

Selecting an Agent in Rank control chat configures evaluation rather than invoking that Agent. Requiring a dataset or integration code prevents quick inspection of conversational behavior and makes the answering Agent ambiguous.

## Decision

One conversation dock contains explicit Rank and Trial tabs. Shared placement avoids competing assistants, while separate conversation state preserves the answering identity and tool permissions. Agent details separate immutable configuration from independently saved evaluation defaults: Configuration contains runtime settings, always-visible tools and Skills, version creation, and a link to a separate comparison page; Evaluation contains family dataset defaults and selected-version records. Keeping both panels mounted preserves unsaved evaluation choices across tab switches. Evaluation handoff creates a separate experiment and unsent version-specific request rather than modifying the user's existing experiment.

An Agent conversation panel owns trial conversations and their history independently of Rank control. It sits alongside Agent configuration or deployed routes and uses family-scoped history to keep the tested context visible without a separate navigation destination. The first message freezes an Agent version, optionally resolved from a deployed alias. Trials reuse Responses admission, persistence, execution, and recovery with an operator identity; nullable deployment and credential references avoid inventing public serving resources. Rank control produces a durable handoff instead of impersonating or silently invoking the tested Agent. [Playground reference](../../../../rank/docs/agent-playground.md) owns API and interaction semantics.

## Alternatives considered

**A top-level playground page.** Rejected because switching between configuration and a separate trial destination adds navigation without adding another task.

**Switch the control conversation to the selected Agent.** Rejected because its experiment state, history, and tools belong to Rank and would obscure who answers.

**Create a hidden Deployment and API key for every preview.** Rejected because unpublished testing would acquire unrelated routing and credential lifecycle obligations.

**Duplicate the serving runtime for previews.** Rejected because invocation, continuation, cancellation, and recovery would diverge from external integrations.

## Consequences

Dataset-free trials preserve the tested version without publishing it. Failed attempts remain auditable while later turns include only completed context. Trial traffic cannot trigger production Webhooks or be accessed through public serving keys. SQLite migration 20 preserves existing references and validates foreign keys. Keyless assembled service/worker transcripts cover continuation, version pinning, duplicate sends, failure, cancellation, restart, and control handoff; browser acceptance covers desktop/mobile navigation and persistence. Native protocol events are normalized before user display. Caller-supplied business tools and new interactive harness approvals remain outside the playground.
