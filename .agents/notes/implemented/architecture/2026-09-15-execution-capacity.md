# Agent Note: Execution capacity and bounded scaling

Status: implemented

English | [中文](2026-09-15-execution-capacity.zh.md)

## Problem

A raw queue length includes work that additional workers cannot start. Model/tenant limits, unsupported runtimes and delivery backoff require different responses. Terminal failures can still retain a runtime and its quota reservation.

## Decision

The authenticated `GET /v1/capacity` endpoint projects queued and resource-retaining executions from one consistent database transaction snapshot. It returns only aggregate online/evaluation counters and configured slots, excluding prompts, results, credentials and tenant/model identifiers. A separate optional `EXECUTION_CAPACITY_TOKEN` authorizes only this read; the service token remains valid. Storage errors remain errors, not zero demand.

PostgreSQL uses its admission limits. Active and terminal cleanup reservations consume the same global model/tenant budgets, including reservations belonging to another backend. Eligible queued entries receive virtual remaining quota in creation-time/ID order across both queues; this estimates demand without granting permission or changing River's claim order. Disabled queues, unsupported backends/executables/history, and missing default credentials without a request connection are capability-blocked. CLI installation is checked independently of default credentials. Future error retries, missing jobs and abandoned deliveries are delivery-blocked; scheduled quota snoozes count toward next-poll demand. SQLite projects its local queue without claiming distributed quotas.

[Sandbox resource admission](2026-09-15-execution-runtime-pool.md) adds durable CPU/memory budgets and frozen plans in both repositories. The projection applies these budgets alongside model/tenant limits, retains unknown allocations, and excludes `resourceBlocked` work from ready demand. This estimates an operator-assigned budget, not physical-host free resources.

For each enabled queue, demand is active plus ready. Active means durable running/cancelling attempts, not measured worker liveness. Terminal cleanup reservations retain quota but do not request new workers. The oldest-ready wait is an age sample, not historical p95 or time since quota became available. A stale active owner can conservatively retain demand until reconciliation. API and workers must share one homogeneous executor/capability configuration, admission limits and slots; this endpoint does not discover worker pools or provider RPM/TPM limits.

The optional [minikube lab](../../../../rank/deploy/k8s/README.md) uses KEDA 2.20.2's authenticated Metrics API scaler. Each queue targets its configured slots per worker; Kubernetes chooses the larger replica requirement. The lab retains one worker, caps at three, grows one per 15 seconds and shrinks one per 30 seconds after a 45-second stabilization window. It neither sums duplicate database views across API replicas nor installs a second HPA for the same deployment. Metrics failures are not synthesized as zero. Graceful termination remains the Execution worker's responsibility.

The Docker lab disables sandbox host-port publication through a version-checked OpenSandbox 0.2.3 build patch. The upstream port probe runs in the server container namespace and can select ports already bound on the Docker host. Private container endpoints and the authenticated proxy satisfy Rank access without that allocator. No change permits replay of an ambiguous creation. Docker create/destroy events with exact execution/attempt/scope labels can supply a lost handle for an audited operator repair; the ordinary reconciler then confirms absence and releases quota while retaining the failed outcome.

## Alternatives considered

**Scale on all queued work or CPU alone.** A saturated model budget can create a queue while additional compute remains useless. CPU does not represent time awaiting model responses.

**Add a worker registry and Prometheus stack first.** The current pool has one configuration and durable global state. KEDA can consume that aggregate directly; live slot occupancy and heterogeneous routing need separate evidence before adding ownership machinery.

## Consequences

Database-backed tests cover delivery eligibility and cleanup reservations; application tests cover shared quotas, capability exclusion, restricted authentication and storage failures. The packaged keyless lifecycle snapshot exercises the same endpoint with SQLite and separate PostgreSQL/River processes. [The lab verifier](../../../../rank/deploy/k8s/verify-autoscaling.py) creates bounded real DSH traffic against a delayed deterministic model, verifies growth/drain and a one-slot model budget, then restores limits and two manually managed workers.

The [measured run](../../../../rank/deploy/reports/2026-09-15-autoscaling.md) completes all 16 load requests at attempt one, observes one-to-three-to-one workers, and holds one worker when the model concurrency budget is one. Remote Docker containers remain outside Kubernetes scheduling; scaling their Worker controllers does not provision sandbox hosts. This signal does not measure provider throttling, physical-host capacity or production availability. The [production proposal](../../proposed/architecture/2026-09-14-rank-production-execution.md) retains those requirements. [Frozen queue deadlines](2026-09-15-execution-queue-deadline.md) exclude expired waiting work from demand. KEDA installation needs cluster-level resources even though its worker watch is restricted to `rank-dev`.
