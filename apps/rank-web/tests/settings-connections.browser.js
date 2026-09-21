async (page) => {
  const check = (value, message) => { if (!value) throw new Error(message); };
  const origin = await page.evaluate(() => location.origin);
  const connection = { id: "settings-fixture", name: "DSH · Test connection", provider: "custom", protocol: "openai-responses", baseUrl: "https://fixture.example/v1", defaultModel: "model-a", models: ["model-a", "model-b"], status: "verified", hasCredential: true };
  const writes = [];
  await page.route("**/api/bootstrap**", async route => {
    const response = await route.fetch();
    const data = await response.json();
    await route.fulfill({ json: { ...data, modelConnections: [connection], systemModels: ["control", "judge"].map(role => ({ role, connectionId: connection.id, model: "model-a", connection })) } });
  });
  await page.route("**/api/local-model-connections/discover", route => route.fulfill({ json: { candidates: [{ id: "candidate-fixture", source: "DSH", name: connection.name, model: "model-a", baseUrl: connection.baseUrl, connectionId: connection.id }], issues: [] } }));
  await page.route("**/api/system-models/*", route => { writes.push(route.request().postDataJSON()); return route.fulfill({ json: {} }); });
  try {
    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 900 });
      await page.goto(`${origin}/#/settings`);
      await page.reload();
      const saved = page.getByRole("tab", { name: "模型连接", exact: true });
      const local = page.getByRole("tab", { name: "本地连接", exact: true });
      await saved.waitFor();
      check(await saved.getAttribute("aria-selected") === "true", "Saved connections are not the default tab");
      check(await page.getByRole("button", { name: "重新发现", exact: true }).isVisible() === false, "Local discovery is visible in the saved tab");
      await local.click();
      await page.getByRole("button", { name: "重新发现", exact: true }).click();
      await page.getByRole("button", { name: "已添加", exact: true }).waitFor();
      await page.screenshot({ path: `output/playwright/settings-local-tabs-${width}.png` });
      await saved.click();

      const control = page.locator(".binding-form").first();
      const model = control.getByRole("combobox", { name: "模型", exact: true });
      await control.getByRole("button", { name: "模型", exact: true }).click();
      await page.getByRole("option", { name: "model-b", exact: true }).click();
      check(await model.inputValue() === "model-b", "Dropdown did not update the model");
      await model.fill("custom-settings-model");
      await model.press("Escape");
      await control.getByRole("button", { name: "保存", exact: true }).click();
      await page.getByText("已保存", { exact: true }).waitFor();
      check(writes.at(-1).model === "custom-settings-model", "Custom model did not reach the binding API");
      await control.getByRole("button", { name: "模型", exact: true }).click();
      await model.press("ArrowDown");
      await model.press("Enter");
      check(await model.inputValue() === "model-a", "Keyboard selection did not update the model");
      check(await page.evaluate(() => [...document.querySelectorAll(".settings-page > .management-section")].every(el => getComputedStyle(el).borderTopWidth === "0px")), "Settings retained section dividers");
      check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "Settings overflow horizontally");
      await page.screenshot({ path: `output/playwright/settings-clean-${width}.png`, fullPage: true });

      await page.getByRole("button", { name: "添加连接", exact: true }).click();
      const form = page.locator(".management-form");
      const editor = form.getByRole("combobox", { name: "模型", exact: true });
      await editor.fill("draft-model");
      await editor.press("Escape");
      await local.click();
      await saved.click();
      check(await editor.inputValue() === "draft-model", "Connection tab switch discarded the editor draft");
      await form.getByRole("button", { name: "模型", exact: true }).click();
      check(await page.getByRole("listbox", { name: "模型", exact: true }).isVisible(), "Editor dropdown did not open");
      check(await page.getByRole("listbox", { name: "模型", exact: true }).evaluate(el => { const r = el.getBoundingClientRect(); return r.left >= 0 && r.right <= innerWidth && r.top >= 0 && r.bottom <= innerHeight; }), "Model menu leaves viewport");
      await editor.press("Escape");
      await form.getByRole("button", { name: "取消", exact: true }).click();

      await page.goto(`${origin}/#/agents`);
      await page.getByRole("button", { name: "新建 Agent", exact: true }).click();
      const dialog = page.getByRole("dialog", { name: "新建 Agent", exact: true });
      await dialog.getByRole("combobox", { name: "模型来源", exact: true }).click();
      await page.getByRole("option", { name: /DSH · Test connection/ }).click();
      const agentModel = dialog.getByRole("combobox", { name: "使用模型", exact: true });
      await dialog.getByRole("button", { name: "使用模型", exact: true }).click();
      await page.getByRole("option", { name: "model-b", exact: true }).click();
      check(await agentModel.inputValue() === "model-b", "Agent model suggestions cannot be selected inside the dialog");
      await agentModel.fill("custom-agent-model");
      await agentModel.press("Escape");
      check(await dialog.isVisible() && await agentModel.inputValue() === "custom-agent-model", "Dismissing model suggestions closed the Agent editor or discarded its model");
      await agentModel.fill("");
      await agentModel.press("Tab");
      check(await agentModel.evaluate(el => el.validity.valid && !el.required), "Agent model cannot use the runtime default");
      await dialog.getByRole("button", { name: "关闭", exact: true }).click();
    }
    return { viewports: [1440, 390], tabs: "saved/local", modelEntry: "dropdown, custom input, keyboard", writes: writes.length };
  } finally { await page.unrouteAll({ behavior: "wait" }); }
}
