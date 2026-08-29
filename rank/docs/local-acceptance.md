# Quick start and local acceptance

This tutorial starts the complete local Rank product either from the published launcher or from source. Both paths own the same four-service lifecycle.

## 用户一条命令启动

正式发布后，用户只需要 Node.js：

```sh
npx -y @xyzbit/chat2ranker start
```

首次运行会下载匹配当前系统和 CPU 的预编译 runtime，校验 SHA256，保存到 `~/.chat2ranker/runtime/`，启动服务并打开浏览器。数据库、凭据、日志和产物也都位于 `~/.chat2ranker/`，npm 的临时缓存只保存启动器，不承载用户数据。

```sh
npx -y @xyzbit/chat2ranker start --detach
npx -y @xyzbit/chat2ranker status
npx -y @xyzbit/chat2ranker open
npx -y @xyzbit/chat2ranker stop
```

使用 `--home /tmp/chat2ranker-clean` 可隔离一套全新初始化环境，不需要删除已有模型连接或实验数据。

## Prerequisites

- Node.js `^22.19` or `>=24`
- pnpm `11.7.0`
- Go `1.24`

Install dependencies once:

```sh
git clone https://github.com/xyzbit/chat2ranker.git
cd chat2ranker
pnpm install
```

## 30 秒启动

启动完整产品：

```sh
pnpm rank:dev
```

Open `http://127.0.0.1:4173`. Press `Ctrl-C` once in the same terminal to stop the complete process group. Local databases, Session history, and artifacts remain under the ignored `rank/var/` directory. Source runs also accept `--home /tmp/chat2ranker-clean`.

首次启动不需要先创建配置文件。浏览器只询问 Provider、模型和 API Key；Base URL 与默认价格来自内置官方目录。连接验证通过后，Rank 会创建两个系统绑定：

- `Control`：Rank 对话 Agent；
- `Judge`：LLM-as-a-Judge。首次默认复用 Control 的连接和模型，之后可独立切换。

也可以在启动时直接完成初始化：

```sh
pnpm rank:dev -- --provider minimax --model MiniMax-M3 --api-key 'replace-with-your-key'
```

`--base-url` 可省略；官方 Provider 会使用平台目录中的默认地址。`deepseek`、`minimax`、`glm`、`openai`、`anthropic` 和 `kimi` 均可直接选择。已有系统绑定会被复用，启动参数不会覆盖用户后来在 UI 或对话中的修改；只有显式添加 `--reconfigure` 才会重新绑定。

需要单独指定 Judge 时，再增加 `--judge-provider`、`--judge-model`、`--judge-api-key`，以及可选的 `--judge-base-url`。服务部署推荐用同名环境变量 `RANK_CONTROL_PROVIDER`、`RANK_CONTROL_MODEL`、`RANK_CONTROL_API_KEY`、`RANK_JUDGE_PROVIDER`、`RANK_JUDGE_MODEL`、`RANK_JUDGE_API_KEY` 或 Secret 管理器，避免明文进入 Shell History。不要提交凭据。

## How the single command works

`pnpm rank:dev` runs [`scripts/rank-dev.mjs`](../../scripts/rank-dev.mjs) as one Node.js parent process. The parent builds missing DSH libraries, compiles the three Go executables, creates one shared set of service URLs and local secrets, and then starts the Node.js and Go children with `spawn()`.

The startup order is deliberate: `executiond` starts and passes its health check before `rankd`; the Control DSH Host and Vite UI start after the backend addresses are known.

The parent owns these long-running processes:

| URL or process | Responsibility |
|---|---|
| `http://127.0.0.1:4173` | Rank conversation UI |
| `http://127.0.0.1:8788/control/v1/health` | Persistent Control DSH |
| `http://127.0.0.1:8787/api/health` | Go `rankd` control plane |
| `http://127.0.0.1:8790/v1/health` | Generic Go `executiond` control plane |
| `execution-worker` children | Isolated candidate and Judge invocations |

The parent tracks every child. `Ctrl-C`, termination of the parent, or an unexpected child exit triggers coordinated shutdown, so one terminal owns the whole local assembly even though the children use different languages.

## Choose a startup method

| Method | Use it for | Current status |
|---|---|---|
| `npx @xyzbit/chat2ranker start` | Local users who should not clone or compile the source | Implemented release path |
| `pnpm rank:dev` | Contributors and source debugging | Implemented development path |
| Individual service commands | Debugging one service or replacing part of the assembly | Supported by the binaries, but intentionally not the quick path |
| `pnpm rank:package` | Maintainer release packaging | Produces a runtime archive, SHA256, and npm package |
| Docker Compose | Reproducible server installation | Deployment option to package next |
| macOS/Windows desktop bundle | Non-technical local users | Distribution option after the server lifecycle and upgrades are stable |

The npm package is intentionally a zero-dependency thin launcher. Platform binaries, DSH, and static Web assets live in versioned GitHub Release archives, so `npx` does not resolve the full Harness dependency graph on every clean machine. Upgrading the launcher or runtime never rewrites `~/.chat2ranker` data.

## Maintainer packaging

Build the current platform release without publishing anything:

```sh
pnpm rank:package
```

Outputs are written to `dist/chat2ranker/`. The command prints a local `npx --package <tgz> chat2ranker ... --runtime-archive <tar.gz>` acceptance command plus explicit `gh release create` and `npm publish` commands. Publishing remains a separate, deliberate action.

## Verify service health

```sh
curl http://127.0.0.1:8787/api/health
curl http://127.0.0.1:8788/control/v1/health
curl http://127.0.0.1:8790/v1/health
```

## 首次启动验收

1. Open `http://127.0.0.1:4173`.
2. 未配置系统模型时，应只看到一次精简初始化；选择 Provider 和模型并填写 API Key。
3. 验证通过后进入正常实验对话；普通消息不会启动运行。
4. 选择测试集和 Agent，只从内联运行确认卡片开始运行。
5. 若测试集只有精确匹配、包含、正则或 JSON 校验等确定性规则，运行不调用 Judge；存在 Rubric 时才调用系统 Judge。
6. 在真正调用 Candidate 前，系统应先完成 Judge 连接预检；缺失时直接阻止运行，避免产生只有 Candidate、没有评分的费用。
7. 重启同一命令，确认模型绑定、Rank 状态、Control DSH Session 和执行产物均可恢复。

## Real DSH acceptance

`@dsh/research` becomes selectable. Candidate 使用 Agent 自己冻结的模型连接；Judge 使用全局 Judge 绑定。每个 Case 和 Judge 都获得不同的 workspace 与 DSH home。DSH home、stdout、stderr、request、result 和 trace 保存在 `rank/var/artifacts/<execution>/`。

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
pnpm rank:package
```

`rank:test` covers the Rank and Execution SQLite Repository adapters, Evaluator freezing, deterministic-first gating, Case × Trial aggregation, invalid-Trial denominators, pass^3, execution idempotency, durable SSE replay, restart recovery, Control DSH Session resume, candidate/Judge process isolation, artifact access, cost aggregation, and cancellation. `rank:build` builds both DSH library faces, three Go binaries, and the Rank Web production bundle.
