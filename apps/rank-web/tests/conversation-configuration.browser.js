async (page) => {
  const check = (condition, message) => { if (!condition) throw new Error(message); };
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  const origin = await page.evaluate(() => location.origin);
  const fixture = await page.evaluate(async () => {
    const request = async (path, body, headers = {}) => {
      const response = await fetch(path, body ? { method: "POST", headers: { "content-type": "application/json", ...headers }, body: JSON.stringify(body) } : {});
      if (!response.ok) throw new Error(`Fixture request: ${response.status}`);
      return response.json();
    };
    const bootstrap = await request("/api/bootstrap");
    const deployment = bootstrap.deployments[0];
    const experiment = await request("/api/experiments", { title: "Configuration acceptance" });
    for (const section of ["preferences", "model_connections", "local_connections", "system_models", "api_keys", "webhooks", "release_policy", "endpoint_integration"]) {
      await request("/api/internal/control/commands", {
        experimentId: experiment.id, controlSessionId: experiment.controlSessionId, idempotencyKey: section, type: "open_configuration",
        payload: { section, ...(["webhooks", "release_policy", "endpoint_integration"].includes(section) ? { deploymentId: deployment.id } : {}) },
      }, { "x-rank-control-token": "configuration-browser-fixture" });
    }
    localStorage.setItem("rank.activeExperiment", experiment.id);
    localStorage.setItem("rank.modelSetupDismissed", "true");
    localStorage.setItem("rank.locale", "zh");
    return { experimentId: experiment.id, deploymentId: deployment.id, tenantId: bootstrap.tenantId };
  });
  await page.route("**/api/local-model-connections/discover", route => route.fulfill({ json: { candidates: [], issues: [] } }));
  await page.goto(`${origin}/#/agent`);
  await page.reload();
  const action = name => page.locator(".configuration-action").filter({ hasText: name });
  const open = async name => { await action(name).click(); const dialog = page.getByRole("dialog", { name, exact: true }); await dialog.waitFor(); return dialog; };
  const close = async dialog => { await dialog.getByRole("button", { name: "关闭", exact: true }).click(); await dialog.waitFor({ state: "hidden" }); };
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.locator(".configuration-action").last().waitFor();
    check(await page.locator(".configuration-action").count() === 8, "Durable actions missing from conversation");
    const preferences = await open("偏好设置");
    check(await preferences.getByRole("heading", { name: "连接", exact: true }).count() === 0, "Unrelated settings visible");
    const companion = preferences.getByRole("checkbox");
    await companion.setChecked(false);
    await close(preferences);
    await page.reload();
    const restoredPreferences = await open("偏好设置");
    check(!await restoredPreferences.getByRole("checkbox").isChecked(), "Preference not persisted");
    await restoredPreferences.getByRole("combobox", { name: "语言", exact: true }).click();
    await page.getByRole("option", { name: "English", exact: true }).click();
    await restoredPreferences.waitFor({ state: "hidden" });
    const english = page.getByRole("dialog", { name: "Preferences", exact: true });
    await english.getByRole("combobox", { name: "Language", exact: true }).click();
    await page.getByRole("option", { name: "简体中文", exact: true }).click();
    await close(page.getByRole("dialog", { name: "偏好设置", exact: true }));

    const local = await open("本地连接");
    check(await local.getByRole("tab", { name: "本地连接", exact: true }).getAttribute("aria-selected") === "true", "Local action opened wrong tab");
    await local.getByRole("button", { name: "重新发现", exact: true }).click();
    await local.getByText("未发现可复用的本地连接", { exact: true }).waitFor();
    await close(local);

    const models = await open("系统模型");
    check(await models.locator(".binding-form").count() === 2, "System bindings unavailable");
    await close(models);
    const connection = await open("模型连接");
    await connection.getByRole("button", { name: "添加连接", exact: true }).click();
    const input = connection.getByRole("combobox", { name: "模型", exact: true });
    await input.fill("custom-conversation-model");
    await input.press("Escape");
    await connection.getByRole("button", { name: "模型", exact: true }).click();
    await input.press("Escape");
    check(await connection.isVisible(), "Model menu Escape closed configuration dialog");
    check(await input.inputValue() === "custom-conversation-model", "Custom model lost");
    await close(connection);

    const keyDialog = await open("API Key");
    await keyDialog.getByRole("button", { name: "创建 API Key", exact: true }).click();
    const form = keyDialog.locator(".credential-form");
    check(/^apikey-\d{8}-\d{6}$/.test(await form.getByRole("textbox", { name: "名称", exact: true }).inputValue()), "Default key name missing");
    check(await form.getByRole("checkbox").evaluateAll(items => items.every(item => item.checked)), "Default permissions not selected");
    await form.getByRole("textbox", { name: "名称", exact: true }).fill(`conversation-key-${width}`);
    await page.screenshot({ path: `output/playwright/conversation-key-form-${width}.png` });
    await form.getByRole("button", { name: "创建 API Key", exact: true }).click();
    await keyDialog.locator(".secret-reveal code").waitFor();
    const secret = await keyDialog.locator(".secret-reveal code").innerText();
    await close(keyDialog);
    check(await page.evaluate(async ({ id, secret }) => {
      const chat = await fetch(`/api/experiments/${id}`).then(response => response.text());
      return !chat.includes(secret) && !JSON.stringify(localStorage).includes(secret) && !JSON.stringify(sessionStorage).includes(secret);
    }, { id: fixture.experimentId, secret }), "Key leaked to chat or browser storage");
    const reopened = await open("API Key");
    check(await reopened.locator(".secret-reveal").count() === 0, "Key retained after close");
    const row = reopened.locator(".credential-row").filter({ hasText: `conversation-key-${width}` });
    await row.getByRole("button", { name: "吊销密钥", exact: true }).click();
    await reopened.getByRole("button", { name: "确认吊销", exact: true }).click();
    await row.getByText("已吊销", { exact: true }).waitFor();
    await close(reopened);

    const policy = await open("发布门槛");
    const passRate = policy.locator("input[type='number']").first();
    await passRate.fill(width === 1440 ? "87" : "88");
    await policy.getByRole("button", { name: "保存", exact: true }).click();
    await policy.getByText("已保存", { exact: true }).waitFor();
    await page.screenshot({ path: `output/playwright/conversation-policy-${width}.png` });
    await close(policy);
    const persisted = await open("发布门槛");
    check(await persisted.locator("input[type='number']").first().inputValue() === (width === 1440 ? "87" : "88"), "Policy not persisted");
    await close(persisted);

    const webhook = await open("Webhook");
    await webhook.getByRole("button", { name: "添加 Webhook", exact: true }).click();
    const webhookForm = page.locator(".webhook-dialog");
    await webhookForm.getByRole("textbox").first().fill(`conversation-webhook-${width}`);
    await webhookForm.getByRole("textbox").nth(1).fill("https://example.com/rank-test");
    await webhookForm.getByRole("button", { name: "创建 Webhook", exact: true }).click();
    await webhookForm.locator(".webhook-secret-panel code").waitFor();
    const signingSecret = await webhookForm.locator(".webhook-secret-panel code").innerText();
    await webhookForm.getByRole("button", { name: "完成", exact: true }).click();
    check(await page.evaluate(async ({ id, secret }) => !(await fetch(`/api/experiments/${id}`).then(response => response.text())).includes(secret), { id: fixture.experimentId, secret: signingSecret }), "Webhook secret leaked into chat");
    const hookRow = webhook.locator(".webhook-list article").filter({ hasText: `conversation-webhook-${width}` });
    await hookRow.getByRole("button", { name: "停用", exact: true }).click();
    await hookRow.getByRole("button", { name: "启用", exact: true }).waitFor();
    await close(webhook);
    const integration = await open("接入");
    await close(integration);
    check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "Horizontal overflow");
    await page.reload();
    await page.locator(".configuration-action").last().waitFor();
    check(await page.locator(".configuration-action").count() === 8, "Configuration actions lost after reload");
  }
  await page.unrouteAll({ behavior: "wait" });
  check(errors.length === 0, errors.join("\n"));
  return { viewports: [1440, 390], actions: 8, persistence: ["preferences", "keys", "policy", "webhooks"], secrets: "excluded from conversation and browser storage" };
}
