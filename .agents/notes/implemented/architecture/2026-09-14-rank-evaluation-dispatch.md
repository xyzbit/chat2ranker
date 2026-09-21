# Agent Note: Durable evaluation invocations

Status: implemented

English | [中文](2026-09-14-rank-evaluation-dispatch.zh.md)

## Problem

A recovered Trial can repeat a candidate or Judge whose result was accepted remotely but not committed locally. Attempt-derived submission keys turn transport recovery into new model/tool work. Reading the current Judge binding after restart also changes an accepted evaluation's configuration. Marking a Run cancelled before reconciling its invocations does not establish that its runtime work stopped.

## Decision

[Evaluation dispatch](../../../../rank/backend/internal/app/evaluation_dispatch.go) stores each candidate request in the Run/Trial admission transaction. Judge requests are created only after the cached candidate result and deterministic checks allow them. A logical key contains the Run, Trial and phase/criterion identity; transport attempts and process restarts do not change it. An explicit user rerun creates new Run/Trial identities.

Run admission resolves runtime availability and the system Judge binding before taking the business transaction lock. The transaction verifies the selected versions and stores the Judge connection/model, tenant routing and execution timeout with the Run. Serving, normal evaluations, comparisons and release regression share persistent repositories without holding a database transaction during an external model or Execution call. Model credentials stay in Execution; Rank stores references and request data only.

The invocation stores its original request, possible-submission marker, Execution ID, event cursor and cached authoritative outcome. Delivery and observation errors retry the same request. Event projection and cursor advancement commit together. Derived phase events have stable keys; Trial completion and cost aggregation retain their existing transactional result identity. A recovered successful candidate is reused when continuing Judge work. Rank does not automatically rerun a failed candidate or Judge; known failed-call usage and Execution references remain in the Trial result. Unknown outcomes remain infrastructure/grading failures and never enter the quality denominator.

Cancellation persists `cancelRequested` and stops scheduling. Never-submitted intents are cancelled without contacting a worker. A possibly submitted intent is recovered by its original key, then cancelled and observed until its remote terminal outcome is known. The Run becomes cancelled after all its invocation outcomes are resolved. Process shutdown instead stops and awaits local observers without cancelling their accepted remote work. One Rank coordinator owner remains required.

The [Execution event reader](../../../../execution/backend/internal/app/service.go) drains events again after observing terminal status, covering completion committed between an empty event read and the status read. The [Go client](../../../../execution/backend/client/client.go) reports named stream failures and never turns an observer error into implicit cancellation. This retains terminal progress as well as results.

SQLite schema 22 and PostgreSQL schema 3 add invocation records, Run execution settings, cancellation intent and unique derived-event keys. Active remote Runs without the supported dispatch format are refused at startup; completed historical Runs remain readable. The [backend reference](../../../../rank/backend/README.md) owns startup and shared reconciliation settings.

## Alternatives considered

**Generate another execution key after failure.** An HTTP error cannot establish whether the first request started. The original key recovers the accepted invocation instead of risking repeated external actions.

**Cancel every prepared candidate through remote submission.** Launches work solely to cancel it. Persisting the possible-submission marker distinguishes untouched intent from an uncertain remote acceptance.

**Rebuild Judge configuration from current settings.** Changes active evaluation meaning and can make completed evidence incomparable. Admission snapshots keep configuration stable across recovery.

## Consequences

The [assembled recovery snapshot](../../../../apps/rank-web/tests/evaluation-recovery.e2e.mjs) cuts actual candidate and Judge submission replies, kills Rank, and verifies one original execution per phase, one verdict/completion record, and retained result/cost. SQLite and PostgreSQL tests cover lost replies, failed ID/result commits, duplicate progress, unknown outcomes, configuration changes and cancellation recovered by a new coordinator. The 60-Trial packaged evaluation checks every candidate/Judge completion event through the real SSE path.

The cached outcome projects Execution's authoritative record; it is not a second runtime scheduler. Connection-backed candidate models are fixed by [Agent version creation](2026-09-14-rank-frozen-model-selection.md). Whole-runtime isolation, scaling metrics and physical cross-host acceptance remain in the [production proposal](../../proposed/architecture/2026-09-14-rank-production-execution.md).
