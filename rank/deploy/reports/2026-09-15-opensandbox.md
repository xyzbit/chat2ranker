# OpenSandbox execution experiment — 2026-09-15

English | [中文](2026-09-15-opensandbox.zh.md)

The [machine-readable report](2026-09-15-opensandbox.json) identifies the tested images and outcomes. OpenSandbox server 0.2.3 and execd 1.1.0 run in Docker Desktop's Linux environment. Host Execution processes use Go 1.26.0 and the pinned Go SDK. The complete minikube platform also runs the remote adapter: API/Worker Pods access a Docker lifecycle service through a Kubernetes Service, and separate Harness containers use the dedicated Docker network. PostgreSQL and S3 remain in minikube.

| Check | Observed result |
| --- | --- |
| Packaged API, SQLite | Remote invocation, persisted runtime handle, durable events, idempotent replay, artifact reads after deletion and queued cancellation match the example snapshot. |
| Packaged API, PostgreSQL/River | The same snapshot passes with separate API and two Worker processes. |
| Full minikube platform | Gateway endpoint calls, daily-news, multi-turn chat, Agent trials, S3 artifacts, control history and Rank restart pass. Scaling Worker Pods to zero and back completes the same queued response at attempt one. |
| Real DSH process | Structured history reaches a deterministic model; the answer recalls Ada. Progress and artifacts remain readable after runtime deletion. |
| Persistence and fault injection | SQLite/real PostgreSQL reject stale ownership; lost creation responses are not replayed, late discovery recovers cleanup, failed handle storage prevents commands and foreign proxies receive no credential-bearing request. |
| Independent runtime expiry | A detached TERM-ignoring workload is observed. The lifecycle server is killed; protected PID 1 stops its container at 25 seconds, before the 90-second provider TTL. |
| Container identity | UID 1000 cannot read root PID 1's environment or signal it; commands do not see the execd token or Docker socket. |

Standalone integration tests use a local artifact store; the complete platform verifies S3 reads after sandbox deletion and Pod replacement. These results do not establish physical-host throughput, production authentication, disk/egress limits or isolation of arbitrary hostile code. The Docker sandbox service runs beside minikube on the same laptop; it is not a Kubernetes-native sandbox controller. Setup and repeatable commands are in [the lab tutorial](../k8s/README.md); ownership semantics are in [the decision](../../../.agents/notes/implemented/architecture/2026-09-15-execution-opensandbox.md).
