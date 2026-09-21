# Sandbox resource admission experiment — 2026-09-15

English | [中文](2026-09-15-runtime-pool.zh.md)

The [machine-readable report](2026-09-15-runtime-pool.json) records images, budgets and observed outcomes. Two minikube Worker Pods control separate Docker Sandbox containers on one laptop. The provider is deterministic, while the DSH processes, lifecycle service, database and container limits are real.

| Phase | Assigned pool budget | Observed result |
| --- | --- | --- |
| CPU constrained | 500 millicores, 2 GiB | Three distinct tenant/model connections finish at attempt one; at most one live Sandbox is observed. |
| Memory constrained | 2 CPU, 512 MiB | The same behavior holds when memory is the limiting resource. |

Each container is configured with 500 millicores and 512 MiB, verified through Docker inspection. Queued work retains attempt zero and no runtime. Resource-blocked work contributes no additional runnable demand; when the pool is idle, one waiting request remains eligible. The verifier restores the original 2-CPU/2-GiB budget and removes its temporary model service.

SQLite and real PostgreSQL tests cover concurrent admission, unknown-outcome reservations, target-change rejection, duplicate service budgets, oversized requests and live budget reductions. Executor tests verify that queued specifications survive default changes, and that a different image cannot claim them. Both database modes pass the packaged remote snapshot with frozen resource fields. Race checks cover resource admission and demand projection.

An assigned budget is not a measurement of physical free resources. Other applications need reserved headroom, and independent Execution databases or direct lifecycle clients must not spend the same budget. These experiments do not establish physical-host provisioning, disk/egress limits or production availability. Commands and prerequisites are in [the tutorial](../k8s/README.md#sandbox-resource-budget-experiment); ownership and update rules are in [the decision](../../../.agents/notes/implemented/architecture/2026-09-15-execution-runtime-pool.md).
