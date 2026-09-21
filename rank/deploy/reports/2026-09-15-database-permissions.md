# Database permission experiment — 2026-09-15

English | [中文](2026-09-15-database-permissions.zh.md)

The complete minikube platform passes its public-client regression with independent restricted Rank and Execution database logins. [Machine-readable evidence](2026-09-15-database-permissions.json) records image identities, deployment/verifier hashes and checks; credentials remain outside the repository.

| Check | Observed result |
| --- | --- |
| Maintenance migration | Execution schema 5 and Rank schema 3 opened through dedicated migrator accounts; application writers remained stopped until migration and grants completed. |
| Runtime DML | Real password-authenticated TCP logins inserted, read, updated and deleted owner-created probe records; sequence defaults worked. |
| Cross-service access | Execution could not read Rank conversations or API credentials; Rank could not read Execution credentials or executions. |
| Elevated operations | Both runtime accounts were denied schema/table creation, temporary tables, ALTER/DROP/TRUNCATE, owner impersonation, role creation and server-file reads. |
| Migration metadata | Runtime reads succeeded; writes were denied for Rank and Execution/River version tables. |
| Credential exposure | Migration Secrets contained only their database URL; running control had neither database credentials nor the action-signing secret. Application Pod declarations omitted migration credentials and Kubernetes API tokens. |
| Full platform | Daily-news request, two-turn SDK chat, direct trial, persistent control conversation, S3 reads, SSE replay, Pod replacement and queued-request recovery passed. |

The first migration attempt failed because its account lacked database CREATE, which the current schema initialization checks require even for an existing schema. No application writers restarted. Granting CREATE only to migrators allowed the maintenance Job to finish. Runtime accounts retain no CREATE permission.

This run used the existing populated lab database, not a fresh-database migration test. It verifies configuration and PostgreSQL authorization on one laptop, with a deterministic model provider. It does not establish production TLS, identity, credential rotation, row-level tenant authorization, physical high availability or hostile-code isolation. Reproduce using the [lab tutorial](../k8s/README.md#database-permission-experiment); the [decision](../../../.agents/notes/implemented/architecture/2026-09-15-rank-database-permissions.md) owns privilege semantics.
