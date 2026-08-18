# Rank backend

English | [中文](README.zh.md)

This Go module will produce two binaries:

- `rankd`: Rank API, version freezing, scheduling, result aggregation, and business persistence.
- `rank-worker`: job leasing, Sandbox lifecycle, Runner invocation, event forwarding, and cleanup.

The backend depends on Runner and storage interfaces. It does not import DSH implementation packages and does not call `ctx.agents.create()`.

Runner processes receive only an immutable execution specification and scoped credentials. They do not access the Rank database.
