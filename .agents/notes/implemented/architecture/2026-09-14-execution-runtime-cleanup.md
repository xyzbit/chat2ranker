# Agent Note: Runtime cleanup retains Execution capacity

Status: implemented

English | [中文](2026-09-14-execution-runtime-cleanup.zh.md)

## Problem

A failed invocation can leave an unconfirmed runtime alive. Counting only running and cancelling records releases model and tenant capacity when an owner disappears, even though another invocation may still consume those resources. A recorded timeout or terminal River job does not prove runtime shutdown.

## Decision

Execution persists `runtimeCleanupPending` when an attempt enters running, before calling its Executor. The application clears it only when the Executor returns with confirmed shutdown; errors wrapping `ErrRuntimeStopUnconfirmed` retain it. Confirmation is attempt-fenced and idempotent, including after a failed business outcome. Confirmation changes neither the business outcome nor River delivery state. Reconciliation reports unknown outcomes without clearing the cleanup obligation, and redelivery never invents shutdown evidence.

PostgreSQL admission counts running/cancelling records and terminal records with pending cleanup as model and tenant reservations. Repository completion and cancellation reject an uncleared obligation; failure preserves it. PostgreSQL schema 6 and SQLite schema 10 retain pending cleanup for existing active attempts and failed records reporting unknown outcomes or unconfirmed shutdown. Repeating a completed migration preserves later cleanup confirmations.

The [OpenSandbox adapter](2026-09-15-execution-opensandbox.md) uses this state for remote cleanup reconciliation. The local Executor remains suitable only for trusted workloads: process-group handling cannot prove isolation of arbitrary detached descendants. Lost local owners can retain reservations indefinitely. There is no automatic expiry or public force-release operation; operators must investigate retained reservations rather than increase capacity on the assumption that the old runtime stopped.

## Alternatives considered

**Release capacity at business failure or timeout.** It allows new invocations while unknown work remains active. A runtime's independently enforced lifetime can contribute shutdown evidence, but an application timer alone cannot.

**Keep the business response running until cleanup succeeds.** It conflates an unknown result with pending resource cleanup and leaves callers waiting without a useful outcome. Separate persistent cleanup state preserves both facts.

## Consequences

Real PostgreSQL tests verify that unknown runtimes block both model and tenant admission until confirmed shutdown. SQLite/PostgreSQL repository tests reject stale confirmations and premature success/cancellation. Application tests distinguish ordinary runtime failure from unconfirmed stop, and migration tests cover persisted old records and repeated upgrades. The [packaged lifecycle example](../../../../rank/examples/execution-lifecycle/README.md) snapshots pending cleanup while running and cleared cleanup after normal completion/cancellation in SQLite and PostgreSQL/River modes.

The [minikube upgrade report](../../../../rank/deploy/reports/2026-09-14-runtime-cleanup.md) records the schema upgrade and a passing full-platform regression on the resulting image.
