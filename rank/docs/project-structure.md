# Repository structure

This reference defines where chat2ranker source belongs. It complements the upstream DSH layout in [`AGENTS.md`](../../AGENTS.md).

```text
chat2ranker/
├── execution/
│   └── backend/
│       ├── cmd/executiond/            generic execution control service
│       ├── cmd/execution-worker/      one harness invocation payload
│       ├── client/                    versioned Go service client
│       ├── contract/                  public HTTP request and result types
│       ├── harness/                   public Adapter interface and registry
│       └── internal/                  execution domain, app, Repository, Executor implementations
├── apps/
│   ├── cli/ and web/                 upstream DSH applications
│   └── rank-web/                     Rank UI and Control DSH product assembly
├── packages/
│   ├── ...                           upstream DSH plugins
│   └── ...                           upstream DSH plugins consumed by rank-web
├── rank/
│   ├── backend/
│   │   ├── cmd/rankd/                product API and case scheduler process
│   │   └── internal/                 Rank domain, application, Repository, and adapters
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
- `rank/backend` owns business state and judging policy but does not import DSH internals or start harness processes.
- `execution/backend` owns generic execution state and runtime lifecycle but cannot import Rank domain packages.
- `execution/backend/contract` and `execution/backend/client` are the only Go packages shared from the execution plane into Rank.
- `execution/backend/harness` owns harness-specific launch and event normalization but cannot access the Rank database.
- `rank/api` owns every cross-process field and terminal state.
- Mutable local runtime data belongs under ignored `var/`, never under source or fixtures.

## Dependency direction

Control DSH calls the versioned Rank API. `rankd` depends on Rank Repository ports and the Execution Service client. `executiond` depends on its own Repository and Executor ports. `execution-worker` starts one selected Harness Adapter; it does not know whether the invocation is part of an experiment. Progress and results return through durable Execution events, then Rank maps them into Run events and Case Results. Native traces never update the browser or Rank database directly.
