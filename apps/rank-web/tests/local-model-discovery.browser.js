async (page) => {
  const check = (value, message) => { if (!value) throw new Error(message); };
  const origin = await page.evaluate(() => location.origin);
  const candidate = { id: "local-fixture", source: "Codex", name: "Codex · Local fixture", protocol: "openai-responses", baseUrl: "https://fixture.example/v1", model: "fixture-model" };
  const connection = { id: "model-fixture", name: candidate.name, provider: "custom", protocol: candidate.protocol, baseUrl: candidate.baseUrl, defaultModel: candidate.model, hasCredential: true, status: "unverified", models: [] };
  let failScan = false;
  let emptyScan = false;
  let holdScan = false;
  const pendingScans = [];
  let imported = false;
  let imports = 0;
  const bindings = [];
  await page.route("**/api/bootstrap**", async route => {
    const response = await route.fetch();
    const data = await response.json();
    await route.fulfill({ json: { ...data, modelConnections: [], systemModels: [] } });
  });
  await page.route("**/api/local-model-connections/discover", async route => {
    if (holdScan) await new Promise(resolve => { pendingScans.push(resolve); });
    return failScan ? route.abort("failed") : route.fulfill({ json: { candidates: emptyScan ? [] : [{ ...candidate, connectionId: imported ? connection.id : undefined }], issues: [] } });
  });
  await page.route("**/api/local-model-connections/local-fixture/import", route => {
    check(route.request().postDataJSON().model === "custom-model", "Custom local model was not sent for verification");
    imported = true; imports += 1;
    return route.fulfill({ json: connection });
  });
  await page.route("**/api/model-connections/model-fixture/verify", route => route.fulfill({ json: { ...connection, status: "verified" } }));
  await page.route("**/api/system-models/*", route => {
    bindings.push({ role: route.request().url().split("/").at(-1), ...route.request().postDataJSON() });
    return route.fulfill({ json: {} });
  });
  const reset = async () => {
    await page.evaluate(() => localStorage.removeItem("rank.modelSetupDismissed"));
    await page.goto(origin);
    await page.reload();
    await page.getByRole("heading", { name: "Rank", exact: true }).waitFor();
  };
  try {
    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 850 });
      await reset();
      await page.getByRole("combobox", { name: "连接", exact: true }).waitFor();
      check(await page.getByRole("tab", { name: "本地连接", exact: true }).getAttribute("aria-selected") === "true", "Local candidates did not select the local tab");
      await page.getByRole("tab", { name: "连接配置", exact: true }).click();
      await page.getByRole("textbox", { name: "API Key", exact: true }).fill("draft-key");
      await page.getByRole("combobox", { name: "模型", exact: true }).fill("manual-model");
      await page.getByRole("tab", { name: "本地连接", exact: true }).click();
      await page.getByRole("tab", { name: "连接配置", exact: true }).click();
      check(await page.getByRole("textbox", { name: "API Key", exact: true }).inputValue() === "draft-key", "Tab switch erased the key draft");
      check(await page.getByRole("combobox", { name: "模型", exact: true }).inputValue() === "manual-model", "Tab switch erased the custom model");
      await page.screenshot({ path: `output/playwright/manual-setup-${width}.png` });
      await page.getByRole("tab", { name: "本地连接", exact: true }).click();
      check(await page.getByRole("button", { name: "稍后配置", exact: true }).isEnabled(), "Scan blocked dismissal");
      check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "Setup overflows viewport");
      check(await page.locator(".system-setup-content").evaluate(el => el.getBoundingClientRect().top >= 0), "Setup heading clipped");
      await page.screenshot({ path: `output/playwright/local-setup-${width}.png` });
    }
    await page.getByRole("combobox", { name: "模型", exact: true }).fill("custom-model");
    await page.getByRole("button", { name: "开始对话", exact: true }).click();
    await page.locator(".system-setup").waitFor({ state: "detached" });
    check(bindings.length === 2 && bindings.every(item => item.connectionId === "model-fixture" && item.model === "custom-model"), "Setup bound the wrong connection or model");
    check(imports === 1, "Candidate imported more than once");
    check(page.url().endsWith("#/agent"), "Start conversation did not open the conversation");

    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 850 });
      await page.goto(`${origin}/#/settings`);
      await page.getByRole("tab", { name: "本地连接", exact: true }).click();
      await page.getByRole("button", { name: "重新发现", exact: true }).click();
      await page.getByRole("button", { name: "已添加", exact: true }).waitFor();
      check(await page.getByRole("button", { name: "已添加", exact: true }).isDisabled(), "Rediscovery allows duplicate import");
      check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "Settings overflows viewport");
      await page.screenshot({ path: `output/playwright/local-settings-${width}.png` });
    }

    holdScan = true;
    await reset();
    await page.getByRole("textbox", { name: "API Key", exact: true }).fill("pending-draft");
    check(pendingScans.length > 0, "Discovery did not start");
    holdScan = false;
    pendingScans.splice(0).forEach(resolve => resolve());
    await page.waitForFunction(() => !document.querySelector('[role="tab"] .spin'));
    check(await page.getByRole("tab", { name: "连接配置", exact: true }).getAttribute("aria-selected") === "true", "Late discovery interrupted manual entry");
    check(await page.getByRole("textbox", { name: "API Key", exact: true }).inputValue() === "pending-draft", "Late discovery discarded a draft");
    await page.getByRole("tab", { name: "连接配置", exact: true }).focus();
    await page.keyboard.press("ArrowLeft");
    check(await page.getByRole("tab", { name: "本地连接", exact: true }).getAttribute("aria-selected") === "true", "Tabs do not support keyboard selection");
    emptyScan = true;
    await reset();
    await page.waitForFunction(() => !document.querySelector('[role="tab"] .spin'));
    check(await page.getByRole("tab", { name: "连接配置", exact: true }).getAttribute("aria-selected") === "true", "Empty discovery did not select manual setup");
    failScan = true;
    await reset();
    await page.waitForFunction(() => !document.querySelector('[role="tab"] .spin'));
    check(await page.getByRole("tab", { name: "连接配置", exact: true }).getAttribute("aria-selected") === "true", "Failed discovery did not preserve manual setup");
    await page.getByRole("textbox", { name: "API Key", exact: true }).fill("manual-fixture-key");
    check(await page.getByRole("button", { name: "开始对话", exact: true }).isEnabled(), "Failed scan blocks manual setup");
    await page.getByRole("button", { name: "稍后配置", exact: true }).click();
    await page.locator(".system-setup").waitFor({ state: "detached" });
    await page.reload();
    await page.locator(".app-shell").waitFor();
    check(await page.locator(".system-setup").count() === 0, "Dismissal did not persist");
    return { viewports: [1440, 390], imports, roles: bindings.map(item => item.role), scanFailure: "manual entry and dismissal remain available" };
  } finally {
    holdScan = false;
    pendingScans.splice(0).forEach(resolve => resolve());
    await page.unrouteAll({ behavior: "wait" });
  }
}
