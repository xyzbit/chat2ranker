# Agent Note: Responses-compatible Agent serving MVP

Status: implemented

English | [中文](2026-09-09-responses-serving-mvp.zh.md)

## Problem

The release registry identifies which immutable Agent version should receive traffic, but external callers still need a tenant-authorized invocation record, a stable protocol, reconnectable output, admission limits, and crash recovery. Sending public traffic to the internal Execution Service would let callers choose trusted harness, credential, system-prompt, tool, workspace, and Executor fields and would expose internal worker events as an application protocol.

## Decision

Rank exposes the supported subset defined by the [Agent Serving API reference](../../../../rank/docs/serving-api.md) and its [OpenAPI 3.1 document](../../../../rank/docs/openapi.yaml). `POST /v1/responses`, retrieval, cancellation, synchronous waiting, background dispatch, create-time streaming, retrieval-time SSE replay, and the [stateful continuation extension](2026-09-09-stateful-response-continuation.md) use the OpenAI Responses object and event vocabulary where the implemented semantics match. The reference defines the rich-input, structured-output, and caller-function extensions; unknown fields and unsupported token-limit or non-durable requests fail explicitly.

A serving credential stores only a SHA-256 secret digest and a display prefix. Its tenant, operation scopes, and Deployment allowlist authorize every data-plane request. The plaintext `rnk_live_` key is returned only when the credential is created. The local `/api` management endpoints are operator-plane integration points rather than part of the public compatibility document.

Request acceptance resolves `deployment@version-or-alias` exactly once. The durable Response stores the requested reference, Deployment, exact AgentVersion, optional ReleaseRevision, credential, normalized text input, metadata, idempotency digest, lifecycle times, Execution identifier, terminal output, token usage, cost, and failure. Exact selectors name the Agent version number; they do not create or refer to a ReleaseRevision. Alias movement after acceptance cannot change execution or idempotent replay.

Rank constructs the private Execution specification from the frozen AgentVersion and Deployment policy. Public input cannot override the model connection, credential reference, harness, preset, system prompt, built-in tools, Skills, workspace, Executor, or artifact path. Deployment policy supplies requests per minute, maximum active Responses, execution timeout, and caller-instruction permission.

The operator UI keeps the Rank conversation as the primary workflow and exposes manual control-plane pages through one desktop sidebar. New chat is an action rather than a Workspace destination; page navigation appears above the recent-chat list, and a header search action reveals filtering only when needed. Mobile keeps compact navigation and opens recent chats from the top bar. Experiment remains the durable selection and Run grouping behind each conversation rather than a duplicate top-level page. Publishing in conversation produces an A2UI confirmation card that compares the current alias target with the candidate Agent version, keeps the manual-override reason or evaluation evidence reviewable, and links to the Deployment page. The Deployment page reads real release and policy state, leaves traffic empty until serving data exists, and opens a detail surface only for work that does not fit in the card. New control-plane copy is available in Chinese and English without translating persisted conversation content.

Admission checks and Response insertion run in one SQLite transaction. Limits are scoped to the credential and Deployment, and composite indexes cover the request-window and active-status admission queries. A credential-scoped idempotency key returns the original Response only when its normalized request digest matches; another payload receives a conflict. Execution submission uses `response:<response-id>` as its private idempotency key.

Public events are stored before delivery with a Response-local sequence. Create streaming and `GET ...?stream=true` project only durable Responses events; `starting_after` and `Last-Event-ID` resume after a committed sequence. Rank startup dispatches persisted queued and in-progress Responses, and Execution idempotency reattaches recovery to the same invocation.

The schema follows the release registry's dual-identifier decision: hot relations use integer physical keys while API credentials and Responses expose prefixed UUIDv7 identifiers. ReleaseRevision physical keys use ordinary `INTEGER PRIMARY KEY`, not SQLite `AUTOINCREMENT`, because repository-local row identifiers do not require permanent non-reuse.

The manual pages expose model-connection verification and default model bindings, immutable Agent comparison and evaluation evidence, Deployment creation and alias-local release history, bounded request history with on-demand events, redacted credential listing and revocation, and failed Webhook delivery replay. Request cursors use physical integer ordering behind public IDs; release cursors use the alias-local revision index. Lists exclude request transcripts and attachment bytes. Revocation rejects new authorization without cancelling already accepted work. Manual delivery replay preserves event identity and payload and requires an enabled endpoint.

Agent drafts retain their configuration when opening model settings and selecting a verified connection. Connection metadata is saved before verification; a failed verification refreshes its visible status and does not present it as verified. Opening evaluation evidence selects the Run's owning experiment before displaying its frozen snapshots. Selecting the comparison version as the primary version clears self-comparison. A Deployment's displayed public reference uses a released production alias, otherwise its released latest alias, so an unpublished production reference is not advertised as callable.

The optional Rankling launcher opens the existing conversation beside a management page rather than creating another session or assistant. Mobile presents that conversation as a modal with focus containment; the full-conversation action retains the selected experiment. Codex v1/v2 pet imports validate local PNG/WebP dimensions and persist only in browser IndexedDB. The v2 renderer uses the standard working/idle rows and pointer-facing gaze cells, and freezes motion under the browser's reduced-motion preference. The conversation renderer isolates malformed recognized Rank Message blocks instead of exposing the entire protocol payload or discarding neighboring valid blocks.

## Verification

Go integration tests cover exact and alias resolution, durable stream events, idempotent replay and conflict, authentication, scope denial, request-rate and concurrency rejection, cancellation, strict unsupported-field errors, and active-Response recovery through a reconstructed Service. The assembled process test starts Rank, Execution Service, and Worker binaries, then creates a Deployment, promotes `production`, issues a credential, invokes synchronous and streaming Responses, replays idempotency, and retrieves the persisted result. A real DeepSeek-backed local invocation verifies the same public endpoint through the current Control model connection.

## Alternatives considered

**Expose Execution Service.** Rejected because its caller is trusted Rank code and its request contains privileged runtime configuration with no tenant, release, or public lifecycle semantics.

**Create a custom invocation protocol.** Rejected because a strict Responses-compatible subset supplies familiar request, response, status, and stream vocabulary while still allowing unsupported behavior to fail deterministically.

**Resolve aliases during every retry.** Rejected because an accepted Response could cross Agent versions after a promotion. Persisted resolution and Execution idempotency keep retries on one exact version.

**Use public UUIDs as physical foreign keys.** Rejected because wider hot indexes and joins buy no wire benefit. UUIDv7 remains the public identifier and an integer remains the repository-local relation key.

## Consequences

External applications can invoke evaluated Agent versions without seeing Rank Runs or the private Execution API. Operators can expose multiple Deployments for one Agent family with different credentials and creation-time policies, and every accepted alias request retains its release lineage. Conversation and deterministic pages share the same persisted product state. A missing pricing source is reported as unknown rather than zero cost. Local operator endpoints remain trusted and unauthenticated; they must not be exposed publicly. Signed artifact access, weighted routing, PostgreSQL, distributed leases, and production operator authentication remain outside this implementation; the [broader lifecycle proposal](../../proposed/feature/2026-09-08-agent-lifecycle-serving.md) owns those phases.
