# Rank 产品源码

[English](README.md) | 中文

`rank/` 目录包含产品控制平面与执行平面集成，不包含第二份 DSH 源码。

| 目录 | 所有者 | 职责 |
|---|---|---|
| `api/` | Rank 协议 | Control API 与 Runner 线协议定义 |
| `backend/` | Go Rank 服务 | 实验、不可变版本、调度、聚合、取消与持久化 |
| `runners/` | Runner 适配器 | 每种 Harness 对应一个隔离适配器与镜像 |
| `assets/` | 产品配置 | Control Skill、Preset 源文件与版本化 Judge 默认配置 |
| `deploy/` | 运维 | 本地 Compose 与生产部署清单 |
| `tests/` | 跨进程验证 | 协议、集成与端到端测试 |
| `docs/` | 产品架构 | 系统结构、数据流、目录与决策 |

运行时包含三个进程角色：

1. Control DSH 承载用户对话、Skill 与 A2UI。
2. `rankd` 负责产品状态并分发不可变执行规格。
3. `rank-worker` 为每个用例启动包含指定 Runner 的 Sandbox。

被测 DSH 运行时是 Sandbox 内的 Runner 实例。它不与 Control DSH 共享 Cordis Context、Session 存储、DSH Home、工作区、环境或进程。
