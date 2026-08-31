# 一分钟上手 Chat2Ranker

[English](quickstart.md) | 中文

本教程面向第一次使用 Chat2Ranker 的本地用户。完成后，你会拥有一个持久化实验，并能从对话中准备测试集、选择 Agent、运行评测和查看结果。

## 1. 启动

准备 Node.js 22.19 或更高版本。Chat2Ranker 支持 macOS 和 Linux 的 x64、arm64 环境。

```sh
npx -y @xyzbit/chat2ranker@latest start
```

首次运行会下载并校验当前平台的预编译 runtime，然后打开浏览器。数据库、模型凭据、日志和运行产物保存在 `~/.chat2ranker`，不会写入 npm 缓存。

## 2. 连接模型

首次打开只需完成三个字段：

1. 选择 Provider。
2. 选择模型。
3. 填写 API Key。

官方 Provider 会自动填写 Base URL 和内置价格。连接验证通过后，Control 对话 Agent 与 Judge 都可运行；Judge 默认复用同一个连接，之后可在实验工作区中单独切换。

## 3. 创建第一个实验

直接描述你要验证的目标，例如：

> 帮我整理 10 个 Web 研究用例，对比现有的两个 Agent，查看通过率、成本和失败原因。

Rank 会根据缺少的信息继续询问，也可以让你从已有测试集和 Agent 中选择。一个会话就是一个实验，测试集与 Agent 配置会保存为可复用的版本。

平台只会在你要求运行时展示运行确认卡。检查测试集、Agent 和重复次数后，点击卡片中的“开始运行”或“开始对比”；普通发送按钮不会启动评测。

## 4. 查看结果

运行完成后，对话中会显示通过率、稳定用例、成本和阶段状态。点击“完整结果”可在实验工作区查看每个用例、Judge 结论、执行日志和产物；“实验表现”会比较当前实验中的多个 Agent 运行。

成本优先使用 Provider 返回值，其次使用模型连接价格和内置官方目录。三者都没有时，界面会显示“缺少计价配置”，不会给出猜测值。

## 后台运行与停止

```sh
npx -y @xyzbit/chat2ranker@latest start --detach
npx -y @xyzbit/chat2ranker@latest status
npx -y @xyzbit/chat2ranker@latest open
npx -y @xyzbit/chat2ranker@latest stop
```

前台运行时按一次 `Ctrl-C` 即可停止完整服务组。再次启动会恢复已有连接、实验、Control Session 和运行产物。

## 使用独立环境

测试首次初始化或临时实验时，不需要删除已有数据：

```sh
npx -y @xyzbit/chat2ranker@latest start --home /tmp/chat2ranker-clean
```

源码开发、服务端口和完整验收流程见[本地开发与验收](local-acceptance.md)。
