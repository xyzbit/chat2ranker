# Rank Web

[English](README.md) | 中文

本目录承载 chat2ranker 的 Control DSH 产品组合与会话优先 UI。

应用只负责浏览器入口与 DSH 插件组合。实验状态、调度、结果和 Runner 生命周期由 [`rank/backend/`](../../rank/backend/README.md) 下的 Go Rank 服务负责。

对话信息完整时直接执行；只缺一两个必要值时，Rank 只追问这些值。结构化 UI 仅用于浏览候选、检查多字段、批量编辑和输入密钥；表单转对话时只使用用户可读名称，不暴露内部 ID 或 JSON。

该组合挂载 Rank Control Host 插件、持久化实验 Session 与 Rank 实验 Skill。Session 通过工具创建、生成新版本和选择不可变测试集与 Agent 资产。React UI 立即显示发送消息，通过 SSE 展示投影后的 DSH 工具活动，并提供结构化资产 Mention、紧凑运行摘要和按请求出现的 A2UI 卡片。控制 Agent 回复使用逐行 Rank Message JSON 对象，由前端统一渲染摘要、正文、列表、事实、提示和代码；已有 Markdown 历史仍可读，复制操作会把两种格式统一导出为 Markdown，同时提供明确的成功或失败反馈及旧浏览器降级方案。只有运行意图才显示运行确认；只有实验数据、结果或表现意图及其输入框快捷操作才显示实验表现，并比较当前实验的全部 Run 与 Agent 版本。单 Agent 运行仍是默认路径；统一的 Agent 多选器在只选一个版本时替换当前 Agent，选中多个版本时创建一个 RunGroup，并为每个版本创建一个不可变子 Run，因此现有 Runner 与 Execution Service API 仍保持单任务语义。选择结果在关闭多选器时立即生效，再次打开仍保持选中，并以紧凑列表显示；列表溢出时可滑动但隐藏滚动条并用渐隐提示，每行都会在实验工作区打开对应版本。这些卡片是持久对话事件，不是固定在页面尾部的组件。控制 Session 可通过对话准备测试集、Rubric、Agent、非敏感模型连接元数据、Tool、Skill、对比 Agent 与重复策略，再将结果渲染为可检查的 A2UI；Provider 密钥绝不进入对话，只留在确定性的安全连接表单。模型连接工作区提供 DeepSeek、MiniMax、GLM、OpenAI、Claude 与 Kimi 官方模板；选择厂商后只展示该厂商模型，并继承端点、协议、价格、来源和计价说明。本地单模型价格覆盖仍收在高级设置中；Agent 表单只能引用已验证连接。确认卡中的资产分别提供查看详情与快捷切换操作。按目录或连接价格计算的运行成本带近似标记；既无 Provider 成本也无匹配价格时显示缺少计价配置，并排除在成本图之外。按需打开的实验工作区在桌面端与对话并存，用于测试集与 Agent 配置、整体实验表现、运行记录、用例、日志和 Artifact；点击聊天卡片或图表点可直接定位到对应实体。用户可直接在工作区修改测试集用例和 Agent 配置；每次保存都会创建并选中一个不可变新版本。Agent Tool 与 Skill 引用使用可搜索多选框，支持显式自定义 ID，也可通过 Rank 对话修改。`rankd` 把被测 Agent 提交给独立 Execution Service，它们不会在 Control DSH 进程内运行。

模型选择会合并平台内置目录与已保存连接发现的模型，标明来源并支持搜索；手动模型 ID 仅在需要时展开。保存密钥后可以重新同步模型列表；同步失败会保留上次成功结果，避免 Provider 临时故障清空配置。

在仓库根目录运行 `pnpm rank:dev` 即可启动完整本地组合。无密钥与真实 Provider 验收流程见 [`rank/docs/local-acceptance.md`](../../rank/docs/local-acceptance.md)。
