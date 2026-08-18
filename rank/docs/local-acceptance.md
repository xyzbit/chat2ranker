# Local acceptance

This tutorial starts the complete local Rank product and verifies the first delivery without provider credentials.

## Prerequisites

- Node.js `^22.19` or `>=24`
- pnpm `11.7.0`
- Go `1.24`

From the repository root, install dependencies once:

```sh
cd /Users/staff/code/ai/chat2ranker
pnpm install
```

## Start the product

```sh
pnpm rank:dev
```

The command builds missing DSH libraries and both Go binaries, then owns all three long-running services:

| URL or process | Responsibility |
|---|---|
| `http://127.0.0.1:4173` | Rank conversation UI |
| `http://127.0.0.1:8788/control/v1/health` | Persistent Control DSH |
| `http://127.0.0.1:8787/api/health` | Go `rankd` control plane |
| `rank-worker` children | Isolated candidate and Judge executions |

Press `Ctrl-C` once in the owning terminal to stop the complete process group. Local state remains under the ignored `rank/var/` directory.

## Keyless acceptance

1. Open `http://127.0.0.1:4173`.
2. Send an ordinary message and confirm that it does not start a run.
3. Select `Web 研究基准集 v3` and `@demo/research v1`.
4. Start the run only from the inline confirmation card.
5. Confirm `10/12`, `83%`, a known cost, and two failed cases.
6. Open a failed case and select `查看轨迹`; the UI reads a retained worker artifact through `rankd`.
7. Start a second run in the same conversation and confirm that both runs remain visible.
8. Restart `pnpm rank:dev`, reopen the experiment, and confirm that Rank state and the Control DSH Session resume.

The Demo Runner is deterministic but still crosses the production process and persistence boundaries: `rankd` starts a separate worker for every candidate and another separate worker for every Judge. It does not prove a provider API credential.

## Real DSH acceptance

Set the provider credential before starting the same command:

```sh
export DEEPSEEK_API_KEY='replace-with-your-key'
export RANK_JUDGE_RUNNER=dsh
pnpm rank:dev
```

`@dsh/research` becomes selectable. Every case and Judge receives a different workspace and DSH home. The DSH home, stdout, stderr, request, result, and trace remain under `rank/var/artifacts/<run>/<case>/<execution>/`.

## Other harnesses

Pi, Claude Code, Codex, and Hermes use the same process protocol. Configure one shell-free JSON argument array with placeholders before startup:

```sh
export RANK_RUNNER_CODEX_ARGV='["your-codex-executable","your-noninteractive-flag","{prompt}"]'
```

Supported placeholders are `{prompt}`, `{workspace}`, `{harnessHome}`, and `{model}`. The executable receives a reduced environment and runs with the workspace as its current directory. Rank marks an adapter unavailable until its corresponding `RANK_RUNNER_<TYPE>_ARGV` value exists.

## Automated verification

```sh
pnpm rank:test
pnpm rank:build
```

`rank:test` covers repository transactions, restart recovery, Control DSH Session resume, tool execution, candidate/Judge process isolation, artifact access, cost aggregation, and cancellation. `rank:build` builds both DSH library faces, both Go binaries, and the Rank Web production bundle.
