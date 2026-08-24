# Execution backend

English | [中文](README.zh.md)

This Go module is a reusable execution control plane. It produces:

- `executiond`: versioned HTTP API, idempotent execution records, lifecycle control, Repository access, Executor selection, and Artifact authorization.
- `execution-worker`: one immutable Harness Adapter invocation with a private workspace and Harness Home.

The public `contract` and `client` packages are the only integration surface. Domain and application code depend on Repository and Executor interfaces. The local assembly uses SQLite and `LocalExecutor`; PostgreSQL, Docker, Kubernetes Job, Kata, and remote Sandbox adapters can replace them without importing Rank business types.

Harness integrations implement the public `harness.Adapter` interface and register by stable Runner type. The built-in registry contains the deterministic Demo adapter, the first-party DSH adapter, and shell-free command adapters for Pi, Claude Code, Codex, and Hermes. Command adapters receive immutable `ExecutionSpec` values through explicit argument placeholders instead of shell interpolation.

`GET /v1/executions/{id}/events` exposes the durable per-execution event log as SSE. Each event has a monotonic sequence, attempt number, type, status, optional data, and timestamp. Reconnecting clients pass `Last-Event-ID` or `?after=` and replay persisted lifecycle and Harness progress before following live events.
