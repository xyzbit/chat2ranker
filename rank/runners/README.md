# Runner adapters

English | [中文](README.zh.md)

Every supported harness is a peer Runner implementation behind one versioned execution protocol.

A Runner receives one immutable case specification, runs inside an isolated process or container, emits normalized ordered events, stores the native harness trace, and returns one terminal result. A Runner cannot read Rank business tables or reuse the Control DSH process.

The first implementations are `mock` for deterministic cross-process tests and `dsh` for real DSH evaluation. Pi, Claude Code, Codex, Hermes, and other adapters are added only with an executable consumer and conformance tests.
