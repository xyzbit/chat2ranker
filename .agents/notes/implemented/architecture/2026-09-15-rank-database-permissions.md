# Agent Note: Rank and Execution database privileges

Status: implemented

English | [中文](2026-09-15-rank-database-permissions.zh.md)

## Problem

Separate schemas do not isolate services when both use the database owner. A compromised runtime can read the other service's credentials or rewrite migration metadata, while control hosts and migration Jobs can inherit unrelated application secrets.

## Decision

The dedicated Kubernetes lab provisions four non-superuser logins: `rank_migrator`, `rank_runtime`, `execution_migrator` and `execution_runtime`. Each migrator owns only its application schema and objects. Migrators have database `CREATE` because the migration entry points check it when initializing schemas. Runtime roles have database `CONNECT`, schema `USAGE`, table SELECT/INSERT/UPDATE/DELETE, sequence USAGE/SELECT and required routine execution. They cannot create temporary tables, change schema definitions, assume owner roles or access the other service's schema. Migration version tables remain SELECT-only.

The [provisioner](../../../../rank/deploy/k8s/database_roles.py) runs through the dedicated PostgreSQL administrator during maintenance, after application producers and consumers stop. It transfers tables, sequences, routines and declared types only within the two application schemas; it never globally reassigns the administrator's objects. Unexpected role memberships fail provisioning. Owner default privileges cover future migration-created objects; metadata restrictions are reapplied after migrations and before applications start. PUBLIC database/schema and application-object grants are removed in this dedicated database. This script is not an installer for an existing shared production cluster.

Passwords remain stable in an owner-only local file. Provisioning sends SCRAM verifiers on stdin and suppresses SQL diagnostics that could contain credentials. The bootstrap administrator remains with PostgreSQL, outside application Pods. Each migration Secret contains only that service's owner database URL. Rank control receives its control and Execution service tokens, without a database URL or action-signing secret. Application Pods cannot mount migration secrets through their declared configuration and have no Kubernetes API token. This is configuration isolation, not an admission policy restricting a cluster administrator.

## Alternatives considered

**Use a common database owner.** Separate schemas would still allow cross-service reads and DDL from an ordinary application process.

**Use runtime roles for migrations.** Requires granting schema creation and version writes to long-running services. Explicit maintenance Jobs already provide a short-lived place for these privileges.

**Split into separate database clusters.** Can improve fault and administrative isolation later, but two independent schemas and restricted credentials suffice for this lab without another database service.

## Consequences

The [verifier](../../../../rank/deploy/k8s/verify-database-roles.py) authenticates over TCP with all four actual logins. It exercises owner-created tables, runtime DML and sequence defaults, cross-service conversation/credential denials, metadata protection and rejected DDL, role creation, owner impersonation and server-file reads. It inspects running control environment key names and migration Secret keys without printing values. The assembled checks have a deterministic snapshot; the complete platform verifier separately exercises published endpoints, DSH conversations, S3 and process replacement using restricted runtime connections.

The lab still uses a private network without PostgreSQL TLS and a single bootstrap administrator. Production identity, secret rotation, row-level tenant authorization, database/network policies, backups and hostile-runtime isolation need their own acceptance evidence. This decision implements database-role separation within the [production proposal](../../proposed/architecture/2026-09-14-rank-production-execution.md); it does not declare that proposal complete.
