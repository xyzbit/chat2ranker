# Repository structure

This reference defines where chat2ranker source belongs. It complements the upstream DSH layout in [`AGENTS.md`](../../AGENTS.md).

```text
chat2ranker/
├── apps/
│   ├── cli/ and web/                 upstream DSH applications
│   └── rank-web/                     Control DSH product assembly
├── packages/
│   ├── ...                           upstream DSH plugins
│   ├── rank/                         Rank-only DSH extensions
│   │   ├── protocol/                 generated TypeScript API client and event types
│   │   ├── control-host/             Rank tools, Remote methods, and Go API bridge
│   │   └── client-ui/                conversation UI and A2UI projections
│   └── bundle/rank-control/          Control DSH plugin composition
├── rank/
│   ├── api/                          versioned Control and Runner wire definitions
│   ├── backend/
│   │   ├── cmd/rankd/                product API and scheduler process
│   │   ├── cmd/rank-worker/          Sandbox worker process
│   │   └── internal/                 domain, application, ports, and adapters
│   ├── runners/
│   │   ├── mock/                     deterministic protocol implementation
│   │   └── dsh/                      isolated DSH harness implementation
│   ├── assets/                       Control Skill, presets, and Judge defaults
│   ├── deploy/                       Compose, container, and cluster manifests
│   ├── tests/                        contract and end-to-end verification
│   └── docs/                         product architecture and decisions
└── var/                              ignored local sessions, artifacts, and sandboxes
```

## Ownership rules

- `apps/rank-web` assembles plugins but does not own experiment business logic.
- `packages/rank` integrates with DSH but does not execute tested harnesses.
- `rank/backend` owns business state but does not import DSH internals.
- `rank/runners/<harness>` owns harness-specific launch and event normalization but cannot access the Rank database.
- `rank/api` owns every cross-process field and terminal state.
- Mutable local runtime data belongs under ignored `var/`, never under source or fixtures.

## Dependency direction

Control DSH calls the versioned Rank API. `rankd` depends on domain interfaces and dispatches work through the queue or local executor. `rank-worker` starts the selected Runner. A Runner depends only on the Runner protocol and its harness. Results and native traces flow back through the worker; they never bypass Rank persistence to update the browser directly.
