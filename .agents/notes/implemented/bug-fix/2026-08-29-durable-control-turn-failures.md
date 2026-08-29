# Agent Note: Durable control turn failures

Status: implemented

English | [中文](2026-08-29-durable-control-turn-failures.zh.md)

## Problem

Control DSH could emit streamed reply fragments and then fail before an `assistant/message`. The transient fragments disappeared, the failed turn was absent after refresh, and the user received no durable explanation or retry path.

## Decision

The Control Host flushes and reconciles the DSH transcript for both successful and failed turns. A failed turn appends one deterministic Rank Message JSONL error message containing a short user explanation and redacted technical detail. The frontend renders that message in the conversation, keeps technical detail collapsed, and offers a retry of the preceding user message. Streamed fragments remain provisional and are not persisted as a completed answer.

## Alternatives considered

**Keep only the composer error banner.** This lost the failure on refresh and separated it from the request that failed.

**Persist the partial model output.** A prefix terminated before tool execution or final synthesis is not a trustworthy answer and would be difficult to distinguish from completed content.

## Consequences

Failures remain visible and actionable without presenting incomplete output as final. Rank stores one additional synthetic assistant message for each failed DSH turn; its deterministic id keeps transcript reconciliation idempotent.
