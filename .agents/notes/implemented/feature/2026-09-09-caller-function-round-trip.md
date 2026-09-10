# Agent Note: Durable caller function and approval round trip

Status: implemented

English | [中文](2026-09-09-caller-function-round-trip.zh.md)

## Problem

A deployed Agent could return text or schema-constrained JSON but could not request a function owned by the external caller. Treating caller functions as built-in Agent tools would give the Worker credentials and execution authority it must not have. A function result or approval also needs a durable relationship to the exact call and version-pinned Conversation rather than an unstructured follow-up message.

## Decision

`POST /v1/responses` accepts up to 16 strict function definitions when the Deployment enables `allowCallerTools`. Rank canonicalizes and validates each unique name and Draft 2020-12 parameter schema, persists the definitions with the Response, includes the requested definitions in its idempotency digest, and adds them to the immutable model-visible Execution prompt. Public callers cannot alter the AgentVersion's built-in tool list.

An Agent requests caller execution by returning one exact internal JSON envelope. Rank recognizes that envelope, rejects undeclared names or schema-breaking arguments, and projects a Responses `function_call` output item with a generated `call_id`. Rank does not execute the function. Normal Agent text remains normal output even when functions are available.

The caller submits one top-level `function_call_output` item with the matching `call_id` and a non-empty string through `previous_response_id`. The new Response inherits the Conversation's frozen function definitions and rejects replacement, missing, duplicate, stale, or unexpected results. Conversation reconstruction records the function request and result before the next Execution.

A definition with `require_approval: true` produces `requires_action` and a pending approval instead of a completed callable request. The `responses:approve` scope controls `/v1/responses/{id}/approval`. Approval decisions are idempotent, durable events; approval completes the Response and permits its result continuation, while rejection cancels it. Approval never authorizes Rank to execute caller code.

## Verification

Rank HTTP tests cover schema validation, Deployment policy, durable definitions and calls, pending approval, idempotent approval, matching function output, inherited definitions, Conversation history, and rejection of invalid output. The assembled serving snapshot drives a function request, scoped approval, and result continuation through `rankd`, `executiond`, and a real worker process.

## Alternatives considered

**Mount caller functions as Worker tools.** Rejected because the external application owns implementation and credentials; Rank cannot safely manufacture an executable plugin from a public request.

**Accept a tool result as ordinary user text.** Rejected because the server could not prove which call the value answers or reject stale and duplicate results.

**Let approval execute the function.** Rejected because approval is only a policy decision. The external caller remains responsible for side effects and submits the resulting value explicitly.

**Allow tool definitions to change during continuation.** Rejected because earlier calls and later results would then be interpreted under different schemas inside one pinned Conversation.

## Consequences

External systems can complete caller-owned function loops without exposing credentials to the Agent worker, and every call, approval, and result remains reconstructable after restart. Function selection currently uses a strict prompt envelope rather than a harness-native tool-call channel, so malformed function envelopes fail the Response and parallel caller calls are not supported. Webhook delivery remains separate lifecycle work.
