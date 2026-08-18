# chat2ranker

English | [中文](README.zh.md)

Conversation-first evaluation for agent harnesses.

chat2ranker turns a conversation into a versioned dataset and agent configuration, executes every case in an isolated harness runtime, and reports pass rate, cost, duration, failures, and raw traces.

The repository is a product fork of [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness). DSH hosts the control conversation, Skill runtime, and A2UI. The Go Rank service owns experiments and dispatches DSH, Pi, Claude Code, Codex, Hermes, and future harnesses as peer Runner implementations.

## Start here

- [Product source](rank/README.md)
- [System architecture](rank/docs/architecture.md)
- [Core data flow](rank/docs/core-data-flow.md)
- [Repository structure](rank/docs/project-structure.md)
- [Architecture decisions](rank/docs/adr/0001-control-and-execution-separation.md)

## Repository ownership

- Upstream DSH source remains under its existing `apps/`, `packages/`, `python/`, `native/`, and `vendor/` trees.
- Rank-specific backend, Runner, deployment, test, and product documentation live under `rank/`.
- Rank-specific DSH packages will live under `packages/rank/`; the Control DSH product assembly will live under `apps/rank-web/`.
- Product behavior extends DSH through plugins and profiles. Changes to DSH core packages require a missing extension point to be documented first.

## Development prerequisites

- Node.js `^22.19.0 || >=24.0.0`
- pnpm `11.7.0`
- Go `1.24`
- Docker or another supported Sandbox executor for real Runner jobs

DSH development commands remain documented in [AGENTS.md](AGENTS.md). Rank commands will be added under `rank/` as the executable services are implemented.

## Run

The upstream DSH Web UI remains available while the Rank assembly is under development:

```sh
npx @deepseek-ai/dsh web
```

### Run from source

```sh
pnpm install
pnpm run build
pnpm dsh web
```

The Rank-specific development command will replace this entry point after `apps/rank-web` becomes executable.

## Upstream synchronization

The product repository uses `origin` for `xyzbit/chat2ranker` and `upstream` for `deepseek-ai/deepseek-harness`.

```sh
git fetch upstream
git merge upstream/master
```

Resolve product assembly conflicts inside Rank-owned directories where possible. Keep the upstream source layout intact so future synchronization remains reviewable.
