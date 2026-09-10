/** Playwright CLI smoke for a running, configured Rank conversation; no model calls. */
async (page) => {
  await page.reload();
  const check = (value, message) => { if (!value) throw new Error(message); };
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.emulateMedia({ reducedMotion: "no-preference" });
  const origin = await page.evaluate(() => location.origin);
  await page.goto(`${origin}/#/agents`);
  const launcher = page.locator(".companion-launcher");
  await launcher.waitFor({ state: "visible" });
  const signal = launcher.locator(".rank-signal");
  const signalBox = await signal.boundingBox();
  check(signalBox.width === 62 && signalBox.height === 62, "Floating companion must stay 62px square");
  await page.mouse.move(signalBox.x - 60, signalBox.y);
  await page.waitForFunction(() => document.querySelector(".rank-signal").dataset.following === "true");
  await page.screenshot({ path: "output/playwright/rank-companion-desktop.png" });
  await page.mouse.move(signalBox.x - 220, signalBox.y);
  await page.waitForFunction(() => !document.querySelector(".rank-signal").dataset.following);
  await page.setViewportSize({ width: 390, height: 844 });
  const mobileSignal = await signal.boundingBox();
  check(mobileSignal.width === 54 && mobileSignal.height === 54, "Mobile companion must stay 54px square");
  await page.screenshot({ path: "output/playwright/rank-companion-mobile.png" });
  await launcher.click();
  await page.locator(".assistant-route .conversation").waitFor({ state: "visible" });
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto(`${origin}/#/settings`);
  await page.reload();
  const preview = page.locator(".companion-preview .rank-signal");
  await preview.waitFor({ state: "visible" });
  await preview.hover();
  check(!await preview.getAttribute("data-following"), "Settings preview must not track the pointer");
  await page.goto(`${origin}/#/agent`);
  const avatar = page.locator(".avatar-assistant").first();
  await avatar.waitFor({ state: "visible" });
  await avatar.scrollIntoViewIfNeeded();
  await page.mouse.move(1200, 720);
  const initial = await avatar.boundingBox();
  check(initial.width === 42 && initial.height === 42, "Desktop avatar must stay 42px square");
  const face = await avatar.evaluate((element) => ({
    color: getComputedStyle(element).backgroundColor,
    eyes: element.querySelectorAll(".assistant-eyes > span").length,
    hidden: element.getAttribute("aria-hidden"),
  }));
  check(face.color === "rgb(39, 155, 250)" && face.eyes === 2 && face.hidden === "true", "Expected decorative blue face with two eyes");
  const glances = await avatar.evaluate((element) => {
    const eyes = element.querySelector(".assistant-eyes");
    const animation = eyes.getAnimations()[0];
    animation.pause();
    const positions = [0, 3600, 8400].map((time) => {
      animation.currentTime = time;
      return new DOMMatrixReadOnly(getComputedStyle(eyes).transform).m41;
    });
    animation.currentTime = 0;
    animation.play();
    return positions;
  });
  check(glances[0] === 0 && glances[1] < -2 && glances[2] > 2, "Idle gaze must look forward, left, and right");

  const x = initial.x + initial.width / 2;
  const y = initial.y + initial.height / 2;
  for (const direction of [-1, 1]) {
    await page.mouse.move(x + direction * 70, y - 30);
    await page.waitForFunction(({ direction }) => {
      const avatar = document.querySelector(".avatar-assistant");
      return avatar.dataset.following === "true" && Number.parseFloat(avatar.style.getPropertyValue("--gaze-x")) * direction > 3;
    }, { direction });
    const gaze = await avatar.locator(".assistant-gaze").evaluate((element) => getComputedStyle(element).transform);
    check(gaze !== "none", "Nearby mouse must move the gaze");
  }
  const following = await avatar.boundingBox();
  check(JSON.stringify(initial) === JSON.stringify(following), "Tracking must not move or resize the avatar");
  await page.screenshot({ path: "output/playwright/rank-assistant-desktop.png" });
  await page.mouse.move(x + 220, y);
  await page.waitForFunction(() => !document.querySelector(".avatar-assistant").dataset.following);
  await avatar.dispatchEvent("pointermove", { pointerType: "touch", clientX: x + 60, clientY: y });
  check(!await avatar.getAttribute("data-following"), "Touch must not activate tracking");
  await page.mouse.move(x + 70, y);
  await page.waitForFunction(() => document.querySelector(".avatar-assistant").dataset.following === "true");
  await page.evaluate(() => window.dispatchEvent(new Event("blur")));
  check(!await avatar.getAttribute("data-following"), "Losing focus must release tracking");
  await page.mouse.move(x - 70, y);
  await page.waitForFunction(() => document.querySelector(".avatar-assistant").dataset.following === "true");
  await page.evaluate(() => window.dispatchEvent(new Event("scroll")));
  check(!await avatar.getAttribute("data-following"), "Scrolling must release tracking");

  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.mouse.move(x + 70, y);
  await page.waitForFunction(() => document.querySelector(".assistant-eyes").getAnimations().length === 0);
  check(!await avatar.getAttribute("data-following"), "Reduced motion must disable pointer tracking");
  check(await avatar.evaluate((element) => element.getAnimations({ subtree: true }).length) === 0, "Reduced motion must stop all face animations");
  await page.setViewportSize({ width: 390, height: 844 });
  await avatar.scrollIntoViewIfNeeded();
  const mobile = await avatar.boundingBox();
  check(mobile.width === 35 && mobile.height === 35, "Mobile avatar must stay 35px square");
  check(mobile.x >= 0 && mobile.x + mobile.width <= 390, "Mobile avatar must remain in view");
  await page.screenshot({ path: "output/playwright/rank-assistant-mobile.png" });
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.emulateMedia({ reducedMotion: "no-preference" });
  await page.mouse.move(1200, 720);
  await page.waitForFunction(() => document.querySelector(".assistant-eyes").getAnimations().length === 1);
  console.log("PASS: blue face, idle left/right gaze, nearby tracking, distant/touch rejection, stable layout, blur/scroll reset, reduced motion, mobile layout, motion restoration");
}
