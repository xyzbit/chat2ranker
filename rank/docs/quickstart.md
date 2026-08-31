# Chat2Ranker in one minute

English | [中文](quickstart.zh.md)

This tutorial is for a first-time local Chat2Ranker user. At the end, you will have a persistent experiment and can prepare a dataset, select agents, run an evaluation, and inspect results through conversation.

## 1. Start

Install Node.js 22.19 or newer. Chat2Ranker supports macOS and Linux on x64 and arm64.

```sh
npx -y @xyzbit/chat2ranker@latest start
```

The first run downloads and verifies the prebuilt runtime for your platform, then opens the browser. Databases, model credentials, logs, and run artifacts stay under `~/.chat2ranker`, outside the npm cache.

## 2. Connect a model

The first screen asks for three values:

1. Choose a provider.
2. Choose a model.
3. Enter an API key.

Official providers supply their default Base URL and built-in pricing. After the connection passes validation, both the Control conversation agent and Judge can run. The Judge initially reuses the same connection and can be changed later in the experiment workspace.

## 3. Create your first experiment

Describe what you want to validate, for example:

> Build 10 Web research cases, compare my two existing agents, and show pass rate, cost, and failure reasons.

Rank asks for missing information or lets you choose existing datasets and agents. One conversation is one experiment, while dataset and agent configurations remain reusable, versioned assets.

The run card appears only when you ask to run. Review the dataset, agents, and repeat count, then select **Start run** or **Start comparison** in that card. The normal send button never starts an evaluation.

## 4. Inspect results

When a run finishes, the conversation shows pass rate, stable cases, cost, and stage status. Open **Full results** to inspect cases, Judge conclusions, execution logs, and artifacts in the experiment workspace. **Experiment performance** compares completed agent runs in the current experiment.

Cost uses provider-reported values first, then connection pricing, then the built-in official catalog. If none is available, the UI reports missing pricing configuration instead of inventing a value.

## Background operation and shutdown

```sh
npx -y @xyzbit/chat2ranker@latest start --detach
npx -y @xyzbit/chat2ranker@latest status
npx -y @xyzbit/chat2ranker@latest open
npx -y @xyzbit/chat2ranker@latest stop
```

For a foreground run, press `Ctrl-C` once to stop the complete service group. Starting again restores model connections, experiments, the Control Session, and run artifacts.

## Use an isolated profile

Test onboarding or temporary work without deleting existing data:

```sh
npx -y @xyzbit/chat2ranker@latest start --home /tmp/chat2ranker-clean
```

For source development, service ports, and full acceptance flows, see [local development and acceptance](local-acceptance.md).
