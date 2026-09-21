# Agent Note: Durable Rank serving dispatch

Status: implemented

English | [中文](2026-09-14-rank-serving-dispatch.zh.md)

## Problem

A stored Response does not prove that Execution accepted it. Rank can stop before submission, lose the HTTP reply after acceptance, or fail to commit the returned Execution ID. Rebuilding an invocation from a mutable endpoint policy can change its idempotency payload. An observation failure cannot establish whether remote work failed or stopped.

## Decision

[Serving admission](../../../../rank/backend/internal/app/serving.go) stores the complete outbound Execution request with the Response and its initial event. The request contains a stable Response-derived idempotency key, Agent configuration, request instructions, conversation input, attachments and timeout. Credentials remain referenced by model-connection ID. Recovery submits those stored request fields without rereading current endpoint policy. The frozen request preserves the model fixed by Agent version creation; Execution validates it and enforces global model quotas.

A single Rank dispatcher observes each active Response; HTTP-only replicas admit and read durable work. Submission, network and persistence errors retain the active business record for reconciliation; remote terminal records determine completion. Trial progress commits text, event and Execution sequence in one transaction, ignoring already recorded sequences. Public output and cost are committed once with terminal business events and the webhook outbox. Rank shutdown waits for serving observers without cancelling their remote invocations.

Cancellation records intent before contacting Execution, including when Rank has not stored an Execution ID. Reconciliation retrieves the original ID through idempotent submission and forwards cancellation. The Response remains active until a remote terminal outcome is known; completion may win a cancellation race. Cancellation is best effort, not a promise to undo external actions or prevent all work between submission and cancellation. No browser connection owns an accepted execution.

SQLite schema 21 and PostgreSQL schema 2 add the frozen request, cancellation flag and replay cursor. Old completed history remains readable. Startup refuses active Responses without a frozen request rather than reconstructing potentially different work. The [backend reference](../../../../rank/backend/README.md) owns observation/retry settings; the [serving API](../../../../rank/docs/serving-api.md) owns cancellation response semantics.

## Alternatives considered

**Mark an invocation failed when its observer disconnects.** Reports a transport outcome as an execution outcome and can hide ongoing side effects. Reconciliation preserves uncertainty until Execution supplies a terminal result.

**Reconstruct request parameters after every restart.** Endpoint configuration or conversation growth can alter the request and defeat idempotency. Stored outbound requests preserve exactly what Rank accepted for dispatch.

**Add another workflow engine for serving.** The business record already supplies a recoverable active-work index. A single-owner observer and Execution's idempotent admission cover this lifecycle without introducing another scheduler.

## Consequences

The [packaged serving snapshot](../../../../apps/rank-web/tests/serving-recovery.e2e.mjs) kills the actual Rank process before submission, after real Execution acceptance while suppressing its HTTP reply, and before committing the completed result. It verifies the original Execution ID, response identity and one terminal event after restart on SQLite and PostgreSQL Rank. Execution uses SQLite in this assembled scenario; separate Execution/River acceptance covers its distributed worker path. Database tests exercise cancellation before ID persistence, endpoint-policy changes, observer disconnects and duplicate trial progress.

This decision covers public serving and Agent trials. [Evaluation dispatch](2026-09-14-rank-evaluation-dispatch.md) owns candidate/Judge recovery and shares the Execution observation protocol. Runtime isolation and safe autoscaling remain in the [production proposal](../../proposed/architecture/2026-09-14-rank-production-execution.md). One dispatcher owner remains required; [HTTP replicas](../bug-fix/2026-09-17-rank-api-rolling-drain.md) do not own observers.
