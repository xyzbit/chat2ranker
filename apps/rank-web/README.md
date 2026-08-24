# Rank Web

English | [中文](README.zh.md)

This directory contains the Control DSH product assembly and conversation-first UI for chat2ranker.

The application owns only the browser entry point and DSH plugin composition. Experiment state, scheduling, results, and Runner lifecycle belong to the Go Rank service under [`rank/backend/`](../../rank/backend/README.md).

The assembly mounts the Rank Control Host plugin, a persistent experiment Session, and the Rank experiment Skill. The Session can create or select immutable dataset and Agent versions through tools. The React UI renders the chat, A2UI preparation and confirmation cards, live SSE progress, run summaries, failures, and scoped artifacts. `rankd` submits tested agents to the independent Execution Service; they never run inside the Control DSH process.

Run the complete local assembly from the repository root with `pnpm rank:dev`. See [`rank/docs/local-acceptance.md`](../../rank/docs/local-acceptance.md) for keyless and real-provider acceptance flows.
