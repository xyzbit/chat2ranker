# chat2ranker

[English](README.md) | 中文

面向 Agent Harness 的对话式评测平台。

chat2ranker 把一次对话整理为版本化的数据集与 Agent 配置，在隔离的 Harness 运行时中执行每个用例，并汇总通过率、成本、耗时、失败信息和原始执行日志。

本仓库是 [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) 的产品 Fork。DSH 承载控制对话、Skill 运行时和 A2UI；Go Rank 服务负责实验，并把 DSH、Pi、Claude Code、Codex、Hermes 及未来 Harness 作为平级 Runner 调度。

## 从这里开始

- [产品源码](rank/README.md)
- [系统架构](rank/docs/architecture.md)
- [核心数据流](rank/docs/core-data-flow.md)
- [仓库结构](rank/docs/project-structure.md)
- [架构决策](rank/docs/adr/0001-control-and-execution-separation.md)

## 目录所有权

- DSH 上游源码保持在原有 `apps/`、`packages/`、`python/`、`native/` 和 `vendor/` 目录中。
- Rank 后端、Runner、部署、测试和产品文档位于 `rank/`。
- Rank 专属 DSH 包放在 `packages/rank/`；Control DSH 产品组合放在 `apps/rank-web/`。
- 产品行为通过插件和 Profile 扩展 DSH。只有确认缺少扩展点后，才修改 DSH 核心包。

## 开发环境

- Node.js `^22.19.0 || >=24.0.0`
- pnpm `11.7.0`
- Go `1.24`
- Docker 或其他受支持的 Sandbox 执行器，用于真实 Runner 任务

DSH 开发命令继续由 [AGENTS.md](AGENTS.md) 维护。可执行服务落地后，Rank 命令统一放在 `rank/` 下。

## 运行

Rank 组合开发期间仍可运行 DSH 上游 Web UI：

```sh
npx @deepseek-ai/dsh web
```

### 从源码运行

```sh
pnpm install
pnpm run build
pnpm dsh web
```

`apps/rank-web` 具备可执行入口后，此处将替换为 Rank 专属开发命令。

## 同步上游

产品仓库使用 `origin` 指向 `xyzbit/chat2ranker`，使用 `upstream` 指向 `deepseek-ai/deepseek-harness`。

```sh
git fetch upstream
git merge upstream/master
```

尽量在 Rank 自有目录内解决产品组合冲突。保持 DSH 上游源码布局不变，让后续同步可审查。
