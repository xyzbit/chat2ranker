/** Real-page regression for shared selects, including native modal ownership. */
async (page) => {
  const check = (value, message) => { if (!value) throw new Error(message); };
  const origin = await page.evaluate(() => location.origin);
  const select = async (trigger, label) => {
    await trigger.click();
    await page.getByRole("option", { name: label, exact: true }).click();
    await page.waitForFunction(() => !document.querySelector(".ui-select-menu"));
  };
  await page.reload();
  await page.emulateMedia({ reducedMotion: "reduce" });
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto(`${origin}/#/settings`);
    const connection = page.getByRole("combobox", { name: "连接", exact: true }).first();
    const original = await connection.innerText();
    await connection.click();
    check(await page.getByRole("option", { name: "尚未配置", exact: true }).getAttribute("data-disabled") === "", "Empty required option must remain disabled");
    await page.screenshot({ path: `output/playwright/shared-settings-${width}.png` });
    await page.keyboard.press("Escape");
    await page.waitForFunction(() => document.activeElement?.getAttribute("aria-label") === "连接");
    check(await connection.innerText() === original, "Dismissing settings select changed its value");
    const style = await connection.evaluate(e => {
      const s = getComputedStyle(e);
      return { radius: s.borderRadius, height: s.minHeight, shadow: s.boxShadow };
    });
    check(style.radius === "6px" && style.height === "38px" && style.shadow === "none", "Shared control tokens overridden by page styles");
    await select(page.getByRole("combobox", { name: "语言", exact: true }), "English");
    await select(page.getByRole("combobox", { name: "Language", exact: true }), "简体中文");
    await page.getByRole("button", { name: "添加连接", exact: true }).click();
    await select(page.getByRole("combobox", { name: "Provider", exact: true }), "自定义 Provider");
    await select(page.getByRole("combobox", { name: "协议", exact: true }), "Responses");
    check(await page.getByRole("combobox", { name: "协议", exact: true }).innerText() === "Responses", "Protocol draft lost selection");
    await page.getByRole("button", { name: "取消", exact: true }).click();

    await page.goto(`${origin}/#/agents`);
    await page.getByRole("button", { name: "新建 Agent", exact: true }).click();
    const dialog = page.locator("dialog[open]");
    const runner = dialog.getByRole("combobox", { name: "运行方式", exact: true });
    await runner.click();
    check(await dialog.locator(".ui-select-menu").count() === 1, "Select menu must be inside the native dialog");
    await page.keyboard.press("End");
    await page.waitForFunction(() => document.activeElement === [...document.querySelectorAll('.ui-select-option:not([data-disabled])')].at(-1));
    await page.keyboard.press("Enter");
    await page.waitForFunction(() => !document.querySelector(".ui-select-menu"));
    await runner.click();
    await page.keyboard.press("Escape");
    check(await dialog.count() === 1, "Menu Escape must preserve parent dialog");
    await page.screenshot({ path: `output/playwright/shared-agent-modal-${width}.png` });
    await page.keyboard.press("Escape");
    check(await dialog.count() === 0, "Second Escape must dismiss parent dialog");

    await page.goto(`${origin}/#/deployments`);
    await page.getByRole("button", { name: "发布新版本", exact: true }).click();
    const route = dialog.getByRole("combobox", { name: "环境", exact: true });
    await select(route, "production");
    await route.click();
    await page.screenshot({ path: `output/playwright/shared-release-${width}.png` });
    await page.keyboard.press("Escape");
    await page.keyboard.press("Escape");
    await page.goto(`${origin}/#/responses`);
    const status = page.getByRole("combobox", { name: "状态", exact: true });
    await status.click();
    await page.keyboard.press("End");
    await page.waitForFunction(() => document.activeElement === [...document.querySelectorAll('.ui-select-option')].at(-1));
    await page.keyboard.press("Enter");
    await page.waitForFunction(() => !document.querySelector(".ui-select-menu"));
    await select(status, "全部状态");
    check(await status.innerText() === "全部状态", "Empty filter value must be selectable again");
    for (const route of ["agent", "agents", "deployments", "responses", "credentials", "settings"]) {
      await page.goto(`${origin}/#/${route}`);
      await page.locator("h1").first().waitFor();
      check(await page.locator('select:not([aria-hidden="true"])').count() === 0, `Unmigrated select on ${route}`);
      check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${route} overflows at ${width}`);
    }
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.emulateMedia({ reducedMotion: "no-preference" });
  return "PASS: settings, language, provider/protocol drafts, modal selects, nested Escape, empty filters, responsive pages and shared styling";
}
