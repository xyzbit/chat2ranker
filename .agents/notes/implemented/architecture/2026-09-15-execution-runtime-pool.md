# Agent Note: Durable Sandbox resource admission

Status: implemented

English | [中文](2026-09-15-execution-runtime-pool.zh.md)

## Problem

Adding Worker consumers does not increase the CPU or memory of a separate Sandbox host. Model and tenant quotas can admit many unrelated callers onto the same host, and a lost Worker can leave an allocation running after its business result fails.

## Decision

Execution stores an operator-assigned resource pool for one OpenSandbox service and ownership scope. The pool contains an exclusive CPU/memory budget; every allocator consuming that budget must share the same Execution database. An endpoint has one pool in that database. This is a reservation against an assigned budget, not host discovery or a claim over resources used by other applications.

Admission freezes `runtimePlan`: pool, service URL, scope, image and integer millicore/byte limits. The public request cannot supply these fields. Idempotent resubmission returns the original plan. A Worker with a different target or image waits without changing the request; a changed per-container default does not change queued limits. Missing plans fail before allocation. Oversized requests are rejected at admission. A later budget reduction keeps existing allocations and queues work that no longer fits.

Before marking an attempt running, the repository checks remaining CPU and memory in the same transaction as the model/tenant checks and running event. PostgreSQL serializes admission across Workers; SQLite serializes its local database writes. Running, cancelling and terminal cleanup-pending attempts retain their frozen resource reservation. Unknown legacy allocations block new reservations conservatively. Only confirmed cleanup releases the obligation, and terminal result publication remains separate. No lease expiry or Worker count frees an unknown allocation.

Pool configuration is durable. Startup creates an absent pool or verifies an exact match. `-configure-sandbox-pool` explicitly updates its budget and exits; running Workers read that authoritative budget on each admission. Service/scope changes require all queued, active and unconfirmed work to drain. API/Worker startup flags must be updated together after an operator changes the stored budget. PostgreSQL schema 6 uses explicit maintenance migration; SQLite records schema 10.

Capacity reads plans, reservations and pool budgets from one consistent transaction snapshot. It virtually reserves remaining resources across online/evaluation queues and reports `resourceBlocked` separately from model/tenant `quotaBlocked`. Resource-blocked work adds no runnable Worker demand. Only attempts matching the configured target/image contribute active Worker demand; other images still retain their shared resource reservation. The pool ID and service location are excluded from aggregate metrics.

## Alternatives considered

**Count Worker replicas as host capacity.** Workers control remote containers; their replica count does not bound the resources those containers consume.

**Use a per-process semaphore.** Each Worker would admit its own maximum, and process loss would discard reservations while containers still run.

**Create a host scheduler and resource leases.** One explicitly assigned service budget needs no node registry, placement algorithm or second lifecycle manager. Durable Execution attempts already own allocation and cleanup.

## Consequences

CPU and memory can independently block admission across unrelated models and tenants. Real SQLite/PostgreSQL tests cover concurrent starts, retained unknown results, explicit budget updates, target-change rejection and frozen plan reads. Executor tests inspect actual create-request limits after configuration changes. The [lab verifier](../../../../rank/deploy/k8s/verify-runtime-pool.py) compares two-Worker, real DSH container behavior against an assembled snapshot and restores the original budget.

The operator must reserve headroom for the kernel, daemon and other workloads, and prevent independent databases or direct lifecycle clients from spending the same budget. Pool configuration does not enforce disk, egress or host-level isolation, provision machines, or discover physical free resources. Multiple physical hosts and provider-side admission remain separate production acceptance work in the [proposal](../../proposed/architecture/2026-09-14-rank-production-execution.md).
