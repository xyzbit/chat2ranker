# Agent Note: Serving release registry identity and alias history

Status: implemented

English | [中文](2026-09-08-serving-release-registry-foundation.zh.md)

## Problem

Agent serving needs stable public references, efficient relational storage, and an auditable answer to which immutable Agent version an alias selected. A UUID-only physical schema enlarges hot indexes and makes random UUIDv4 insertion unfriendly to B-tree locality, while public auto-increment identifiers expose storage order and couple clients to one database. Keeping only a mutable `production_version_id` cannot prove prior routing decisions or reject concurrent promotions safely.

## Decision

The Rank SQLite repository schema version 9 introduces `deployments`, `deployment_aliases`, and `release_revisions`. These serving tables use auto-assigned `INTEGER PRIMARY KEY` row identifiers for physical relationships and separate prefixed UUIDv7 `public_id` values for application, API, event, and future cross-service references. SQLite does not use its stricter `AUTOINCREMENT` keyword because physical keys never cross the repository and do not require a permanent no-reuse promise. The domain brands Deployment, DeploymentAlias, and ReleaseRevision identifiers as distinct Go string types. A production PostgreSQL adapter uses the same logical split with generated `BIGINT` identity keys.

A Deployment is a tenant-scoped stable serving target for one AgentFamily and one policy digest. It owns `latest`, `staging`, and `production` DeploymentAlias rows. Each alias stores a nullable internal pointer to its current ReleaseRevision, while ReleaseRevision rows are immutable and carry a scoped revision number, exact AgentVersion, predecessor, evaluation Run when present, policy result, actor, reason, and timestamp.

Alias advancement is a compare-and-swap operation. The caller supplies the expected public ReleaseRevision identifier; the repository verifies the current head, inserts the next revision, and updates the alias only if its internal pointer still matches. `AppendReleaseRevision` opens a transaction when its caller has not already done so, so a failed head update cannot commit an orphan revision. Rollback targets an older AgentVersion by appending another revision rather than rewriting history.

Creating a Deployment initializes its `latest` alias from the AgentFamily's current version. Creating another AgentVersion advances every matching Deployment's system-managed `latest` alias in the same Rank transaction. `staging` and `production` advance only through a trusted release-controller decision. A passed decision must reference a completed, evaluation-complete Run for that exact AgentVersion; a manual override requires an actor and reason. Exact references such as `customer-support@v17` resolve directly and therefore carry no synthetic ReleaseRevision, while alias references retain both the exact AgentVersion and ReleaseRevision.

The existing Rank Agent tables keep their pre-existing text identifiers in this slice. Their physical-key migration belongs to the planned Agent Registry extraction, where every reference can move together without an in-place compatibility layer. The new serving tables already use internal integer foreign keys between Deployment, DeploymentAlias, and ReleaseRevision.

## Verification

Repository tests assert the serving tables' integer primary keys and separate public identifiers. Application tests assert UUIDv7 generation, automatic `latest` advancement, exact-version resolution without a release revision, stale compare-and-swap rejection without an orphan row, append-only rollback, policy evidence requirements, and resolution through the committed alias head.

## Alternatives considered

**Use UUID as every primary and foreign key.** This gives one identifier but doubles key width relative to `BIGINT`, and UUIDv4 creates random index insertion. UUIDv7 improves locality but does not remove the cost from every foreign key and join.

**Expose auto-increment identifiers.** This keeps the database compact but leaks storage order, complicates cross-environment imports, and makes an implementation detail part of the public protocol.

**Resolve an alias with `ORDER BY revision DESC LIMIT 1`.** A suitable index makes the query possible, but every serving request would read the history table. DeploymentAlias is the committed O(1) projection; ReleaseRevision remains the audit history.

**Update one mutable version column.** This is simpler but destroys release lineage and cannot express rollback as a new decision with its own evidence and actor.

## Consequences

Serving identifiers have two representations, so persistence adapters must translate public references to internal keys and must never serialize the physical key. Alias reads are cheap and historical decisions remain reconstructable. Promotion conflicts fail before traffic changes, and accepted invocations freeze the returned resolution without re-reading a moved alias. The [Responses serving MVP](../feature/2026-09-09-responses-serving-mvp.md) builds its HTTP lifecycle, policy enforcement, and durable records on this registry; the PostgreSQL adapter remains in the lifecycle serving proposal.

The broader protocol and delivery plan is recorded in the [Agent lifecycle serving proposal](../../proposed/feature/2026-09-08-agent-lifecycle-serving.md).
