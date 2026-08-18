# Rank product source

English | [中文](README.zh.md)

The `rank/` tree contains the product control plane and execution-plane integration. It does not contain a second copy of DSH.

| Directory | Owner | Responsibility |
|---|---|---|
| `api/` | Rank protocol | Control API and Runner wire definitions |
| `backend/` | Go Rank service | Experiments, immutable versions, scheduling, aggregation, cancellation, and persistence |
| `runners/` | Runner adapters | One isolated adapter and image per supported harness |
| `assets/` | Product configuration | Control Skill and preset source assets; versioned Judge defaults |
| `deploy/` | Operations | Local Compose and production deployment manifests |
| `tests/` | Cross-process verification | Protocol, integration, and end-to-end tests |
| `docs/` | Product architecture | Current system map, data flow, structure, and decisions |

The runtime has three process roles:

1. Control DSH hosts the user conversation, Skill, and A2UI.
2. `rankd` owns product state and dispatches immutable execution specifications.
3. `rank-worker` starts a Sandbox containing one selected Runner for each case.

The tested DSH runtime is a Runner instance inside a Sandbox. It never shares a Cordis context, Session store, DSH home, workspace, environment, or process with Control DSH.
