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
| `docs/` | Product architecture | Current system map, data flow, structure, diagrams, and decisions |

The product runtime has four process roles:

1. Control DSH hosts the user conversation, Skill, and A2UI.
2. `rankd` owns product state and submits immutable candidate and Judge specifications.
3. `executiond` owns generic execution state, durable progress events, Executor selection, cancellation, and artifacts.
4. `execution-worker` resolves one public Harness Adapter and starts it inside an isolated local process or Sandbox.

The tested DSH runtime is a Runner instance inside a Sandbox. It never shares a Cordis context, Session store, DSH home, workspace, environment, or process with Control DSH.

The local [development launcher](../scripts/rank-dev.mjs) owns the service and Vite child processes and stops them together. Vite binds strictly to `127.0.0.1:4173`; an occupied port fails startup instead of selecting a different URL. Keep the launcher running while using the console.

The operator console provides model connections and defaults, Agent version comparison and evaluation evidence, deployment creation and release history, request details, API credential revocation, and failed Webhook retry. The optional Rankling companion opens the same conversation from management pages and accepts browser-local Codex v1/v2 pet imports. `pnpm --filter @xyzbit/chat2ranker-web test:operator-live` explicitly runs paid acceptance against a running local stack using a verified stored DeepSeek connection and Judge binding; it retains labeled test records and revokes its issued serving key. No provider key is passed to the script or conversation.

The [serving release registry](../.agents/notes/implemented/architecture/2026-09-08-serving-release-registry-foundation.md), [Responses serving MVP](../.agents/notes/implemented/feature/2026-09-09-responses-serving-mvp.md), [stateful continuation](../.agents/notes/implemented/feature/2026-09-09-stateful-response-continuation.md), [strict structured output](../.agents/notes/implemented/feature/2026-09-09-structured-response-output.md), [frozen rich input](../.agents/notes/implemented/feature/2026-09-09-frozen-response-input.md), [caller function round trip](../.agents/notes/implemented/feature/2026-09-09-caller-function-round-trip.md), and [signed Webhook outbox](../.agents/notes/implemented/feature/2026-09-09-signed-webhook-outbox.md) expose governed text, image, file, function, approval, and asynchronous event interaction without exposing Rank Runs or the private Execution Service. The web control plane keeps the Agent conversation primary, renders release comparison and confirmation as A2UI, and provides a bilingual Deployment page for deterministic inspection, manual release, and Webhook endpoint management. The [Agent Serving API reference](docs/serving-api.md) defines the authenticated `/v1/responses` subset, Deployment references, durable events, version-pinned Conversation continuation, rich input, strict output, function calls, approval, signed Webhooks, limits, and local-development constraints; the broader [Agent lifecycle and serving proposal](../.agents/notes/proposed/feature/2026-09-08-agent-lifecycle-serving.md) owns later release-automation and ecosystem phases.
