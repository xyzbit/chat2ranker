# 产品资产

[English](README.md) | 中文

`control/` 包含 Rank 实验 Skill 与 Control DSH Preset 源文件；`judge/` 包含版本化的默认 Judge 配置。

被测 Agent 配置属于业务数据。运行会冻结 `AgentVersion` 快照并通过 Runner 协议传递；运行期间不能直接从本源码树加载可变 Agent 配置。
