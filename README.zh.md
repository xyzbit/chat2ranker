# AgentKiln

[![Release](https://img.shields.io/github/v/release/xyzbit/agentkiln)](https://github.com/xyzbit/agentkiln/releases/latest) [![CI](https://github.com/xyzbit/agentkiln/actions/workflows/ci.yml/badge.svg)](https://github.com/xyzbit/agentkiln/actions/workflows/ci.yml) [![License](https://img.shields.io/github/license/xyzbit/agentkiln)](LICENSE)

[English](README.md) | 中文

AgentKiln 是一个 Agent 开发与交付平台：创建 Agent，直接对话试用，用评测比较版本，发布为端点，再接入自己的应用。实验负责验证质量，是这条工作流中的一环。

模型、系统指令、工具和 Skill 随 Agent 版本一起固定；试用、评测与发布围绕同一个版本展开，便于复现行为、定位问题和决定发布哪个版本。

## 66 秒看完核心流程

[![AgentKiln：创建、试用、测评对比、发布与接入](rank/docs/assets/agentkiln-lifecycle-poster.jpg)](rank/docs/assets/agentkiln-lifecycle.mp4)

点击封面观看 1080p 视频。视频采用实际产品界面和中文短字幕，无旁白。对比画面中的“演示数据”来自确定性 Demo Runner，用于展示操作流程，不代表模型基准成绩。

## 从创建到接入

### 1. 创建 Agent

选择运行方式与模型，配置系统指令、工具和 Skill，保存为可追溯的 Agent 版本。也可以通过对话描述需求，让 AgentKiln 准备配置。

![创建 Agent](rank/docs/assets/lifecycle/01-create.jpg)

### 2. 直接对话试用

从 Agent 详情打开试用面板，发起多轮对话、查看执行详情，重新打开时继续已有记录。试用会话独立保存，无需先创建测试集或运行实验。详见[Agent 试用](rank/docs/agent-playground.md)。

![Agent 对话试用](rank/docs/assets/lifecycle/02-trial.jpg)

### 3. 测评与对比

选定同一测试集，比较多个 Agent 版本的通过率、成本和耗时；再进入具体用例，检查输出、评分和执行证据。一个对比组为每个 Agent 版本创建独立 Run。

![多 Agent 测评对比，示例数据](rank/docs/assets/lifecycle/03-comparison.jpg)

### 4. 发布为端点

选择 Agent 版本，结合回归评测与发布门槛决定是否发布。端点保留发布版本和记录，支持版本更新与回滚。详见[发布规则](rank/docs/release-policy.md)。

![发布 Agent](rank/docs/assets/lifecycle/04-publish.jpg)

### 5. 接入应用

打开端点的接入面板，配置 API Key，通过 cURL 或 JavaScript SDK 调用 Responses 兼容接口。支持流式输出、多轮上下文及调用记录追溯。详见[服务 API](rank/docs/serving-api.md)与[可运行客户端](rank/examples/responses-client/README.md)。

![接入自己的应用](rank/docs/assets/lifecycle/05-integrate.jpg)

<a id="run"></a>

## 一分钟开始

需要 Node.js 22.19 或更高版本，支持 macOS 和 Linux 的 x64、arm64 环境。

以下命令需要已发布的 AgentKiln npm 包和同版本运行包；发布前请使用下方源码启动方式。

```sh
npx -y @xyzbit/agentkiln@latest start
```

首次打开时选择 Provider 和模型，再填写 API Key。随后直接描述目标，例如“创建一个图片提示词助手，先让我试用”。AgentKiln 只会询问缺少的信息；需要评测时，再准备测试集并确认运行。

完整的首次使用、后台运行和独立数据目录说明见[一分钟上手](rank/docs/quickstart.md)。

## Harness 与架构

[DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) 是第一方运行时和控制对话基础。Pi、Claude Code、Codex 与 Hermes 通过平级 Runner Adapter 接入，未安装或未配置的 Runner 不会显示为可运行。

Go Rank 服务管理 Agent 版本、试用会话、评测任务和发布端点；Execution Service 在隔离的进程或 Sandbox 中执行 Agent 与 Judge。更多内容见[系统架构](rank/docs/architecture.md)和[核心数据流](rank/docs/core-data-flow.md)。

## 从源码运行

```sh
git clone https://github.com/xyzbit/agentkiln.git
cd agentkiln
pnpm install
pnpm rank:dev
```

源码开发、服务健康检查和完整验收见[本地开发与验收](rank/docs/local-acceptance.md)。仓库布局见[项目结构](rank/docs/project-structure.md)。

## 参与项目

欢迎提交 Issue 和 Pull Request。如果 AgentKiln 对你有帮助，也欢迎给仓库一个 Star。

本项目采用 [MIT License](LICENSE)。
