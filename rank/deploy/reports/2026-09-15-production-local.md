# Local production-path acceptance — 2026-09-15

English | [中文](2026-09-15-production-local.zh.md)

The shared model-account quota increment and remaining local recovery/scaling windows pass on the remote minikube platform. This report covers local implementation acceptance; physical multi-host validation remains with the operator. [Machine-readable evidence](2026-09-15-production-local.json) retains exact images, samples, source hashes and raw report identifiers.

## Environment

The platform and gateway use `09a7dc4989fbd5c3` image tags. Sandbox image digest is `sha256:98ae4daa4d023f80ef7de0ef487209a47743976835ecae34b4284f51d35946a2`. PostgreSQL schema 7 and Worker protocol 3 are deployed together. Topology is a single minikube node, two restored Worker Pods, an external Docker OpenSandbox service, PostgreSQL/River and SeaweedFS S3 storage. Runtime resource admission remains limited to 2 CPU cores and 2 GiB for the shared pool; additional Worker replicas do not provision another host.

## Results

| Area | Observed result |
| --- | --- |
| Complete platform regression | All ten checks pass on the deployed images: management authentication/revocation, public credentials, serving, trial, control conversation, S3 evidence, request budget and restart recovery. |
| Shared quotas | Two independent local Worker processes share RPM and TPM across two connections. API restart retains debits, changed rates do not reset usage, cancellation and idempotent replay preserve one attempt, unsupported harness admission fails. |
| Remote quota and scaling | Real DSH in OpenSandbox requests permits through the restricted callback ingress. Seven quota-blocked tasks remain outside runnable demand over 25 seconds of KEDA samples; Workers do not grow. Service tokens and management routes are rejected at the callback ingress. |
| DeepSeek demos | Actual recent HN titles produce a cited Chinese digest. Two-turn SSE chat recalls the user's name, interest and ten-minute allowance. All three responses complete, retain positive usage/S3 artifacts and finish Sandbox cleanup. |
| Provider budget | Four actual requests, including one connection-verification request. Maximum configured output is 512 tokens; actual demo outputs are 219, 47 and 13 tokens. The forwarding ledger persists the total before dispatch; no retry reset occurred. |
| Combined failure | PostgreSQL outage plus actual Worker SIGKILL converges to an unknown outcome, attempt 1, one external effect and confirmed runtime cleanup. Idempotent replay does not execute again. |
| Object/reference window | S3 acknowledges a successful PUT; the acknowledgement is held before Worker reference publication, then the Worker is killed. Known output and usage survive, the object has no database reference and the artifact API returns 404 for it. The object is identified for retention-based operator collection. |
| Fresh database | Packaged Rank and Execution migrations run twice against a newly created database. Both schemas have their intended owners, business tables remain empty, River and quota tables exist. Only the disposable test database is removed. |
| Runtime permissions | TCP-authenticated application roles retain required DML and cannot perform DDL, access the other service's protected tables or alter migration metadata. |
| Automatic active drain | KEDA/HPA selects a live task owner. Within-grace completion succeeds; grace expiry records an unknown outcome. Both preserve one attempt/effect, exit cleanly, release the Sandbox and leave new work on a surviving Worker. |

The active-drain experiment deliberately uses a zero-second downscale stabilization overlay and Pod deletion cost to select a live owner. The standard scaler retains its 45-second stabilization; this experiment proves shutdown behavior under an automatic selection, not a production scale-down timing SLO. Scalers and fault proxies are removed afterwards and two Workers restored.

## Measurements and limits

The news request took 14.622 seconds end to end, including source collection and remote runtime startup. Both chat turns together took 27.443 seconds. These are three serialized demo responses, not a load-test p95 or throughput guarantee. The request ledger records provider durations below 2.3 seconds for these four calls; cold runtime and platform overhead merit measurement before changing capacity policy. See the earlier [capacity report](2026-09-15-autoscaling.md) for deterministic load observations.

Quota TPM uses conservative input estimates followed by positive reported usage; it is not exact provider billing or a monetary budget. Waiting inside DSH occupies its existing slot. Local raw tool traffic and hostile plugin bypass are outside this cooperative control. Automatic orphan collection, production TLS deployment, multi-host performance and platform high availability are not claimed.

The real-provider proxy has been removed; its temporary endpoint/connection is retained as acceptance history and is not a reusable live connection. The platform's normal system model remains the deterministic lab connection. Credentials never enter this report. The [production reference](../k8s/production.md) documents private networking, TLS, identities, rotation, storage retention and remaining operator checks.

## Repeat

The [lab tutorial](../k8s/README.md#shared-quotas-and-remaining-production-checks) lists each acceptance command. Unit and real-PostgreSQL tests cover quota concurrency, usage reconciliation, retries, stale admission, permit identity retention and cleanup. `pnpm run rank:test:model-quota` checks the actual packaged API/two-Worker/DSH transcript without a provider key. Real-provider verification remains opt-in and consumes the existing persisted 10-request allowance.

`pnpm run lint` and `git diff --check` pass. The full documentation run initially passed 26 checks with two failures; the translation-link mismatch was fixed and all 1011 pairs pass the targeted rerun. The remaining failure is the pre-existing root `AGENTS.md` word budget (2398 > 1950), untouched by this increment.
