# PostgreSQL deadline verification — 2026-09-15

English | [中文](2026-09-15-database-deadlines.zh.md)

The local remote-runtime platform passes dependency interruption and the complete public-client regression with `rank-platform-lab:1390f12176a7e501`. [Machine-readable evidence](2026-09-15-database-deadlines.json) records the final image digests, source hashes, private report identities and observations. This follows the client-timeout gap in the [earlier dependency experiment](2026-09-15-dependencies.md).

| Verification | Observed result |
| --- | --- |
| Database absent, timeout path | The first hardened image returned HTTP 503 after 3.007 seconds with a three-second database budget. The ten-second client watchdog did not expire. |
| Database absent, final image | PostgreSQL disconnected an established connection; HTTP 500 returned after 0.003 seconds. The verifier requires a real server error in under eight seconds and rejects client timeouts or transport-only failures. An immediate disconnect need not consume the timeout budget. |
| Durable recovery and artifact interruption | Both database-loss and interrupted-upload cases retained attempt one and one external tool effect, with confirmed runtime cleanup. The upload case retained known output/usage and its confirmed artifact. Earlier artifacts remained readable and fresh work succeeded after recovery. |
| Authenticated PostgreSQL traffic discarded | A Go integration proxy kept both sockets open after authentication and discarded traffic. Query I/O returned a deadline error before its caller watchdog. Pool exhaustion, delayed queries and shorter caller cancellation also passed. |
| Rank transaction and credential lookup | Lock contention and a query started late in a transaction used the existing transaction deadline. A blocked API-key lookup returned 503, recovered to normal valid-key behavior after unlock, and still rejected invalid keys with 401. |
| Complete platform | Daily-news client, two-turn SDK chat, direct Agent trial, persistent Rank control conversation, S3 reads, SSE cursor replay, Rank/control Pod replacement and queued-request recovery passed with the final images. |

Execution's Go suite ran with real PostgreSQL. Rank passed SQLite tests and its real-PostgreSQL suite under the race detector; focused timeout tests also passed under the race detector. Lint, Markdown links and 1,005 bilingual pairs passed. After repairing the new links' bilingual target mismatch, the remaining documentation-gate failure is the pre-existing root AGENTS.md word ceiling (2,398 versus 1,950).

The lab uses three-second PostgreSQL operation budgets and explicit 45-second migration budgets. It restores PostgreSQL, the object Service selector and two Workers, and removes fault fixtures. Image identities and all credentials remain under the private lab state directory. Reproduce through the [lab tutorial](../k8s/README.md#dependency-interruption-experiment); the [decision](../../../.agents/notes/implemented/architecture/2026-09-15-postgres-deadlines.md) defines acquisition, transaction, streaming and retry semantics.

This single-laptop experiment uses deterministic models. It does not prove storage failover, physical-host capacity, production identity, disk/egress enforcement or real-provider limits. A timeout is not proof that a transaction rolled back; clients preserve request identity, and unknown external effects do not authorize replay.
