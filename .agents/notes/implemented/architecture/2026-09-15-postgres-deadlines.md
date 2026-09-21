# Agent Note: Bound PostgreSQL dependency operations

Status: implemented

English | [中文](2026-09-15-postgres-deadlines.zh.md)

## Problem

A database outage can leave API reads waiting until the client disconnects. A connection timeout alone does not bound pool contention or I/O on an established connection. A short HTTP-wide deadline would also cut off normal synchronous model calls and SSE subscriptions.

## Decision

Rank and Execution configure `-database-timeout` (default five seconds) for PostgreSQL connection acquisition, query I/O and whole application transactions. Pool acquisition and query I/O each have a budget; a nontransactional operation can spend both, and a request can contain several operations. Earlier caller deadlines remain authoritative. Explicit migrations use the same configured total transaction budget; operators can select a longer value for the migration process without changing serving Pods.

The shared Go `pgtimeout` helper uses pgx acquisition/query tracing hooks to attach operation contexts and releases their timers at acquisition completion or query closure. It bounds returned rows until closure and does not cancel the caller's parent context. Connection establishment also has a configured timeout. It does not wrap arbitrary low-level pgconn calls, COPY or batch APIs; callers introducing those paths must provide explicit deadlines. Current application SQL uses Exec/Query/QueryRow, while multi-statement application and migration work also receives a total context deadline.

Rank retains its SQL repository and uses the supported pgx `OpenDBFromPool` adapter. pgx owns the sole connection limit; `database/sql` keeps zero idle connections and no second finite pool limit. Adding another finite limit would introduce a separate unbounded acquisition wait. WithinTx passes its bounded context to the callback; each callback query uses the remaining transaction budget. Closing Rank's store closes both the SQL facade and its owned pool. Execution's credential store and River SQL share the configured pgx pool.

Raw deadline errors become HTTP 503 `dependency_timeout`, using the existing response format for management or serving routes. Existing application errors keep their own status. Credential lookup failures propagate as dependency errors; only absent, revoked or mismatched credentials return an invalid-key error. A connection refusal or server disconnect can still return a different server error before the timeout. An already-open SSE stream cannot change its HTTP status; it ends when a database operation fails, and consumers reconnect from the last committed cursor. The stream and model invocation have no new overall five-second limit.

A timeout does not prove a transaction failed to commit and does not authorize replaying model or tool work. Callers retain the logical request identity and use existing idempotency/recovery rules. Accepted tasks remain owned by durable dispatch and attempt state, independently of an HTTP client's lifetime. No automatic transaction callback retry is added.

## Alternatives considered

**Only PostgreSQL statement_timeout or connect_timeout.** Neither bounds every client pool wait and established-connection network outage.

**One short HTTP TimeoutHandler.** Buffers responses and changes streaming behavior; applying its lifetime to synchronous serving would terminate valid long invocations.

**Wrap every repository method or replace the SQL repository.** Adds repetitive policy or a persistence rewrite. The maintained pgx hooks and SQL adapter cover the current SQL paths with explicit transaction contexts.

## Consequences

Real PostgreSQL tests cover exhausted pools, delayed queries, shorter caller deadlines, schema write-lock contention and successful access after recovery. A bidirectional proxy preserves a real authenticated connection and then discards traffic without closing either socket; the operation returns before its caller's watchdog. HTTP tests verify the explicit dependency error in both APIs. These checks do not establish database high availability.

The local Kubernetes manifests use three-second operation budgets and explicit migration Jobs use 45 seconds. The [dependency experiment](../../../../rank/deploy/k8s/README.md#dependency-interruption-experiment) requires an actual HTTP server error in under eight seconds during database absence; a ten-second client timeout or transport disconnect fails verification. The [lab report](../../../../rank/deploy/reports/2026-09-15-database-deadlines.md) records bounded outage errors and the complete public-client regression. The [production proposal](../../proposed/architecture/2026-09-14-rank-production-execution.md) owns remaining fault and deployment requirements.
