# Rank protocol

English | [中文](README.zh.md)

This directory will own the versioned wire definitions between Control DSH, `rankd`, `rank-worker`, and Runner processes.

The first protocol revision will define:

- experiment, dataset version, agent version, run, and run-item identifiers;
- immutable `ExecutionSpec` input for one case;
- ordered execution events for messages, model calls, tool calls, usage, artifacts, completion, and failure;
- `ExecutionResult` with final output, exit status, duration, token usage, cost, and raw trace location;
- create, start, watch, cancel, and query operations for Rank runs.

Generated Go and TypeScript code will be derived from these definitions. Hand-written transport DTOs are not allowed once the protocol is introduced.
