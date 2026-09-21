/** Browser smoke for deployment list navigation and shell language controls; no server writes. */
async (page) => {
  const check = (value, message) => { if (!value) throw new Error(message); };
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto("http://127.0.0.1:4173/#/deployments");
    await page.reload();
    const row = page.locator(".deployment-list-row").first();
    await row.waitFor();
    check(await page.locator(".deployment-heading").count() === 0, "List auto-selected a deployment");
    const name = await row.locator("strong").innerText();
    await page.screenshot({ path: `output/playwright/deployment-list-${width}.png` });
    await row.focus();
    await page.keyboard.press("Enter");
    await page.getByRole("heading", { name, exact: true }).waitFor();
    check(page.url().includes("deploymentId="), "Detail URL lost deployment selection");
    await page.reload();
    await page.getByRole("heading", { name, exact: true }).waitFor();
    await page.locator(".deployment-breadcrumb").click();
    await row.waitFor();
    check(await page.locator(".deployment-heading").count() === 0, "Back navigation did not restore list");
    const language = page.locator(width > 640 ? ".sidebar-account .locale-control" : ".product-topbar .locale-control");
    const label = await language.getAttribute("aria-label");
    await language.click();
    check(await language.getAttribute("aria-label") !== label, "Language toggle did not switch locale");
    await language.click();
    check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "Deployment list overflows viewport");
  }
  await page.setViewportSize({ width: 1440, height: 900 });
  return "PASS: deployment list, keyboard navigation, detail reload, back navigation, language toggle and mobile bounds";
}
