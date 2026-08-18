# Agent Note: Separate Rank control and harness execution processes

Status: proposed

English | [中文](2026-08-18-rank-control-and-execution-planes.zh.md)

## Problem

chat2ranker uses DSH to host the product conversation and also needs to evaluate agents implemented with DSH, Pi, Claude Code, Codex, Hermes, and future harnesses. Creating a tested DSH Agent inside the Control DSH process would share Cordis services, Session persistence, workspace state, credentials, and plugin globals that other harnesses cannot access. The result would compare different execution models and allow tested code to affect the product control Session.

DSH Session logs preserve agent execution history, but they do not own dataset versions, agent versions, experiment transactions, cross-harness scheduling, Judge results, or aggregate cost and pass-rate records.

## Proposal

Control DSH will host the persistent experiment Session, Rank Skill, Rank tools, and A2UI. A Go Rank service will own datasets, agent configurations, experiments, runs, scheduling, judging, and aggregation. The Control DSH Rank plugin will call that service through a versioned wire API.

Every tested harness will implement one Runner protocol and execute in an isolated process, container, or cluster job. DSH, Pi, Claude Code, Codex, and Hermes will be peer Runner implementations. A tested DSH instance will create its Agent inside the Runner process and will not share a Cordis context, Session store, DSH home, workspace, environment, or credentials with Control DSH.

Rank workers will lease immutable execution specifications, create Sandboxes, forward normalized ordered events, and retain native harness traces. Judge work will use an independent execution and will not reuse the Control DSH Session or tested Agent process.

Product code will remain isolated under `rank/`, `packages/rank/`, and `apps/rank-web/`. Upstream DSH core packages will change only when the product demonstrates a missing extension point.

## Alternatives considered

**Create tested DSH Agents through the Control DSH `ctx.agents` service.** This has the smallest prototype surface but produces unequal isolation and lets tested global state affect the control process.

**Make DSH own Rank business state and scheduling.** This reuses the Session log outside its purpose and couples cross-harness experiment semantics to one Runner implementation.

**Launch arbitrary CLI commands without a common Runner protocol.** This preserves process separation but cannot guarantee comparable lifecycle events, cancellation, usage, artifacts, or terminal results.

**Keep Rank as a TypeScript plugin in the Control DSH process.** Execution could still be external, but long-running scheduling, leases, database transactions, and worker coordination would remain coupled to the conversational host lifecycle.

## Acceptance criteria

- Control DSH communicates with Rank through a versioned process API and cannot start a tested Agent directly.
- `rankd` freezes dataset and agent versions before creating per-case executions.
- DSH and at least one deterministic Mock Runner pass the same protocol conformance suite.
- Every case and Judge invocation runs in an isolated process or container with scoped workspace and credentials.
- Rank persists business records independently from DSH Session logs while retaining links to native execution traces.
- An end-to-end test proves conversation action, run dispatch, event streaming, judging, aggregation, and result projection through real process entry points.

## Risks

Process and protocol boundaries increase initial implementation and deployment work. Event ordering, cancellation, worker loss, artifact publication, and terminal-state idempotency require explicit contracts and conformance tests.

The product fork must keep Rank assembly changes isolated so upstream DSH synchronization does not become a recurring broad merge. A product requirement that lacks a DSH extension point may still require a narrowly documented upstream patch.
