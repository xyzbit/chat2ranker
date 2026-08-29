# Rank 产品源码

[English](README.md) | 中文

`rank/` 目录包含产品控制平面与执行平面集成，不包含第二份 DSH 源码。

| 目录 | 所有者 | 职责 |
|---|---|---|
| `api/` | Rank 协议 | Control 与产品 API 定义 |
| `backend/` | Go Rank 服务 | 实验、不可变版本、用例调度、评分、聚合与持久化 |
| `runners/` | Rank 集成 | Harness 预设与执行平面配置 |
| `assets/` | 产品配置 | Control Skill、Preset 源文件与版本化 Judge 默认配置 |
| `deploy/` | 运维 | 本地 Compose 与生产部署清单 |
| `tests/` | 跨进程验证 | 协议、集成与端到端测试 |
| `docs/` | 产品架构 | 系统结构、数据流、目录与决策 |

产品运行时包含四个进程角色：

1. Control DSH 承载用户对话、Skill 与 A2UI。
2. `rankd` 拥有产品状态并提交不可变的候选与 Judge 规格。
3. `executiond` 拥有通用执行状态、持久进度事件、Executor 选择、取消与 Artifact。
4. `execution-worker` 解析一个公共 Harness Adapter，并在隔离本地进程或 Sandbox 中启动它。

被测 DSH 运行时是 Sandbox 内的 Runner 实例。它不与 Control DSH 共享 Cordis Context、Session 存储、DSH Home、工作区、环境或进程。
