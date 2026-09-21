# Agent Note: Durable Execution delivery with River

Status: implemented

English | [中文](2026-09-14-execution-river-delivery.zh.md)

## Problem

Execution API replicas and independent workers need durable task delivery without repeating uncertain model or tool calls. SQLite process-local notifications cannot carry work across hosts. A cancelled queue job is not evidence that its runtime stopped, and files left on a worker disappear when that worker is replaced.

## Decision

Idempotent replay compares JSON values rather than serialized bytes. PostgreSQL jsonb may reorder raw schema or tool-argument objects and rewrite decimal notation. Object whitespace/key order and equivalent decimal representations do not change request identity; array order, string/number types and distinct integer values do. Decimal comparison adjusts coefficient/exponent text without expanding exponent-sized numbers or rounding through floating point. Both the initial lookup and concurrent-insert conflict use the same comparison.

Execution uses River OSS v0.47.0 and PostgreSQL for distributed delivery, with Go 1.26.0. The application owns immutable execution input, ordered progress, attempts and terminal outcomes. River arguments contain only the execution ID. Record creation, its initial event and enqueue share a transaction; result publication and River acknowledgement also share a transaction. This implements part of the [production proposal](../../proposed/architecture/2026-09-14-rank-production-execution.md), which retains the remaining platform and isolation work.

Runtime and cleanup limits are recorded separately from request input so changed defaults do not break idempotent retries or shorten an existing owner's recovery window. A current attempt may publish progress only while running. Running cancellation retains capacity until execution returns; forced shutdown that cannot confirm runtime stop fails explicitly and retains its workspace. Delivery retries never restart a recorded running invocation. Lost owners yield an unknown outcome after the recorded lifetime, and a small reconciler closes terminal River deliveries that lack an application outcome.

River controls per-process online/evaluation concurrency. PostgreSQL serializes the application's narrow runtime-admission transaction and counts running/cancelling executions and terminal attempts with [pending runtime cleanup](2026-09-14-execution-runtime-cleanup.md) as model/tenant reservations. It does not implement another queue or lease mechanism. SQLite keeps a bounded single-process pool with matching lifecycle semantics. Graceful shutdown stops claims before draining; waiting local records survive for the next owner.

SIGTERM stops River claims and waits for the configured shutdown grace. If draining times out, StopAndCancel interrupts active deliveries under a separate cleanup-plus-persistence budget. A clean Worker exit is distinct from a successful invocation: an interrupted remote tool call can retain an unknown business outcome even after its Sandbox is confirmed absent. Kubernetes Pod grace must exceed this sequence with operational headroom.

Application schema changes commit before River's versioned migrations, under a dedicated PostgreSQL session lock. PostgreSQL enum additions must commit before subsequent migrations use the new values. The schema readiness marker is written only after both sets succeed. A failed migration requires another explicit migration attempt rather than starting a partially migrated service.

Execution encrypts PostgreSQL model credentials with a deployment-owned AES-GCM key. The service token authenticates private API calls; credential resolution also requires the control token. S3-compatible artifact storage uploads digest-addressed evidence before publishing references, and API reads check those references. Database rollback after upload can leave unreferenced objects; automatic garbage collection is outside this implementation.

## Alternatives considered

**A custom database task queue.** It retains delivery recovery, maintenance and shutdown code that River already owns. Application-specific attempt and side-effect rules remain necessary with either choice.

**Temporal or Kafka for each invocation.** Neither removes runtime isolation or external side-effect uncertainty. The accepted scope needs bounded task delivery with PostgreSQL transactions; durable workflow orchestration and an independently consumed event bus do not justify extra services here.

**Treat queue cancellation or retry as runtime authority.** Queue state cannot prove detached processes or remote tool calls stopped. Confirmed cancellation and conservative unknown outcomes preserve that distinction.

**Keep artifacts only on worker disks.** It ties API reads and retained evidence to a particular worker. The S3 adapter separates file lifetime from worker lifetime while keeping the local filesystem option.

## Consequences

The [runnable keyless example](../../../../rank/examples/execution-lifecycle/README.md) checks matching SQLite and PostgreSQL/River transcripts through packaged API/worker processes, including SSE replay and cancellation. Real PostgreSQL tests cover rollback, idempotency, concurrent reservation and reconciliation. Real S3 checks cover stored bytes, deletion and signed access; the assembled S3 example removes local artifacts and restarts the API before reading retained evidence.

The [Kubernetes experiment](../../../../rank/deploy/k8s/README.md) runs the same trusted fixture in independently replaceable API/worker Pods with persistent PostgreSQL and S3 dependencies. Its [acceptance report](../../../../rank/deploy/reports/2026-09-14-execution.md) records retained evidence, cancellation, lost-owner outcomes, rolling updates and observed one/two-worker batch timings. These measurements do not establish real-model capacity.

The [active-drain verifier](../../../../rank/deploy/k8s/README.md#active-worker-drain-experiment) pins live two-to-one scale-down and rolling replacement with real DSH tools. It correlates durable ownership with watched Pod UIDs and terminal container exit codes, keeps a following online request away from draining owners, and verifies artifact bytes, one attempt/effect and confirmed runtime deletion. The [report](../../../../rank/deploy/reports/2026-09-15-active-drain.md) distinguishes successful draining from explicit unknown-outcome interruption at the grace deadline.

[Rank PostgreSQL storage](2026-09-14-rank-postgres-storage.md) is a separate implementation. These Execution checks do not establish durable Rank dispatch recovery, structured multi-turn history, whole-runtime isolation, heterogeneous worker capability routing, autoscaling, or cross-host high availability. The local executor remains a trusted development runtime. The [production proposal](../../proposed/architecture/2026-09-14-rank-production-execution.md) stays proposed until its remaining acceptance requirements are satisfied.
