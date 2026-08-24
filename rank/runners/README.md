# Runner adapters

English | [中文](README.zh.md)

Every supported harness is a peer Harness Adapter behind the versioned Execution Service contract.

An Adapter receives one immutable generic invocation, runs inside an isolated process or container, stores the native harness trace, and returns one normalized terminal result. An Adapter cannot read Rank business tables, infer experiment policy, or reuse the Control DSH process.

The public Go interface and registry live in `execution/backend/harness`. The built-in implementations are `mock` for deterministic cross-process tests and `dsh` for real DSH evaluation. Pi, Claude Code, Codex, and Hermes are registered through one shell-free command adapter and become available when their JSON argument array is configured. A native SDK adapter can replace a command adapter without changing Rank.
