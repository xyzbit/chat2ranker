# Agent Note: Frozen DSH model request budgets

Status: implemented

English | [中文](2026-09-15-execution-model-budget.zh.md)

## Problem

An execution timeout and a per-error retry limit do not bound total model requests. A tool loop, repeated transient failures or auxiliary model calls can consume many requests within one execution. Increasing Worker replicas must not reset already accepted execution budgets.

## Decision

Execution resolves a total model-request cap for capable adapters at admission. The service flag defaults to 32; a request can only tighten it. The resolved value lives outside the immutable request used for idempotency, so repeated submissions retain their allowance even when service defaults change. SQLite and PostgreSQL persist the cap, and version 2 of the internal worker protocol carries it explicitly. A newly capable Worker refuses old DSH work without a resolved cap before starting it; an incapable Worker leaves budgeted work queued. Capacity projection excludes incompatible work.

The first-party DSH adapter mounts an invocation-owned plugin on the existing `llm/stream` extension point. It reserves synchronously before calling the next listener. All requests through that DSH LLM service, including retries, compaction and auxiliary calls, consume the same total. The provider adapters retain their existing finite retry policies and disable underlying SDK retries; the new total also bounds successive failure/recovery cycles. The request exceeding the total throws a plugin error before provider dispatch, preventing model-error recovery from restarting it. Independent execution and Sandbox deadlines still bound waiting and cleanup.

Every accepted execution starts a fresh DSH process. The application never retries a started invocation, so the in-process counter is not reconstructed after owner loss or reset by queue delivery. Original requests and errors remain in native session events, and the Execution record retains its frozen cap. No additional model-visible text or general-purpose scheduler is introduced.

Capability reporting is explicit. Native CLIs and custom command adapters do not claim this limit; requesting it fails admission. A configured DSH image must package the plugin and worker protocol together. This is cooperative application metering, not a security boundary against tools using raw sockets, plugin removal or arbitrary code. It does not replace production identities, provider RPM/TPM, output-token limits or monetary budgets. A shared model gateway remains a separate decision when those requirements are implemented.

When a later model call exhausts the budget, the DSH adapter retains usage already observed in its native session history. Failure does not imply zero usage; missing usage remains explicitly unavailable.

## Alternatives considered

**Count River deliveries.** Deliveries are not model requests. One Harness invocation can make many calls, while a repeated completed delivery makes none.

**Only shorten execution timeout.** A short burst can consume the request allowance before the timer expires, and slow providers can exhaust time with very few calls.

**Reimplement provider retry or add a model gateway solely for this cap.** DSH already owns single-attempt adapters and finite retry policy. A small listener bounds aggregate invocation work without duplicating the transport stack; it makes no claim about distributed provider quotas.

## Consequences

The [minikube report](../../../../rank/deploy/reports/2026-09-15-model-budget.md) records provider-side request counts, packaged remote snapshots and final-image cleanup checks. The actual remote stream includes post-terminal keepalives; the Executor ignores them without accepting incomplete command results.

The [runtime reference](../../../../apps/execution-runtime/README.md) defines plugin scope. Built DSH tests count actual local HTTP requests for persistent 429, authentication failure and transient recovery through prompt and structured-history drivers. The [platform verifier](../../../../rank/deploy/k8s/verify-platform.mjs) compares the public failure result and provider-side request count with a checked-in snapshot, repeats the same logical request, and confirms cleanup. Repository tests retain the budget across storage and capacity projection; application tests reject unsupported adapters and preserve accepted limits across default changes.

The [production proposal](../../proposed/architecture/2026-09-14-rank-production-execution.md) continues to require shared provider limits and real-provider validation. This cap controls per-execution request amplification; it does not prove those separate requirements complete.
