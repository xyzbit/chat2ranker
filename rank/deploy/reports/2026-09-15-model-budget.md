# Model request budget verification — 2026-09-15

English | [中文](2026-09-15-model-budget.zh.md)

The [recorded evidence](2026-09-15-model-budget.json) identifies the exact images, source hashes and nine passing minikube platform checks. A DSH execution limited to one model request receives HTTP 429, stops with `execution_model_budget_exhausted`, and leaves exactly one request in the deterministic provider's log. Repeating the submission retains attempt 1 and the frozen limit; runtime cleanup completes.

The remote command stream can emit a transport `ping` after its terminal frame. The Executor ignores that keepalive while continuing to reject trailing output, contradictory terminal events, malformed data and truncated HTTP responses. Real DSH integration confirms that the failed result retains artifacts after cleanup. The [budget decision](../../../.agents/notes/implemented/architecture/2026-09-15-execution-model-budget.md) defines counting, capability negotiation and known-usage retention.

The complete platform passes daily-news collection, structured multi-turn chat, Agent trials, control-host history, S3 evidence, event replay, Pod replacement and queued-work recovery. Packaged remote snapshots pass with SQLite and PostgreSQL/River with two Workers. The same Sandbox image passes writable-storage limits, network egress and protected-lifetime experiments, including stopping the lifecycle service while detached work runs.

Executor tests and contract lint pass. Documentation checks pass 27 of 28 gates; the existing root `AGENTS.md` word budget remains over its limit at 2,398 words versus 1,950. The experiment uses actual DSH processes and local deterministic model services; it does not establish real-provider behavior, shared RPM/TPM enforcement, production identity, cross-host capacity or high availability. The [production proposal](../../../.agents/notes/proposed/architecture/2026-09-14-rank-production-execution.md#remaining-implementation-order) defines the remaining implementation order.
