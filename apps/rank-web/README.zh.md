# Rank Web

`ManagementUI.jsx` 管理共用页面标题、错误提示、状态标签、空状态、分页和行内确认。页面直接引用该模块，无需通过其他页面导入组件。翻页可用性、请求和确认操作仍由调用页面负责。

[English](README.md) | 中文

本目录承载 agentkiln 的 Control DSH 产品组合与会话优先 UI。

应用只负责浏览器入口与 DSH 插件组合。实验状态、调度、结果和 Runner 生命周期由 [`rank/backend/`](../../rank/backend/README.md) 下的 Go Rank 服务负责。

对话信息完整时直接执行；只缺一两个必要值时，Rank 只追问这些值。结构化 UI 仅用于浏览候选、检查多字段、批量编辑和输入密钥；表单转对话时只使用用户可读名称，不暴露内部 ID 或 JSON。

该组合挂载 Rank Control Host 插件、持久化实验 Session 与 Rank 实验 Skill。Session 通过工具创建、生成新版本和选择不可变测试集与 Agent 资产。React UI 立即显示发送消息，通过 SSE 展示投影后的 DSH 工具活动，并提供结构化资产 Mention、紧凑运行摘要和按请求出现的 A2UI 卡片。控制 Agent 回复使用逐行 Rank Message JSON 对象，由前端统一渲染摘要、正文、列表、事实、提示和代码；已有 Markdown 历史仍可读，复制操作会把两种格式统一导出为 Markdown，同时提供明确的成功或失败反馈及旧浏览器降级方案。只有运行意图才显示运行确认；只有实验数据、结果或表现意图及其输入框快捷操作才显示实验表现，并比较当前实验的全部 Run 与 Agent 版本。单 Agent 运行仍是默认路径；统一的 Agent 多选器在只选一个版本时替换当前 Agent，选中多个版本时创建一个 RunGroup，并为每个版本创建一个不可变子 Run，因此现有 Runner 与 Execution Service API 仍保持单任务语义。选择结果在关闭多选器时立即生效，再次打开仍保持选中，并以紧凑列表显示；列表溢出时可滑动但隐藏滚动条并用渐隐提示，每行都会在实验工作区打开对应版本。这些卡片是持久对话事件，不是固定在页面尾部的组件。控制 Session 可通过对话准备测试集、Rubric、Agent、非敏感模型连接元数据、Tool、Skill、对比 Agent 与重复策略，再将结果渲染为可检查的 A2UI；Provider 密钥绝不进入对话，只留在确定性的安全连接表单。模型连接工作区提供 DeepSeek、MiniMax、GLM、OpenAI、Claude 与 Kimi 官方模板；选择厂商后只展示该厂商模型，并继承端点、协议、价格、来源和计价说明。本地单模型价格覆盖仍收在高级设置中；Agent 表单只能引用已验证连接。确认卡中的资产分别提供查看详情与快捷切换操作。按目录或连接价格计算的运行成本带近似标记；既无 Provider 成本也无匹配价格时显示缺少计价配置，并排除在成本图之外。按需打开的实验工作区在桌面端与对话并存，用于测试集与 Agent 配置、整体实验表现、运行记录、用例、日志和 Artifact；点击聊天卡片或图表点可直接定位到对应实体。用户可直接在工作区修改测试集用例和 Agent 配置；每次保存都会创建并选中一个不可变新版本。Agent Tool 与 Skill 引用使用可搜索多选框，支持显式自定义 ID，也可通过 Rank 对话修改。`rankd` 把被测 Agent 提交给独立 Execution Service，它们不会在 Control DSH 进程内运行。

消息解析器会先修复模型常见的引号、转义和尾逗号错误，仍无法解析时再回退为可读文本。

助手头像采用纯蓝圆形与白色眼睛。闲置时眼睛会左右看；鼠标进入周围 140 CSS 像素内时，视线跟随鼠标。离开该区域、滚动页面或窗口失焦后恢复闲置动作。减少动态效果设置会关闭闲置动画和鼠标跟随；触摸输入不会移动眼睛。

默认悬浮助手共用这套形象与视线行为。点击入口会打开已有助手抽屉；显示开关、自定义形象导入与工作状态圆点仍由助手设置管理。

对话和最近会话区域隐藏滚动条，仅在仍有内容的边缘显示渐隐。键盘滚动保持可用；只有读者已接近底部时，对话更新才跟随最新内容。表单采用实底，移动端固定导航采用轻微半透明。原生模态弹窗约束焦点并在关闭时恢复焦点；可搜索模型菜单使用浏览器顶层，支持方向键、Enter 和 Escape。参见 [UI 交互决策](../../.agents/notes/implemented/feature/2026-09-10-rank-ui-interaction.md)。

本地验收会话可用时，`playwright-cli run-code --filename apps/rank-web/tests/ui-layout.browser.js` 检查组装后的页面、桌面和移动端布局、语言切换、弹窗焦点、对话滚动和模型选择，不提交表单或模型请求。截图保存在 `output/playwright/`。

在 Playwright CLI 中打开已配置的本地 Rank 对话后，运行 `playwright-cli run-code --filename apps/rank-web/tests/assistant-avatar.browser.js` 可检查视线交互，并将桌面和移动端截图保存到 `output/playwright/`，不发送模型请求。

模型选择会合并平台内置目录与已保存连接发现的模型，标明来源并支持搜索；手动模型 ID 仅在需要时展开。保存密钥后可以重新同步模型列表；同步失败会保留上次成功结果，避免 Provider 临时故障清空配置。

在仓库根目录运行 `pnpm rank:dev` 即可启动完整本地组合。无密钥与真实 Provider 验收流程见 [`rank/docs/local-acceptance.md`](../../rank/docs/local-acceptance.md)。

所有单选字段使用基于 Radix Select 的 `SelectField`。页面提供 `value`、`onValueChange` 和带标签的选项；共用组件负责空值、禁用项、必填校验、键盘导航、焦点恢复及适配模态弹窗的菜单挂载。可编辑模型联想和可搜索模型发现保留专用输入控件。`theme.css` 管理视觉变量，`controls.css` 管理共用控件外观和焦点，`ui-config.js` 管理菜单定位。语言选项位于 `i18n.js`，Provider 和协议选项位于 `model-connections.js`。表单草稿保持本地状态，模型目录来自服务端。参见 [共用控件决策](../../.agents/notes/implemented/feature/2026-09-10-rank-shared-controls.md)。

导航语言控件共用一个翻译图标组件，提供本地化提示和无障碍名称。`playwright-cli run-code --filename apps/rank-web/tests/ui-controls.browser.js` 检查组装后的设置和筛选器、桌面和移动端布局、模态归属、嵌套 Escape 行为、禁用占位项及可选空值筛选，不保存表单。`playwright-cli run-code --filename apps/rank-web/tests/deployment-switcher.browser.js` 覆盖部署导航和语言切换。
