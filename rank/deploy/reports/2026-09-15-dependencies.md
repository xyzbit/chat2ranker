# Dependency interruption experiment — 2026-09-15

English | [中文](2026-09-15-dependencies.zh.md)

The remote minikube platform passes both dependency scenarios and the subsequent public-client regression. [Machine-readable evidence](2026-09-15-dependencies.json) records image identities, source hashes, observed outcomes and upload byte counters. Private reports remain outside Git.

| Scenario | Observed result |
| --- | --- |
| PostgreSQL removed during a real DSH tool call | The database Pod disappeared; an authenticated execution read hit its ten-second client timeout. After restoration, the original execution failed with an explicit unknown outcome at attempt one. The external effect occurred once and runtime deletion was confirmed. |
| S3 body interrupted after one successful artifact | The proxy interrupted ten retried upload requests, receiving 13,528 bytes and forwarding only 160. The model/tool invocation was not replayed. The execution failed at attempt one, retaining known output/usage and one confirmed compressed-session artifact. Its SHA-256 matched after base64 decoding through the artifact API. |
| Recovery | Prior artifact bytes stayed unchanged; a fresh invocation completed at attempt one. The original request keys returned the same failed executions. Model/proxy fixtures were removed, PostgreSQL and the object Service selector restored, and two Workers remained healthy. |
| Full platform regression | Daily-news request, two-turn SDK chat, direct trial, persistent control conversation, S3 reads, SSE replay, Rank/control replacement and queued-request recovery passed with the final images. |

The interrupted artifact case exposed the loss of observed result evidence and UTF-8 corruption of binary previews. Executors persist known results and confirmed references progressively; previews now declare their encoding. Earlier verifier runs also exposed a local artifact-store fixture without its root directory, an outdated capacity snapshot, and a proxy cleanup timeout equal to its termination grace. The final run uses the corrected fixture, current snapshot and bounded cleanup.

This is a deterministic single-laptop experiment, not storage failover, a model-quality test or a production latency target. Database loss can prevent in-memory results from becoming durable, so unknown outcomes are intentional and are not replayed. The unavailable-read observation is a client timeout, not evidence of a bounded server-side 503 response; request/database deadline behavior remains a separate production hardening item. An object committed before its database reference can still become an unreferenced object. Reproduce using the [lab tutorial](../k8s/README.md#dependency-interruption-experiment); the [decision](../../../.agents/notes/implemented/architecture/2026-09-15-execution-failure-evidence.md) defines result and artifact semantics.
