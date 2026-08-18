# Repository structure

This reference defines where chat2ranker source belongs. It complements the upstream DSH layout in [`AGENTS.md`](../../AGENTS.md).

```text
chat2ranker/
├── apps/
│   ├── cli/ and web/                 upstream DSH applications
│   └── rank-web/                     Rank UI and Control DSH product assembly
├── packages/
│   ├── ...                           upstream DSH plugins
│   └── ...                           upstream DSH plugins consumed by rank-web
├── rank/
│   ├── backend/
│   │   ├── cmd/rankd/                product API and scheduler process
│   │   ├── cmd/rank-worker/          Sandbox worker process
│   │   └── internal/                 domain, application, ports, and adapters
│   ├── runners/                      Runner deployment and adapter documentation
│   ├── assets/                       Control Skill, presets, and Judge defaults
│   ├── deploy/                       Compose, container, and cluster manifests
│   ├── tests/                        contract and end-to-end verification
│   └── docs/                         product architecture and decisions
│   └── var/                          ignored SQLite, sessions, artifacts, binaries, and sandboxes
└── scripts/rank-dev.mjs              one-command local process assembly
```

## Ownership rules

- `apps/rank-web` owns presentation and the Rank-specific Control DSH plugin but does not own experiment business logic or tested execution.
- `rank/backend` owns business state but does not import DSH internals.
- `rank/runners/<harness>` owns harness-specific launch and event normalization but cannot access the Rank database.
- `rank/api` owns every cross-process field and terminal state.
- Mutable local runtime data belongs under ignored `var/`, never under source or fixtures.

## Dependency direction

Control DSH calls the versioned Rank API. `rankd` depends on domain interfaces and dispatches work through the local process executor. `rank-worker` starts the selected Runner and an independent Judge. A Runner depends only on the Runner protocol and its harness. Results and native traces flow back through the worker; they never bypass Rank persistence to update the browser directly.
