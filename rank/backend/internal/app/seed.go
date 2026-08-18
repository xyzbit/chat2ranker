package app

import (
	"context"
	"fmt"

	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
)

func (s *Service) EnsureSeed(ctx context.Context) error {
	families, err := s.repo.ListDatasetFamilies(ctx)
	if err != nil {
		return err
	}
	if len(families) > 0 {
		return nil
	}
	now := s.now().UTC()
	return s.repo.WithinTx(ctx, func(repo domain.Repository) error {
		webFamily := domain.DatasetFamily{ID: "dataset-web-research", Name: "Web 研究基准集", Description: "覆盖检索、总结、引用和结构化输出。", LatestVersionID: "dataset-web-research-v3", CreatedAt: now}
		webVersion := domain.DatasetVersion{ID: webFamily.LatestVersionID, FamilyID: webFamily.ID, Name: webFamily.Name, Version: 3, Source: "浏览器采集 + Web 搜索", Description: webFamily.Description, Schema: defaultJSON(nil), Rubric: defaultJSON(nil), Cases: seedWebCases(), CreatedAt: now, CaseCount: 12}
		if err := repo.CreateDatasetFamily(ctx, webFamily, webVersion); err != nil {
			return err
		}
		browserFamily := domain.DatasetFamily{ID: "dataset-browser-actions", Name: "浏览器操作冒烟集", Description: "登录、导航、筛选、表单和文件下载等基础操作。", LatestVersionID: "dataset-browser-actions-v1", CreatedAt: now}
		browserCases := make([]domain.Case, 8)
		titles := []string{"打开页面", "站内搜索", "筛选结果", "填写表单", "翻页", "下载文件", "异常恢复", "提取结果"}
		for index := range browserCases {
			expected := map[string]any{"summary": "操作完成且返回结构化结果", "demoOutcome": "pass"}
			if index == 6 {
				expected["demoOutcome"] = "fail"
				expected["failureReason"] = "异常恢复后状态不一致"
			}
			browserCases[index] = domain.Case{ID: fmt.Sprintf("browser-%02d", index+1), Title: titles[index], Input: fmt.Sprintf("完成浏览器操作场景 %d，保留关键步骤结果。", index+1), Expected: expected}
		}
		browserVersion := domain.DatasetVersion{ID: browserFamily.LatestVersionID, FamilyID: browserFamily.ID, Name: browserFamily.Name, Version: 1, Source: "手工维护", Description: browserFamily.Description, Schema: defaultJSON(nil), Rubric: defaultJSON(nil), Cases: browserCases, CreatedAt: now, CaseCount: 8}
		if err := repo.CreateDatasetFamily(ctx, browserFamily, browserVersion); err != nil {
			return err
		}
		agents := []struct {
			family  domain.AgentFamily
			version domain.AgentVersion
		}{
			{domain.AgentFamily{ID: "agent-research-demo", Name: "Research Demo", Handle: "@demo/research", Description: "无需密钥即可验证数据集、运行、评分和日志链路。", LatestVersionID: "agent-research-demo-v1", CreatedAt: now}, domain.AgentVersion{ID: "agent-research-demo-v1", FamilyID: "agent-research-demo", Name: "Research Demo", Handle: "@demo/research", Version: 1, RunnerType: "mock", Description: "无需密钥即可验证数据集、运行、评分和日志链路。", Model: "deterministic-demo", Tools: []string{"browser", "web_search"}, CreatedAt: now}},
			{domain.AgentFamily{ID: "agent-dsh-research", Name: "DSH Research", Handle: "@dsh/research", Description: "使用 DeepSeek Harness headless profile；模型可由版本配置覆盖。", LatestVersionID: "agent-dsh-research-v1", CreatedAt: now}, domain.AgentVersion{ID: "agent-dsh-research-v1", FamilyID: "agent-dsh-research", Name: "DSH Research", Handle: "@dsh/research", Version: 1, RunnerType: "dsh", Description: "使用 DeepSeek Harness headless profile；模型可由版本配置覆盖。", Model: "由 DSH profile 决定", Tools: []string{"browser", "web_search", "files"}, CreatedAt: now}},
			{domain.AgentFamily{ID: "agent-pi-research", Name: "Pi Web Research", Handle: "@pi/research", Description: "使用 Pi 原生 agent loop；也可切换为外部 Runner Adapter。", LatestVersionID: "agent-pi-research-v1", CreatedAt: now}, domain.AgentVersion{ID: "agent-pi-research-v1", FamilyID: "agent-pi-research", Name: "Pi Web Research", Handle: "@pi/research", Version: 1, RunnerType: "pi", Description: "使用 Pi 原生 agent loop；也可切换为外部 Runner Adapter。", Model: "由 Pi 配置决定", Tools: []string{"browser", "web_search", "files"}, CreatedAt: now}},
		}
		for _, entry := range agents {
			if err := repo.CreateAgentFamily(ctx, entry.family, entry.version); err != nil {
				return err
			}
		}
		return nil
	})
}

func seedWebCases() []domain.Case {
	rows := [][3]string{
		{"检索行业规模", "检索 2025 年全球人形机器人市场规模，给出至少两个公开来源。", "包含明确年份、数值和两个可访问引用"},
		{"比较机构预测", "比较两家研究机构对 AI Agent 市场未来三年的预测差异。", "区分机构、口径和预测时间"},
		{"提炼趋势证据", "总结企业搜索产品的三个新趋势，每个趋势附原始出处。", "三条趋势均有可回溯引用"},
		{"识别冲突信息", "查找两篇对同一行业增速结论不同的资料并解释原因。", "指出统计口径或时间范围差异"},
		{"限定时间检索", "只使用最近 90 天的公开资料总结浏览器 Agent 进展。", "所有引用均处于时间范围内"},
		{"过滤营销内容", "调研 RAG 评测工具，优先使用官方文档和论文而不是软文。", "来源以一手资料为主"},
		{"校验失效链接", "汇总五个可公开访问的 Agent benchmark 页面并验证链接。", "链接均返回可访问页面"},
		{"结构化输出", "将三种 Web Agent 的能力、限制和开源协议整理成表格。", "字段完整且比较口径一致"},
		{"引用原句定位", "找出两项关于工具调用可靠性的公开研究结论并定位原文。", "引用可以定位到原始段落"},
		{"多语言来源", "结合中文和英文资料总结 AI 搜索产品在亚洲的采用情况。", "至少各含一个中英文来源"},
		{"引用回溯", "生成一段带脚注的行业摘要，并确保每个脚注都能回溯网页。", "脚注与网页内容逐一对应"},
		{"结论与证据分离", "总结浏览器自动化的机会与风险，区分事实和判断。", "事实有引用，判断有明确标记"},
	}
	cases := make([]domain.Case, len(rows))
	for index, row := range rows {
		expected := map[string]any{"summary": row[2], "demoOutcome": "pass"}
		if index == 6 {
			expected["demoOutcome"] = "fail"
			expected["failureReason"] = "有一个来源链接无法访问"
		}
		if index == 10 {
			expected["demoOutcome"] = "fail"
			expected["failureReason"] = "两个脚注无法回溯到原文"
		}
		cases[index] = domain.Case{ID: fmt.Sprintf("web-%02d", index+1), Title: row[0], Input: row[1], Expected: expected}
	}
	return cases
}
