async (page) => {
  const check = (value, message) => { if (!value) throw new Error(message); };
  const origin = await page.evaluate(() => location.origin);
  const endpoint = "demo-integration";
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.evaluate(() => { localStorage.setItem("rank.modelSetupDismissed", "true"); localStorage.setItem("rank.locale", "zh"); });
  await page.goto(`${origin}/#/deployments?deploymentId=deploy_01a098f3-cfcc-7459-8128-e24550ee797b`);
  await page.reload();
  await page.getByRole("button", { name: "接入", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "接入", exact: true });
  await dialog.getByRole("button", { name: "下载每日热点", exact: true }).waitFor();
  for (const [kind, label] of [["news", "下载每日热点"], ["chat", "下载聊天助手"]]) {
    const downloadPromise = page.waitForEvent("download");
    await dialog.getByRole("button", { name: label, exact: true }).click();
    const download = await downloadPromise;
    check(download.suggestedFilename() === `rank-${kind}.zip`, "Wrong download name");
    await download.saveAs(`output/playwright/rank-${kind}.zip`);
  }
  const href = await dialog.getByRole("link", { name: "完整接入文档", exact: true }).getAttribute("href");
  check(href.includes("endpoint=demo-integration"), "Documentation lost endpoint");
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await dialog.getByRole("button", { name: "下载聊天助手", exact: true }).scrollIntoViewIfNeeded();
    check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "Drawer overflow");
    await page.screenshot({ path: `output/playwright/example-download-${width}.png`, fullPage: true });
  }
  await page.goto(origin + "/" + href);
  await page.getByRole("heading", { name: "快速接入", exact: true }).waitFor();
  check((await page.locator(".documentation-page > article").innerText()).includes(endpoint), "Quickstart uses a placeholder");
  await page.locator(".documentation-page > nav").getByRole("link", { name: "完整案例", exact: true }).click();
  await page.getByRole("button", { name: "下载聊天助手", exact: true }).waitFor();
  check(page.url().includes("endpoint=demo-integration"), "Topic navigation lost endpoint");
  for (const width of [1440, 390]) {
    await page.setViewportSize({ width, height: 900 });
    check(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "Docs overflow");
    await page.screenshot({ path: `output/playwright/example-docs-${width}.png`, fullPage: true });
  }
  await page.getByRole("link", { name: "错误排查", exact: true }).click();
  await page.getByRole("heading", { name: "错误排查", exact: true }).waitFor();
  check((await page.locator(".documentation-page > article").innerText()).includes("通常没有 Response ID"), "Pre-acceptance errors unexplained");
  check(errors.length === 0, errors.join("\n"));
  return { downloads: ["rank-news.zip", "rank-chat.zip"], endpoint, viewports: [1440, 390], pageErrors: errors };
}
