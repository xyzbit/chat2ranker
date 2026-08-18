# Rank backend

English | [中文](README.zh.md)

This Go module produces two binaries:

- `rankd`: Rank API, version freezing, scheduling, result aggregation, and business persistence.
- `rank-worker`: one isolated candidate or Judge execution from a versioned JSON request, with bounded output, process-tree cancellation, artifact capture, and cleanup.

The backend depends on Runner and storage interfaces. It does not import DSH implementation packages and does not call `ctx.agents.create()`.

`rankd` starts a fresh `rank-worker` process for every candidate and Judge execution. Runner processes receive only an immutable execution specification, a private workspace, a private Harness home, and a reduced environment. They do not access the Rank database. The local first version uses child-process isolation; the same JSON protocol can later be transported through containers or Kubernetes Jobs without changing the Rank domain model.
