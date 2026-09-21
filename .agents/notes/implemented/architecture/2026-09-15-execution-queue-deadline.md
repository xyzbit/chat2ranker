# Agent Note: Frozen Execution queue deadlines

Status: implemented

English | [中文](2026-09-15-execution-queue-deadline.zh.md)

## Problem

Execution timeouts bound an invocation after it starts. Work waiting for absent workers, unsupported capabilities or a saturated model budget can otherwise remain accepted indefinitely and inflate scaling demand.

## Decision

Admission freezes `Execution.queueExpiresAt` from the service's `-queue-timeout` limit, default five minutes. `Spec.queueTimeoutMs` can shorten that wait; zero uses the service limit, and negative or overflowing values reject before admission. Millisecond-resolution deadlines remain unchanged across same-key resubmission, API replacement and later configuration changes. Running invocations retain their separate runtime timeout; the queue deadline limits entry into the running state, not completion time.

API reconciliation expires queued records independently of worker slots, including disabled queues. PostgreSQL locks overdue records, appends the failed event and cancels River delivery in one transaction. SQLite updates queued rows and appends their events in one transaction. Workers independently check expiry while claiming, before reserving quota or allocating a runtime. A stale pre-lock timestamp cannot admit expired work. Competing reconciliation skips locked PostgreSQL rows; a delivery does not acknowledge a still-queued record after such a skip. Periodic reconciliation determines when failure becomes visible, while the claim check prevents late start; database/API downtime can delay visibility until recovery. Absolute deadlines require synchronized service/database clocks.

An expired execution remains failed with `execution_queue_expired`, attempt zero and no runtime allocation. Replaying its idempotency key retrieves that outcome; an explicit new logical request uses a new key. This is an execution failure, not an Agent quality score. Missing frozen queue limits fail as `execution_limits_missing`; upgrades do not invent new waiting time for old queued records. PostgreSQL schema 6 requires explicit maintenance migration; SQLite records schema 10. A local restart retains the existing queued fact without emitting another queue event.

Capacity reports count overdue or missing-deadline queued work as `expired`, excluding it from ready demand and virtual model/tenant reservations. Existing active runtime reservations remain unchanged. See [service configuration](../../../../execution/backend/README.md) for the API and CLI fields.

## Alternatives considered

**Let workers expire work only when delivered.** Disabled or absent workers would leave the caller waiting indefinitely, so API reconciliation is independent of consumer capacity.

**Restart the waiting timeout on delivery or API restart.** Persistent overload could keep requests alive indefinitely. Admission owns one immutable absolute deadline.

**Reuse execution timeout or cancel running work at queue expiry.** Queue waiting and runtime execution have different starting points. Combining them would unexpectedly interrupt a request that started within its admission window.

## Consequences

SQLite and real PostgreSQL tests verify concurrent expiry, late claims, unchanged running/cancelled outcomes, missing limits and atomic rollback when River cancellation fails. Application tests verify deadline capping, idempotent replay and capacity exclusion. The [packaged lifecycle snapshot](../../../../rank/examples/execution-lifecycle/README.md) restarts the API with a different default and retains the same attempt-zero failure in SQLite and PostgreSQL/River. The [cluster verifier](../../../../rank/deploy/k8s/verify-queue-deadline.py) exercises API replacement with zero workers, durable SSE and fresh remote work after worker restoration.

The [local cluster report](../../../../rank/deploy/reports/2026-09-15-queue-deadline.json) records zero-worker expiry, API replacement, a fresh remote invocation and the complete gateway/chat/restart regression. This bounds Execution queue wait after admission. It does not define a total Rank request deadline, provider RPM/TPM backoff, sandbox-host resource admission or runtime recovery after every ownership-loss transition; those remain separate parts of the [production design](../../proposed/architecture/2026-09-14-rank-production-execution.md).
