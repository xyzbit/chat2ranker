# Agent Note: Rank 多 Agent 对比

Status: implemented

[English](2026-08-25-rank-multi-agent-comparison.md) | 中文

## Problem

实验原本只能逐次选择并运行 Agent 版本。用户无法先冻结一份测试集，再选择多个 Agent 版本并通过一次操作启动公平对比。若把多个 Agent 放进同一个 Run，还会削弱现有执行与计费不变量。

## Decision

单 Agent 运行仍是默认路径。运行确认 A2UI 卡片只保留一个 Agent 选择器，可选择一至四个版本：单选时替换当前 Agent，多选时准备对比并在确认前展示相乘后的 Trial 总数。关闭选择器就会应用草稿，再次打开会恢复选择；已选版本以紧凑列表展示，溢出时可滑动但隐藏滚动条并用渐隐提示，点击任意一行都会在详情侧栏打开对应版本。Control Agent 也可以把用户点名的不可变 Agent 版本 ID 与重复次数直接传给确认卡，并会把多 Agent 执行说明为含独立子 Run 的 RunGroup，而不再声称平台只能手动依次运行。

Rank 为一次对比请求持久化一个 RunGroup，并通过同一事务为每个选定 Agent 版本创建一个子 Run。每个子 Run 冻结相同的 DatasetVersion 与重复次数，分别冻结自己的 AgentVersion 和评分配置，独立持有 Trial、成本、轨迹与结果记录，再通过现有单 Run 路径调度。RunGroup 负责对比请求的幂等性，记录有序 Agent 版本与子 Run ID；其状态由子 Run 状态推导。重启恢复继续直接处理子 Run，因此 Execution Service、Runner Adapter 与 Harness 进程都不需要多 Agent 协议。

浏览器同时订阅所有活跃子 Run。完成后的子 Run 仍是普通实验结果，因此会直接进入已有的通过率—成本或时延图表，无需新增对比仪表盘。

## Alternatives considered

**在一个 Run 中保存多个 Agent。** 放弃，因为 Run 的快照、成本、重试、取消、事件与结果聚合有意限定在一个被测 Agent 版本内；扩展该实体会把对比 UI 耦合到每条 Runner 与持久化路径。

**由浏览器依次创建多个 Run。** 放弃，因为部分创建、重复点击或浏览器中断都可能留下不完整对比，而且没有父级幂等记录。

**把多 Agent 行为加入 Execution Service。** 放弃，因为扇出属于 Rank 业务编排。Execution Service 应继续接收一个 Harness 任务并报告一个执行生命周期。

## Consequences

一次确认即可在相同测试集与重复策略下比较多个 Agent，同时保留现有执行隔离和逐 Agent 计费。对比会按照选中 Agent 数量成比例增加 Provider 并发与总成本；确认卡会明确展示该乘数，并把单组限制为四个 Agent。当前不需要组级取消与专用组结果卡，因为子 Run 控制和实验对比图已经提供必要操作与结果。
