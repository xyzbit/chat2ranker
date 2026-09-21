# Agent Note: Rank PostgreSQL business storage

Status: implemented

English | [中文](2026-09-14-rank-postgres-storage.zh.md)

## Problem

Rank needs persistent business records independent of a platform process or local SQLite file. Adding an Execution queue does not persist Rank conversations, releases or webhook delivery records. Duplicating every repository query for PostgreSQL would create two business implementations with separate fixes.

## Decision

The [SQL store](../../../../rank/backend/internal/sqlstore/store.go) owns shared business queries and separate SQLite/PostgreSQL migrations. Queries use numbered parameters, portable conflict handling and null-safe comparisons. Boolean arguments for integer columns and comparisons convert explicitly to 0/1, including the direct-only trial-history filter. PostgreSQL uses generated bigint identities, preserves public identifiers, and indexes the JSON fields used for history lookup. Foreign keys are installed after all tables so conversation/response and alias/revision relationships remain enforced despite their circular references.

PostgreSQL migration is explicit and transactional, serialized by an owned-schema migration lock. The API checks the exact supported version before serving; missing and future versions are refused. SQLite retains its development migration path. PostgreSQL starts with a fresh database and does not silently import SQLite records.

Short Rank business write transactions share a schema-specific PostgreSQL advisory transaction lock. Admission counts, conversation-head updates, event sequences and release compare-and-swap operations remain atomic across database connections. Model and Execution calls run outside that transaction. This retains the initial single-coordinator deployment and does not claim high write throughput or safe replication of background coordinators.

PostgreSQL server startup requires explicit internal action/control/Execution secrets and a persistent webhook-secret directory. Model credentials remain owned by Execution. Database URLs are environment inputs, never command arguments or health-response fields. The [backend reference](../../../../rank/backend/README.md) owns flags, startup and test configuration.

## Alternatives considered

**Copy the entire repository.** Doubles business-query maintenance and invites different domain behavior for each database. Shared queries keep migration and connection differences explicit.

**Introduce a runtime SQLite-to-PostgreSQL SQL translator or ORM.** Adds interpretation or remapping to already small database/sql queries. Explicit portable SQL and dedicated DDL avoid that machinery.

**Add fine-grained distributed coordinator ownership immediately.** Does not solve durable dispatch or original-input recovery by itself. Initial serialized write transactions trade platform write throughput for simpler admission and transaction safety; narrower locks require measured contention and their own concurrency evidence.

## Consequences

Real PostgreSQL tests exercise competing pools, admission rollback, conversation-head conflicts, release CAS, event ordering and migration/version refusal. The same application and HTTP tests run against SQLite or isolated PostgreSQL schemas. Trial-history tests cover family, exact version, direct-only, tenant and cursor filters; the packaged trial snapshot asserts the selected database and uses its configured internal control credential. The packaged serving/release snapshots pass with PostgreSQL Rank and SQLite Execution, including Rank restart followed by stored response and webhook-outbox retrieval; these are deterministic harness tests, not real-model capacity measurements.

Database persistence alone does not complete Rank-to-Execution dispatch. [Serving dispatch](2026-09-14-rank-serving-dispatch.md) supplies frozen request recovery for Responses and trials. [Evaluation dispatch](2026-09-14-rank-evaluation-dispatch.md) covers candidate/Judge invocation recovery. [Structured history](2026-09-14-rank-structured-history.md) and [the platform lab](2026-09-14-rank-platform-lab.md) provide DSH continuation and Kubernetes recovery evidence. Runtime isolation remains in the [production proposal](../../proposed/architecture/2026-09-14-rank-production-execution.md).
