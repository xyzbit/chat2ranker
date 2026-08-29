# Agent Note: Rank multi-Agent comparison

Status: implemented

English | [中文](2026-08-25-rank-multi-agent-comparison.zh.md)

## Problem

An experiment could compare Agent versions only by selecting and running them one at a time. Users could not prepare one immutable dataset snapshot, select several Agent versions, and start a fair comparison as one action. Putting several Agents inside one Run would have weakened the existing execution and accounting invariants.

## Decision

Single-Agent execution remains the default. One selector on the run-confirmation A2UI card accepts one to four Agent versions: one selection replaces the current Agent, while several selections prepare a comparison and show the resulting total Trial count before confirmation. Closing the selector applies its draft, reopening restores it, and selected versions appear in a compact scrollable list with hidden scrollbars and an overflow fade; each row opens that exact version in the inspector. The Control Agent can also pass named immutable Agent version IDs and a repeat count into the confirmation card. It describes multi-Agent execution as a RunGroup with independent child Runs instead of claiming that the platform requires sequential manual runs.

Rank persists one RunGroup for the comparison request and atomically creates one child Run per selected Agent version. Every child freezes the same DatasetVersion and trial count, freezes its own AgentVersion and evaluator configuration, owns separate Trial, cost, trace, and result records, and is dispatched through the existing single-Run path. The RunGroup owns comparison idempotency and records the ordered Agent version and child Run IDs; its status is derived from child Run states. Restart recovery continues to operate on child Runs, so the execution service, Runner adapters, and Harness processes receive no multi-Agent protocol.

The browser subscribes to every active child Run concurrently. Completed child Runs remain ordinary experiment results and therefore enter the existing pass-rate versus cost or latency chart without a separate comparison dashboard.

## Alternatives considered

**Store several Agents inside one Run.** Rejected because Run snapshots, costs, retries, cancellation, events, and result aggregation are intentionally scoped to one tested Agent version. Expanding that entity would couple comparison UX to every Runner and persistence path.

**Create Runs sequentially from the browser.** Rejected because partial creation, repeated clicks, and browser interruption could leave an incomplete comparison with no parent idempotency record.

**Add multi-Agent behavior to Execution Service.** Rejected because fan-out is Rank business orchestration. Execution Service should continue to accept one harness task and report one execution lifecycle.

## Consequences

One confirmation can compare several Agents under the same dataset and repeat policy while preserving existing execution isolation and per-Agent accounting. A comparison increases provider concurrency and total cost in direct proportion to the number of selected Agents; the confirmation card makes that multiplication visible and limits one group to four Agents. Group-level cancellation and a dedicated group result card are not required because child Run controls and the experiment comparison chart already expose the necessary operations and results.
