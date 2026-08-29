# chat2ranker

[English](README.md) | 中文

面向 Agent Harness 的对话式评测平台。

chat2ranker 把一次对话整理为版本化的数据集与 Agent 配置，在隔离的 Harness 运行时中执行每个用例，并汇总通过率、成本、耗时、失败信息和原始执行日志。

本仓库是 [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) 的产品 Fork。DSH 承载控制对话、Skill 运行时和 A2UI；Go Rank 服务负责实验，并把 DSH、Pi、Claude Code、Codex、Hermes 及未来 Harness 作为平级 Runner 调度。

## 从这里开始

- [快速启动](rank/docs/local-acceptance.md)
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
- 可选的 Docker 或其他 Sandbox 执行器，用于远程或容器化 Runner 任务

DSH 开发命令继续由 [AGENTS.md](AGENTS.md) 维护。完整的本地 Rank 组合只需在仓库根目录执行一个命令。

## 运行

### 一条命令运行

正式发布后无需克隆源码：

```sh
npx -y @xyzbit/chat2ranker start
```

启动器会下载当前系统的预编译运行包、打开浏览器，并把数据库、凭据、日志和运行产物保存在 `~/.chat2ranker`。可使用 `--home <目录>` 隔离一套全新环境；`start --detach`、`status`、`open` 和 `stop` 管理后台实例。

### 从源码运行

```sh
pnpm install
pnpm rank:dev
```

打开 `http://127.0.0.1:4173`。首次使用只需选择 Provider、模型并填写 API Key；也可通过 `pnpm rank:dev -- --provider minimax --model MiniMax-M3 --api-key '...'` 在启动时初始化。Base URL 未填写时使用平台官方目录默认值。使用 `--home /tmp/chat2ranker-clean` 可验证全新初始化。按一次 `Ctrl-C` 即可一起停止全部服务。完整参数与验收步骤见[快速启动](rank/docs/local-acceptance.md)。

维护者运行 `pnpm rank:package` 会生成当前平台的 runtime 压缩包、SHA256 文件和可发布的 npm 包；脚本末尾会打印本机验收与正式发布命令。

DSH 上游 Web UI 仍可通过 `npx @deepseek-ai/dsh web` 独立运行，但它不是 Rank 产品组合。

## 同步上游

产品仓库使用 `origin` 指向 `xyzbit/chat2ranker`，使用 `upstream` 指向 `deepseek-ai/deepseek-harness`。

```sh
git fetch upstream
git merge upstream/master
```

尽量在 Rank 自有目录内解决产品组合冲突。保持 DSH 上游源码布局不变，让后续同步可审查。
