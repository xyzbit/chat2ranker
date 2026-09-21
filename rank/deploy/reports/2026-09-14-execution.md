# Execution acceptance evidence — 2026-09-14

English | [中文](2026-09-14-execution.zh.md)

This report covers the working tree based on `c078fbd287e6cfe3de6a220ffa293fe6347a4e01`, including uncommitted implementation changes. It is not a production-readiness certificate. The source tree contains earlier product work as well; no commit or push is part of this acceptance run.

## Environment

macOS arm64, Go 1.26.0, Node 24.14.1, PostgreSQL 15.15 in a private test container, River OSS v0.47.0, MinIO Go S3 client v7.3.0, and native SeaweedFS v4.47. Secrets remain in private local files and are omitted from this report. Docker builds target Linux arm64.

| Image | Local content tag | Docker image identity |
| --- | --- | --- |
| execution | `rank-execution-lab:7e423a6b8bd50681` | `sha256:bcac3a0ba33c0b751930bebee7bf876aaabc8de08497dc7b309a9d2f2c7e9037` |
| objects | `rank-object-lab:92ddf30de78b2113` | `sha256:0371de08621980bb63c930570038b0aac417d20129e9b74aef85308402315fcc` |
| postgres | `rank-postgres-lab:b3968e348b48f119` | `sha256:b3968e348b48f1198cc6de6611d055dbad91cd561b7990c406c3fc28d7095b21` |

These images pass container entry-point smoke checks and the following Kubernetes Execution experiment.

## Verified results

| Check | Observed result |
| --- | --- |
| Execution and Rank Go suites | Pass; tests requiring explicit PostgreSQL/S3 variables skip in the ordinary suite. |
| Real PostgreSQL repository tests | Pass: transaction rollback, concurrent idempotent admission, ordered/fenced progress, cancellation, encrypted credentials, shared capacity and terminal-delivery reconciliation. |
| Packaged API with two independent worker processes | Pass: both workers execute requests; terminal results and River acknowledgements agree; queued/running cancellation and lost-owner unknown outcomes converge. |
| SQLite lifecycle and race checks | Pass: bounded online/evaluation pools, queue admission, shutdown cleanup and retained queued work. Unconfirmed forced termination retains the workspace. |
| Runnable lifecycle snapshot | Pass with SQLite; pass with PostgreSQL/River and two workers; pass with PostgreSQL/River plus S3 after removing local artifacts and restarting the API. |
| S3 adapter against SeaweedFS | Pass: write/read/delete, signed download and local traversal protection. |
| Control-host assembled test with service authentication | Pass: persistent control session and Rank tools continue to work with the private Execution token. |
| Documentation and formatting | Lint and diff whitespace checks pass. Documentation checks report 27 passes and one pre-existing root `AGENTS.md` word-budget failure (2398 words, ceiling 1950). Updated bilingual pairs and relative links pass. |
| Kubernetes Execution experiment | Pass: migration/rollout, retained queued work/cancellation, idempotent submission, SSE replay, running cancellation, evidence after service Pod replacement, unknown outcome after worker loss, and eight accepted requests across a rolling update. |

## Kubernetes measurement

The [experiment record](2026-09-14-kubernetes.json) uses single-node minikube 1.39.0, Kubernetes 1.37.0, containerd 2.3.4 and Linux arm64. Docker limits the node container to 4 CPUs and 4 GiB. Kubernetes reports allocatable resources from the Docker VM as 8 CPUs and about 7.65 GiB; that does not establish access to those capacities. Each worker is limited to 1 CPU/512 MiB with one online and one evaluation slot; this load uses only the online queue.

| Worker replicas | Requests | Batch duration | Observed peak concurrency | Queue p95 |
| --- | --- | --- | --- | --- |
| 1 | 24 | 10.812 s | 1 | 9.855 s |
| 2 | 24 | 3.875 s | 2 | 3.414 s |

This is one observation of a deterministic short CLI load, not a stable benchmark or a promise of linear scaling. It excludes real models, physical cross-host load and autoscaling. Both replicas recorded ownership. Force-removing an active worker left one attempt with `execution_outcome_unknown`, without replay. API, worker, PostgreSQL and object-service Pods were replaced; existing events and stdout artifacts remained readable.

Docker Hub timeouts were resolved by importing the intended kindnet version through Google's official image cache. A digest-only PostgreSQL archive lacked a runnable tag; an owned lab tag preserves the pinned source image. System Python 3.9 could not parse Go nanosecond timestamps; acceptance uses the documented Python 3.12 prerequisite with an added startup version check.

The dedicated cluster was stopped and started again. Read-only verification recovered the same completed execution, ordered SSE history and SHA-256-checked stdout; the unknown outcome remained failed at attempt one. Execution containers restarted while dependencies initialized, then recovered. This tests retained data after a local restart, not uninterrupted service availability.

## Remaining acceptance

This report is scoped to the earlier Execution fixture images. The separate [complete platform report](2026-09-14-platform.md) records real DSH daily-news/chat clients, Rank PostgreSQL dispatch, structured history and control-session recovery. The [lab tutorial](../k8s/README.md) provides both experiments.

Whole-runtime isolation, useful scaling metrics, bounded autoscaling and physical cross-host evidence remain part of the [production proposal](../../../.agents/notes/proposed/architecture/2026-09-14-rank-production-execution.md). The current tests exercise deterministic CLI invocations; they do not measure real-model quality, provider budgets or production traffic capacity.
