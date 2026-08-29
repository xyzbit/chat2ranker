# chat2ranker

English | [中文](README.zh.md)

Conversation-first evaluation for agent harnesses.

chat2ranker turns a conversation into a versioned dataset and agent configuration, executes every case in an isolated harness runtime, and reports pass rate, cost, duration, failures, and raw traces.

The repository is a product fork of [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness). DSH hosts the control conversation, Skill runtime, and A2UI. The Go Rank service owns experiments and dispatches DSH, Pi, Claude Code, Codex, Hermes, and future harnesses as peer Runner implementations.

## Start here

- [Quick start](rank/docs/local-acceptance.md)
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
- Optional Docker or another Sandbox executor for remote or containerized Runner jobs

DSH development commands remain documented in [AGENTS.md](AGENTS.md). The complete local Rank assembly uses one repository-root command.

## Run

### One-command install

After the first public release, no source checkout is required:

```sh
npx -y @xyzbit/chat2ranker start
```

The launcher downloads the prebuilt runtime for the current platform, opens the browser, and keeps databases, credentials, logs, and artifacts under `~/.chat2ranker`. Use `--home <path>` for an isolated clean profile; `start --detach`, `status`, `open`, and `stop` manage a background instance.

### Run from source

```sh
pnpm install
pnpm rank:dev
```

Open `http://127.0.0.1:4173`. On first use, choose a provider and model and enter an API key; Rank creates persistent Control and Judge bindings. You can also initialize them at startup with `pnpm rank:dev -- --provider minimax --model MiniMax-M3 --api-key '...'`, or use `--home /tmp/chat2ranker-clean` to test a clean profile. See the [quick start](rank/docs/local-acceptance.md) for all options.

Maintainers can run `pnpm rank:package` to produce the current platform runtime archive, its SHA256 file, and the publishable npm package. The script prints local acceptance and release commands when it finishes.

The upstream DSH Web UI remains independently available through `npx @deepseek-ai/dsh web`; it is not the Rank product assembly.

## Upstream synchronization

The product repository uses `origin` for `xyzbit/chat2ranker` and `upstream` for `deepseek-ai/deepseek-harness`.

```sh
git fetch upstream
git merge upstream/master
```

Resolve product assembly conflicts inside Rank-owned directories where possible. Keep the upstream source layout intact so future synchronization remains reviewable.
