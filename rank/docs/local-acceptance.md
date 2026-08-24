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

The command builds missing DSH libraries and three Go binaries, then owns all four long-running services:

| URL or process | Responsibility |
|---|---|
| `http://127.0.0.1:4173` | Rank conversation UI |
| `http://127.0.0.1:8788/control/v1/health` | Persistent Control DSH |
| `http://127.0.0.1:8787/api/health` | Go `rankd` control plane |
| `http://127.0.0.1:8790/v1/health` | Generic Go `executiond` control plane |
| `execution-worker` children | Isolated candidate and Judge invocations |

Press `Ctrl-C` once in the owning terminal to stop the complete process group. Local state remains under the ignored `rank/var/` directory.

## Keyless acceptance

1. Open `http://127.0.0.1:4173`.
2. Send an ordinary message and confirm that it does not start a run.
3. Select `Web 研究基准集 v3` and `@demo/research v1`.
4. Start the run only from the inline confirmation card.
5. Keep `可靠 · 5 次`, start the Run, and confirm `50/60` valid Trials, `83%`, `10/12` reliable cases, a known cost, and two unstable cases.
6. Open a failed case and select `查看轨迹`; the UI reads a retained worker artifact through `rankd`.
7. Open `运行日志` and confirm that Agent and Judge lifecycle/output events appear without refreshing the page.
8. Start a second run in the same conversation and confirm that both runs remain visible.
9. Restart `pnpm rank:dev`, reopen the experiment, and confirm that Rank state and the Control DSH Session resume.

The Demo Harness is deterministic but still crosses the production HTTP, process, and persistence boundaries: the default dataset creates 60 candidate Executions and 60 independent Judge Executions through `executiond`. It does not prove a provider API credential.

## Real DSH acceptance

Set the provider credential before starting the same command:

```sh
export DEEPSEEK_API_KEY='replace-with-your-key'
export RANK_JUDGE_RUNNER=dsh
pnpm rank:dev
```

`@dsh/research` becomes selectable. Every case and Judge receives a different workspace and DSH home. The DSH home, stdout, stderr, request, result, and trace remain under `rank/var/artifacts/<execution>/`.

## Other harnesses

Pi, Claude Code, Codex, and Hermes use the same process protocol. Configure one shell-free JSON argument array with placeholders before startup:

```sh
export EXECUTION_HARNESS_CODEX_ARGV='["your-codex-executable","your-noninteractive-flag","{prompt}"]'
```

Supported placeholders are `{prompt}`, `{workspace}`, `{harnessHome}`, `{model}`, `{preset}`, `{systemPrompt}`, `{toolsJson}`, and `{skillsJson}`. The executable receives a reduced environment and runs with the workspace as its current directory. Execution Service validates every configured argument array at startup and marks an unconfigured external adapter unavailable.

The DSH adapter uses the repository's built headless CLI and writes an invocation-local patch under the private Harness Home. That patch applies the frozen model and system prompt without mutating the shared DSH source tree or Control DSH profile.

## Automated verification

```sh
pnpm rank:test
pnpm rank:build
```

`rank:test` covers the Rank and Execution SQLite Repository adapters, Evaluator freezing, deterministic-first gating, Case × Trial aggregation, invalid-Trial denominators, pass^3, execution idempotency, durable SSE replay, restart recovery, Control DSH Session resume, candidate/Judge process isolation, artifact access, cost aggregation, and cancellation. `rank:build` builds both DSH library faces, three Go binaries, and the Rank Web production bundle.
