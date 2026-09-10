# Agent Note: Stateful Response continuation

Status: implemented

English | [中文](2026-09-09-stateful-response-continuation.zh.md)

## Problem

The serving MVP freezes one AgentVersion for each accepted Response, but independent Responses cannot preserve a multi-turn interaction. Resolving `production` again for every turn can move one user conversation across Agent versions after a release, while accepting two continuations from the same predecessor can fork history or run conflicting work.

## Decision

Every newly accepted Response belongs to a durable Conversation. The Conversation is tenant-scoped, bound to one Deployment, and pins the AgentVersion and optional ReleaseRevision selected for its first Response. The public Response object returns the Conversation identifier and nullable `previous_response_id`.

`previous_response_id` may name only the latest completed Response in the same Conversation. A continuation must authenticate to the same tenant and Deployment. Alias selectors retain the Conversation's pinned version even when the alias has moved and report `deployment.resolution_kind: conversation`; an exact selector naming another AgentVersion fails instead of migrating the Conversation.

Admission inserts the Response and compare-and-swaps the Conversation head in one SQLite transaction. A partial unique index permits at most one queued or in-progress Response per Conversation. Idempotent replay is checked before head validation, so retrying an already accepted continuation returns the original Response after the head has advanced.

Execution input is reconstructed from the ordered durable Response chain. Each completed Response contributes its normalized user input and terminal assistant output, followed by the new user input. A predecessor that is incomplete, no longer the head, from another tenant or Deployment, or from a pre-Conversation record is rejected before Execution starts.

Conversation and Response rows use integer physical keys for joins and prefixed UUIDv7 public identifiers at the API. The Conversation head references the Response physical key; predecessor and Conversation links on Response rows remain nullable so existing SQLite databases migrate without rewriting historical records.

## Verification

HTTP integration coverage creates a first Response through `production`, advances that alias to another AgentVersion, and verifies that a continuation keeps the original version and release lineage. It also pins reconstructed history, idempotent continuation replay, stale-head rejection, and exact-version mismatch rejection. The assembled Rank, Execution Service, and Worker snapshot exercises the public continuation request and verifies its returned links and history-aware execution output.

## Alternatives considered

**Resolve the requested alias again for each turn.** Rejected because a promotion could silently change behavior within one Conversation and make its history impossible to attribute to one immutable Agent version.

**Store the complete transcript on every Response.** Rejected because duplicated history grows quadratically and creates multiple writable copies. Ordered predecessor links plus the Conversation head retain one durable chain.

**Allow concurrent branches from one predecessor.** Rejected because the public API currently exposes one linear Conversation and has no branch-selection or merge semantics. A single live writer makes ordering deterministic.

**Move long-lived runtime state into Execution Service.** Rejected because Execution owns one private attempt, while Conversation identity, public authorization, version lineage, and Response history belong to the serving domain.

## Consequences

Clients can continue a completed Response without version drift and can retry an accepted continuation safely. The transaction and unique index prevent two live writers from creating ambiguous history. Existing Responses created before the Conversation migration remain readable but cannot become continuation predecessors. History is currently reconstructed in full; explicit retention, context compaction, Conversation migration, and webhooks remain later lifecycle work.
