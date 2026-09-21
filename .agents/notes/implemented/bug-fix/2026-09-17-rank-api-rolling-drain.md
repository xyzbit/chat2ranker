# Agent Note: Rank API rolling replacement and bounded HTTP drain

Status: implemented

English | [中文](2026-09-17-rank-api-rolling-drain.zh.md)

## Problem

A single `Recreate` Rank API leaves Nginx without a reachable upstream during replacement. A five-second HTTP drain cuts off slower responses. Replicating the combined process would also duplicate recovery ownership and lose process-local login sessions.

## Decision

[Rank process modes](../../../../rank/backend/README.md#process-modes-and-http-draining) separate shared-database HTTP admission from exactly one dispatcher. API instances never start serving or evaluation observers, including cancellation paths. The dispatcher discovers durable work periodically without resetting its active runs. Its own replacement remains `Recreate`; HTTP-only replacement leaves accepted Execution tasks and their observer running.

[Login storage](../../../../rank/backend/internal/sqlstore/sessions.go) holds cookie and credential digests in SQLite schema 23 or PostgreSQL schema 4. A transaction serializes rotation, expiry cleanup and installation-wide capacity. Every request revalidates the live credential file; shared logout remains revocable without storing raw credentials or introducing Redis. The in-memory implementation remains an isolated-test provider.

The lab's API Deployment uses zero unavailable replicas, one surge replica, five-second minimum readiness and preStop, a configurable 30-second HTTP drain and 45-second Pod termination grace. HTTP timeout closes remaining connections but does not cancel durable remote tasks. Consumers must distinguish a stream ending from a Response completing and resume through its ID/cursor. Shared event polling bounds latency when the notifying dispatcher is another process.

This partially supersedes the single-process details in the [platform lab](../architecture/2026-09-14-rank-platform-lab.md) and [management access](../architecture/2026-09-15-rank-management-access.md) decisions. Their packaging, maintenance, authorization and revocation rationale remains active; [serving recovery](../architecture/2026-09-14-rank-serving-dispatch.md) retains one observer owner.

## Alternatives considered

**Increase only the shutdown timeout.** Helps accepted requests but leaves the `Recreate` interval without an API listener.

**Replicate the combined process.** Startup recovery resets active evaluation state and assumes one owner. HTTP scale-out does not justify changing that execution ownership model.

**Add Redis or replace operator login with a new identity service.** The existing database provides the required shared expiry, capacity and revocation without another service or authentication protocol.

## Consequences

The [runnable rolling snapshot](../../../../rank/examples/api-rolling/README.md) uses evaluated publication, real DSH workers, slow and over-budget streams, continuous gateway reads, login preservation, shared logout, one Execution attempt and stored SSE replay. Backend tests cover HTTP completion beyond five seconds, bounded close, shared session capacity/rotation/revocation and persistent digests on SQLite and PostgreSQL.

Initial schema migration uses the maintenance path and can interrupt service. The HTTP rolling guarantee does not cover dispatcher/control-host replacement, incompatible schema changes, exhausted surge capacity or physical-node loss. The dispatcher remains a single-owner component rather than a new distributed scheduler.
