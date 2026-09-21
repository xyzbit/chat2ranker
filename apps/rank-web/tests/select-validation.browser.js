/** Browser-only bootstrap variants verify empty required values and long option lists. */
async (page) => {
  const check = (v, message) => { if (!v) throw new Error(message); };
  await page.evaluate(() => localStorage.setItem("rank.modelSetupDismissed", "true"));
  await page.route("**/api/bootstrap", async route => {
    const response = await route.fetch();
    const json = await response.json();
    json.systemModels = [];
    json.deployments = [...json.deployments, ...Array.from({ length: 60 }, (_, i) => ({ ...json.deployments[0], id: `ui-fixture-${i}`, name: `Long-deployment-name-for-scroll-verification-${i}` }))];
    await route.fulfill({ response, json });
  });
  try {
    await page.goto("http://127.0.0.1:4173/#/settings");
    await page.reload();
    const field = page.locator(".binding-form").first();
    const connection = field.getByRole("combobox", { name: "连接", exact: true });
    await connection.waitFor();
    check(await field.locator("select").evaluate(e => e.validity.valueMissing), "Required empty selection must be invalid");
    await connection.click();
    await page.locator('.ui-select-option:not([data-disabled])').first().click();
    await page.waitForFunction(() => !document.querySelector(".ui-select-menu"));
    check(await field.locator("select").evaluate(e => e.checkValidity()), "Selected connection must satisfy required validation");
    check(await field.getByRole("combobox", { name: "模型", exact: true }).inputValue() !== "", "Connection choice must populate model draft");
    await page.goto("http://127.0.0.1:4173/#/responses");
    await page.setViewportSize({width:390,height:844});
    await page.getByRole("combobox", { name: "部署", exact: true }).click();
    await page.keyboard.press("End");
    await page.waitForFunction(() => document.activeElement === [...document.querySelectorAll('.ui-select-option')].at(-1));
    const menu = await page.locator(".ui-select-menu").boundingBox();
    const last = await page.locator(".ui-select-option").last().boundingBox();
    check(menu.height <= 302 && menu.x >= 0 && menu.x + menu.width <= 390, "Long menu must respect shared height and viewport limits");
    check(last.y >= menu.y && last.y + last.height <= menu.y + menu.height, "Keyboard navigation must scroll last option into view");
    await page.keyboard.press("Escape");
  } finally {
    await page.unroute("**/api/bootstrap");
    await page.setViewportSize({width:1440,height:900});
    await page.reload();
  }
  return "PASS: required empty selection, model draft propagation, long menu scrolling and mobile bounds";
}
