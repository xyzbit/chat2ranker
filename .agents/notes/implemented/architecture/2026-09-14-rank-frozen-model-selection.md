# Agent Note: Frozen model selection

Status: implemented

English | [中文](2026-09-14-rank-frozen-model-selection.zh.md)

## Problem

A connection default read at worker startup can change an accepted invocation's model. Empty and explicit names for the same default also form different concurrency groups. Conversation creation bypassing connection validation can persist an unusable or ambiguous Agent version.

## Decision

[Agent configuration](../../../../rank/backend/internal/app/service.go) resolves an omitted model from the verified connection while saving a new Agent version. The model name is trimmed and persisted with the connection ID. An explicit custom model remains valid even when absent from the discovered model list. A connection without either an explicit or default model is rejected. Changing connection defaults affects subsequent configuration saves, not existing versions, Runs or Responses.

[Conversation commands](../../../../rank/backend/internal/app/control.go) use the same resolution and protocol checks before taking the business transaction lock. A version edit inherits immutable base configuration and applies supplied fields; an explicitly empty model selects the connection default at that save. Command replays return the original record before reading current connection settings. The transaction repeats the idempotency and session-ownership checks.

The [Execution protocol](../../../../execution/backend/contract/model.go) requires an explicit model whenever a model connection ID is present, and rejects surrounding whitespace in either identifier. Execution admission and worker dispatch enforce that rule. Workers never substitute a connection default. PostgreSQL concurrency limits therefore count one exact connection/model pair; they do not combine unrelated connections, resolve provider aliases or implement token-per-minute limits.

Rank refuses new evaluations and trial/serving Responses against a stored connection-backed Agent without a fixed model; the operator saves a new version with an explicit selection. Startup refuses affected active Rank records rather than silently changing their requests. An unresolved queued Execution fails before runtime startup. Completed records remain readable. Native CLI authentication without a model connection remains a trusted local-development path outside connection/model quotas; it is not a production quota identity.

## Alternatives considered

**Resolve the model when a worker starts.** A queued request changes meaning when connection defaults change and cannot share a stable model quota with explicitly configured requests.

**Store submitted and resolved model specifications in Execution.** Would preserve HTTP idempotency there, but Rank could still resolve a different default after recovering a request that Execution had not accepted. Resolving configuration before immutable version creation avoids a second specification and freezes the value earlier.

## Consequences

The [assembled model-resolution scenario](../../../../apps/rank-web/tests/model-resolution.e2e.mjs) starts packaged Rank and Execution processes and checks form creation, conversation creation/version editing, changed defaults, explicit names, invalid connection/protocol rejection, original-command replay and unresolved Execution rejection. Its checked-in snapshot records the externally visible models. SQLite and PostgreSQL application tests cover frozen versions and rejection of legacy ambiguous Agents before evaluation or trial admission. Existing concurrent PostgreSQL worker tests cover the resulting connection/model reservation group.

This change freezes model selection, not provider credentials or the entire connection record. Credential rotation and operator-managed connection metadata remain Execution concerns. Whole-runtime isolation and production capacity acceptance remain in the [production proposal](../../proposed/architecture/2026-09-14-rank-production-execution.md).
