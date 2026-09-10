# Agent Note: Strict structured Response output

Status: implemented

English | [中文](2026-09-09-structured-response-output.zh.md)

## Problem

The public Responses endpoint could return plain text only. Accepting `text.format` without enforcement would let a caller request JSON Schema output while the harness or worker still completed arbitrary text, making the compatibility claim unsafe.

## Decision

`POST /v1/responses` accepts `text.format.type: json_schema` with a 1–64 character name, optional description, object schema, and `strict: true`. Rank canonicalizes and validates the schema, persists it with the Response, includes it in the idempotency digest, and copies it into the immutable private Execution specification.

Execution protocol version 2 carries the structured-output requirement. Agent harnesses receive an explicit JSON-only instruction and the schema. The worker parses the final output as exactly one JSON value and validates it against Draft 2020-12 before completion. Successful results retain the compact JSON value in `Result.structured`; invalid output fails the Execution and therefore the public Response.

Schemas cannot use external `$ref`, `$dynamicRef`, or `$recursiveRef`. This prevents worker-side network or filesystem loading and makes validation reconstructable from the persisted Execution specification. Judge executions reject structured output because their verdict parser owns a separate result format.

## Verification

Contract tests cover valid values, wrong types, extra properties, Markdown fences, malformed schemas, size limits, and external references. Service tests reject invalid specifications before persistence. Worker tests prove valid mock output completes and schema-breaking output fails. Rank HTTP tests prove strict request parsing, format persistence, immutable Execution propagation, and response projection. The assembled keyless serving snapshot covers structured output through Rank, Execution Service, and the worker.

## Alternatives considered

**Validate only in Rank after Execution completes.** Rejected because every Execution consumer should receive the same success guarantee, and the worker is the first component that owns the complete harness output.

**Trust the prompt instruction.** Rejected because models can emit Markdown fences, prose, or schema-breaking values even when prompted for JSON.

**Allow remote schema references.** Rejected because validation would then depend on mutable external content and worker network or filesystem access.

## Consequences

Callers can depend on completed `json_schema` Responses matching the persisted schema. Plain text remains the default. Webhooks remain separate lifecycle work.
