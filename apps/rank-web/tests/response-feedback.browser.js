async page => {
  const check = (value, message) => { if (!value) throw new Error(message); };
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  const origin = await page.evaluate(() => location.origin);
  const fixture = await page.evaluate(async () => {
    const api = async (path, body, headers = {}) => {
      const response = await fetch(path, body === undefined ? {} : { method: "POST", headers: { "content-type": "application/json", ...headers }, body: JSON.stringify(body) });
      if (!response.ok) throw new Error(await response.text());
      return response.json();
    };
    const b = await api("/api/bootstrap");
    const endpoint = b.deployments.find(d => d.name === "research-public");
    const key = await api("/api/serving-credentials", { name: "feedback-browser", deploymentIds: [endpoint.id], scopes: ["responses:create"], tenantId: b.tenantId });
    try {
      const first = await api("/v1/responses", { model: endpoint.name, input: "Contact user@example.com for help", instructions: "Be concise" }, { authorization: "Bearer " + key.apiKey });
      const second = await api("/v1/responses", { model: endpoint.name, input: "Which address did I mention?", previous_response_id: first.id }, { authorization: "Bearer " + key.apiKey });
      localStorage.setItem("rank.locale", "zh"); localStorage.setItem("rank.modelSetupDismissed", "true");
      return { first: first.id, second: second.id, agent: first.deployment.agent_version_id, endpoint: endpoint.id };
    } finally { await api(`/api/serving-credentials/${key.id}/revoke`, {}); }
  });
  await page.goto(`${origin}/#/responses?responseId=${fixture.second}`);
  await page.reload();
  await page.getByText(fixture.second, { exact: true }).waitFor();
  await page.reload();
  await page.getByRole("link", { name: "上一轮调用", exact: true }).click();
  await page.getByText(fixture.first, { exact: true }).waitFor();
  await page.getByRole("tab", { name: "请求配置", exact: true }).click();
  await page.getByText("Be concise", { exact: true }).waitFor();
  await page.getByRole("tab", { name: "执行事件", exact: true }).click();
  await page.getByText("response.completed", { exact: true }).waitFor();
  await page.getByRole("button", { name: "查看整个会话", exact: true }).click();
  await page.locator(".management-table tbody tr").first().waitFor();
  check(await page.locator(".management-table tbody tr").count() === 2, "Conversation filter lost a turn");
  await page.getByRole("button", { name: "清除", exact: true }).click();
  await page.getByRole("textbox", { name: "搜索请求 ID 或端点", exact: true }).fill(fixture.first);
  await page.getByRole("button", { name: "搜索", exact: true }).click();
  await page.waitForFunction(id => document.querySelectorAll(".management-table tbody tr").length === 1 && document.querySelector(".management-table")?.textContent.includes(id), fixture.first);
  await page.locator(".management-table .table-link").click();
  await page.getByRole("button", { name: "返回调用记录", exact: true }).click();
  check(await page.getByRole("textbox", { name: "搜索请求 ID 或端点", exact: true }).inputValue() === fixture.first, "Returning from detail lost the search");
  await page.locator(".management-table .table-link").click();
  await page.getByRole("button", { name: "加入数据集", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "加入数据集", exact: true });
  await dialog.getByRole("textbox", { name: "数据集名称", exact: true }).fill("Browser feedback");
  await dialog.getByRole("textbox", { name: "用例标题", exact: true }).fill("Help request");
  await dialog.getByRole("textbox", { name: "输入（可编辑脱敏）", exact: true }).fill("Contact [email] for help");
  check(await dialog.getByRole("textbox", { name: "期望结果", exact: true }).inputValue() === "", "Actual output was mistaken for expected output");
  await dialog.getByRole("textbox", { name: "期望结果", exact: true }).fill("Provide a useful support answer");
  check(await dialog.getByRole("button", { name: "保存用例", exact: true }).isDisabled(), "Unreviewed case can be saved");
  await dialog.getByRole("checkbox").check();
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.screenshot({ path: `output/playwright/response-capture-${width}.png` });
    check(await dialog.evaluate(node => node.getBoundingClientRect().right <= innerWidth && node.getBoundingClientRect().left >= 0), "Capture dialog overflows");
  }
  const savedRequest = page.waitForResponse(r => r.url().includes("/dataset-cases") && r.request().method() === "POST");
  await dialog.getByRole("button", { name: "保存用例", exact: true }).click();
  const saved = await (await savedRequest).json();
  check(saved.cases[0].source.responseId === fixture.first && !JSON.stringify(saved).includes("user@example.com"), "Capture lost source or copied unreviewed text");
  await dialog.getByRole("button", { name: "去评测", exact: true }).click();
  await page.waitForURL("**/#/agent");
  await page.getByRole("textbox", { name: "输入消息", exact: true }).waitFor();
  const selection = await page.evaluate(async () => (await fetch(`/api/experiments/${localStorage.getItem("rank.activeExperiment")}`)).json());
  check(selection.dataset.id === saved.id && selection.agent.id === fixture.agent, "Evaluation handoff lost exact inputs");
  await page.locator(".ready-card").getByRole("button", { name: "开始运行", exact: true }).waitFor();
  check(selection.controlEvents.some(event => event.type === "a2ui/prepare_run") && selection.runs.length === 0, "Evaluation must prepare confirmation without starting execution");
  await page.screenshot({ path: "output/playwright/response-evaluation-mobile.png" });
  await page.goto(`${origin}/#/responses?responseId=${fixture.first}`);
  await page.getByText(fixture.first, { exact: true }).waitFor();
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.screenshot({ path: `output/playwright/response-detail-${width}.png` });
    check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "Response detail overflows");
  }
  await page.goto(`${origin}/#/agents`);
  await page.getByRole("button", { name: "新建 Agent", exact: true }).click();
  await page.getByRole("textbox", { name: "名称", exact: true }).fill("Asset creation stays here");
  await page.getByRole("combobox", { name: "运行方式", exact: true }).click();
  await page.getByRole("option", { name: /Demo/ }).click();
  await page.getByRole("button", { name: "创建并选择", exact: true }).click();
  await page.getByRole("heading", { name: "Asset creation stays here", exact: true }).waitFor();
  check(page.url().includes("/#/agents?agent="), "Asset creation went to an unrelated conversation");
  check(!errors.length, errors.join("\n"));
}
