/** Playwright CLI regression against the running Rank assembly; no backend writes. */
async (page) => {
  const check = (value, message) => { if (!value) throw new Error(message); };
  const origin = await page.evaluate(() => location.origin);
  await page.reload();
  await page.emulateMedia({ reducedMotion: "reduce" });
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    for (const route of ["agent", "agents", "deployments", "responses", "credentials", "settings"]) {
      await page.goto(`${origin}/#/${route}`);
      await page.locator("h1").first().waitFor();
      check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `${route} overflows at ${width}px`);
      await page.screenshot({ path: `output/playwright/ui-${route}-${width}.png` });
    }
    await page.goto(`${origin}/#/agents`);
    const create = page.getByRole("button", { name: "新建 Agent", exact: true });
    await create.click();
    const dialog = page.locator("dialog[open]");
    await dialog.waitFor();
    for (let i = 0; i < 25; i++) {
      await page.keyboard.press("Tab");
      check(await dialog.evaluate(e => e.matches(":modal") && (e.contains(document.activeElement) || document.activeElement === document.body)), "Modal focus entered background content");
    }
    await page.screenshot({ path: `output/playwright/ui-modal-${width}.png` });
    await page.keyboard.press("Escape");
    check(await dialog.count() === 0, "Escape must close modal");
    check(await create.evaluate(e => e === document.activeElement), "Modal must restore trigger focus");
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto(`${origin}/#/agent`);
  const region = page.locator(".conversation.scroll-region");
  await region.waitFor();
  await region.evaluate(e => { e.scrollTop = e.scrollHeight; });
  await page.waitForFunction(() => document.querySelector(".conversation").dataset.scrollBottom === "false");
  await page.waitForTimeout(120);
  await region.evaluate(e => {
    const fixture = document.createElement("div");
    fixture.dataset.scrollFixture = "true";
    fixture.style.height = "2400px";
    fixture.textContent = "Scroll verification";
    e.append(fixture);
  });
  await page.waitForFunction(() => {
    const e = document.querySelector(".conversation");
    return e.dataset.scrollTop === "true" && e.dataset.scrollBottom === "false";
  });
  await region.evaluate(e => { e.scrollTop = 0; });
  await page.waitForFunction(() => document.querySelector(".conversation").dataset.scrollBottom === "true");
  await region.focus();
  await page.keyboard.press("PageDown");
  await page.waitForFunction(() => document.querySelector(".conversation").scrollTop > 20);
  await page.waitForTimeout(400);
  await region.evaluate(e => { e.scrollTop = 100; });
  await page.waitForFunction(() => document.querySelector(".conversation").scrollTop === 100);
  await region.evaluate(e => { e.querySelector("[data-scroll-fixture]").style.height = "2800px"; });
  await page.waitForTimeout(120);
  check(await region.evaluate(e => Math.abs(e.scrollTop - 100) < 2), "New content moved a reader who scrolled up");
  await region.evaluate(e => { e.scrollTop = e.scrollHeight; });
  await page.waitForFunction(() => document.querySelector(".conversation").dataset.scrollBottom === "false");
  await region.evaluate(e => { e.querySelector("[data-scroll-fixture]").style.height = "3200px"; });
  await page.waitForFunction(() => {
    const e = document.querySelector(".conversation");
    return e.scrollHeight - e.clientHeight - e.scrollTop < 2;
  });
  await page.reload();
  await page.goto(`${origin}/#/settings`);
  await page.getByRole("button", { name: "添加连接", exact: true }).click();
  const form = page.locator(".management-form");
  const trigger = form.getByRole("button", { name: "模型", exact: true });
  await trigger.click();
  const search = form.getByRole("combobox", { name: "模型", exact: true });
  await search.fill("deepseek");
  await search.press("ArrowDown");
  const active = await search.getAttribute("aria-activedescendant");
  check(Boolean(active), "Model search needs active option");
  await search.press("Enter");
  check(await page.locator(":popover-open").count() === 0, "Selecting must close menu");
  await trigger.click();
  await search.press("Escape");
  check(await form.isVisible(), "Menu Escape closed the connection form");
  check(await search.evaluate(e => e === document.activeElement), "Menu must retain input focus");
  await page.setViewportSize({ width: 390, height: 844 });
  await trigger.click();
  await page.waitForFunction(() => {
    const rect = document.querySelector(":popover-open")?.getBoundingClientRect();
    return rect && rect.x >= 0 && rect.right <= innerWidth && rect.y >= 0 && rect.bottom <= innerHeight;
  });
  const bounds = await page.locator(":popover-open").boundingBox();
  check(bounds.x >= 0 && bounds.x + bounds.width <= 391 && bounds.y >= 0 && bounds.y + bounds.height <= 845, "Model menu clips viewport");
  await page.screenshot({ path: "output/playwright/ui-model-menu-mobile.png" });
  await search.press("Escape");
  await form.getByRole("button", { name: "取消", exact: true }).click();
  await page.goto(`${origin}/#/settings`);
  await page.getByRole("combobox", { name: "语言", exact: true }).click();
  await page.getByRole("option", { name: "English", exact: true }).click();
  for (const route of ["agent", "agents", "deployments", "responses", "credentials", "settings"]) {
    await page.goto(`${origin}/#/${route}`);
    await page.locator("h1").first().waitFor();
    check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `English ${route} overflows`);
  }
  await page.getByRole("combobox", { name: "Language", exact: true }).click();
  await page.getByRole("option", { name: "简体中文", exact: true }).click();
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.emulateMedia({ reducedMotion: "no-preference" });
  return "PASS: six routes, desktop/mobile, English, modal focus/Escape, scroll fades, keyboard scroll, preserved reading position, tail following, model keyboard/placement";
}
