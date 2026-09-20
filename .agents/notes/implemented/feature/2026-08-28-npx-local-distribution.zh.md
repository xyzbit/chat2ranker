# Agent Note：npx 本地分发

状态：已实现

[English](2026-08-28-npx-local-distribution.md) | 中文

## 问题

源码启动器会在仓库中编译 Go 服务并启动 Vite，因此用户必须克隆源码并安装 pnpm 与 Go。若直接通过 npm 发布该脚本，持久数据库和凭据还会落入可能被清理的 npm 临时缓存。

## 决策

`@xyzbit/agentkiln` 是一个轻量 Node.js 启动器。它把匹配当前平台、经过 SHA256 校验的版本化 runtime 下载到 `~/.agentkiln/runtime/`，统一管理 Execution Service、Rank、Control DSH 和静态 Web UI，并把全部持久状态保存在 `~/.agentkiln`。`--home` 可隔离另一套环境。默认前台启动；后台启动、状态、打开和停止通过一个私有 PID 文件管理。

runtime 压缩包包含三个静态编译的 Go 二进制、生产 Web 资源、Rank Skills 和 DSH 运行时。DSH 不进入轻量 npm 启动器，避免每个用户执行 `npx` 时重新解析庞大的依赖图。Execution Worker 通过 `RANK_DSH_BIN` 获得已打包 DSH CLI 的绝对路径；源码运行仍保留仓库路径回退。

打包的 DSH runtime 固定使用完整的 `0.1.0-rc.8` 包族。发布清单把传递依赖中的 DSH workspace 包统一覆盖为该版本，并拒绝包含其他 DSH 版本的 runtime，避免 npm 后续预发布版本静默形成混装的插件依赖图。

`pnpm rank:package` 在 `dist/agentkiln/` 中生成当前平台 runtime、校验文件和 npm 压缩包。脚本只打印本机验收与外部发布命令，不会自行发布。

产品公开名称为 AgentKiln（包、仓库、启动器和压缩包名称使用 `agentkiln`，启动器环境变量使用 `AGENTKILN_*`）。该名称覆盖准备、评测、发布和对外服务。内部 Rank 服务名、API 请求头与资源标识保持稳定。默认数据目录为 `~/.agentkiln`；使用原有数据时，需要先停止旧启动器，再显式传入 `--home`，详见[快速上手](../../../../rank/docs/quickstart.md#existing-local-data)。向 GitHub 推送源码不会发布 npm 包或运行压缩包。

## 考虑过的备选方案

**把完整 runtime 作为 npm 依赖发布。** 这会让每次 `npx` 都解析庞大的 DSH 依赖图，并可能混装不同预发布版本，因此最终使用轻量启动器安装一个经过校验的 runtime 压缩包。

**只保留源码启动。** 这种方式实现最短，但要求每位用户安装 Go 与 pnpm，不适合作为公开分享后的快速试用入口。

**只通过 Docker 分发。** Docker 适合服务端部署，但会给默认的本地上手流程增加容器环境和文件映射等额外概念。

## 结果

用户仍需 Node.js，但不再需要源码、Go 或 pnpm。清理 npm 缓存不会删除实验或凭据。程序升级替换不可变 runtime 版本，SQLite 迁移继续原地升级持久数据。DSH 升级必须显式修改发布脚本并完成发布包工具调用验收，不再由依赖解析顺带发生。Docker 仍是独立的服务端部署方式，不作为本地默认安装路径。
