# Rank product source

English | [中文](README.zh.md)

The `rank/` tree contains the product control plane and execution-plane integration. It does not contain a second copy of DSH.

| Directory | Owner | Responsibility |
|---|---|---|
| `api/` | Rank protocol | Control and product API definitions |
| `backend/` | Go Rank service | Experiments, immutable versions, case scheduling, judging, aggregation, and persistence |
| `runners/` | Rank integration | Harness presets and execution-plane configuration |
| `assets/` | Product configuration | Control Skill and preset source assets; versioned Judge defaults |
| `deploy/` | Operations | Local Compose and production deployment manifests |
| `tests/` | Cross-process verification | Protocol, integration, and end-to-end tests |
| `docs/` | Product architecture | Current system map, data flow, structure, and decisions |

The product runtime has four process roles:

1. Control DSH hosts the user conversation, Skill, and A2UI.
2. `rankd` owns product state and submits immutable candidate and Judge specifications.
3. `executiond` owns generic execution state, durable progress events, Executor selection, cancellation, and artifacts.
4. `execution-worker` resolves one public Harness Adapter and starts it inside an isolated local process or Sandbox.

The tested DSH runtime is a Runner instance inside a Sandbox. It never shares a Cordis context, Session store, DSH home, workspace, environment, or process with Control DSH.
