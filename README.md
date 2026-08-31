# Chat2Ranker

[![npm](https://img.shields.io/npm/v/@xyzbit/chat2ranker?color=5454e8)](https://www.npmjs.com/package/@xyzbit/chat2ranker) [![Release](https://img.shields.io/github/v/release/xyzbit/chat2ranker)](https://github.com/xyzbit/chat2ranker/releases/latest) [![CI](https://github.com/xyzbit/chat2ranker/actions/workflows/ci.yml/badge.svg)](https://github.com/xyzbit/chat2ranker/actions/workflows/ci.yml) [![License](https://img.shields.io/github/license/xyzbit/chat2ranker)](LICENSE)

English | [中文](README.zh.md)

Chat2Ranker turns a goal into a versioned dataset, comparable Agent runs, and evidence you can inspect. No evaluation script is required up front.

## What can you compare?

- **New models:** keep the dataset, Harness, tools, and judging rules fixed to compare model versions on pass rate, cost, duration, and stability.
- **Harnesses:** run the same model and cases through DeepSeek Harness, Codex, Claude Code, or Hermes to measure runtime differences.
- **Agent configurations:** freely combine models, system prompts, tools, and Skills, then compare the frozen Agent versions in one experiment.
- **Regressions:** repeat trials before a release, find unstable cases, and inspect failures and execution traces.

## See one experiment end to end

[![Create, run, and inspect a Chat2Ranker experiment](rank/docs/assets/chat2ranker-demo-poster.jpg)](rank/docs/assets/chat2ranker-demo.mp4)

Describe one goal, let Rank prepare reusable data and Agents, confirm the run, compare quality against cost or duration, and open the side workspace only when you need details. Click the image to play the 1080p video.

<a id="run"></a>

## Start in one minute

Requires Node.js 22.19 or newer on macOS or Linux, x64 or arm64.

```sh
npx -y @xyzbit/chat2ranker@latest start
```

On first open, choose a provider and model, then enter an API key. Describe the outcome you want, such as “Build a Web research dataset and compare two agents on accuracy and cost.” Rank asks only for missing information and waits for explicit confirmation in the run card.

See the [one-minute quick start](rank/docs/quickstart.md) for first-run setup, background operation, and isolated data directories.

## From conversation to comparable results

- **Prepare:** create or select versioned datasets and agent configurations through conversation.
- **Run:** isolate every trial and compare multiple agent versions in one run group.
- **Evaluate:** prefer deterministic checks and invoke rubric-based LLM judging only when semantic evaluation is needed.
- **Inspect:** aggregate pass rate, stable cases, cost, duration, failures, and raw execution artifacts.

## Harnesses and architecture

[DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) is the first-party runtime and control-conversation foundation. Pi, Claude Code, Codex, and Hermes connect through peer Runner adapters; an uninstalled or unconfigured Runner is not presented as runnable.

The Go Rank service owns experiments, frozen versions, scheduling, and aggregation. The Execution Service runs candidates and judges in isolated processes or sandboxes. Read the [system architecture](rank/docs/architecture.md) and [core data flow](rank/docs/core-data-flow.md) for details.

## Run from source

```sh
git clone https://github.com/xyzbit/chat2ranker.git
cd chat2ranker
pnpm install
pnpm rank:dev
```

See [local development and acceptance](rank/docs/local-acceptance.md) for source prerequisites, health checks, and acceptance flows. See the [repository structure](rank/docs/project-structure.md) for code ownership.

## Contributing

Issues and pull requests are welcome. If Chat2Ranker is useful to you, consider starring the repository.

Released under the [MIT License](LICENSE).
