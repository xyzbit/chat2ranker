import assert from "node:assert/strict";
import test from "node:test";

import { createTranslator, formatDateTime } from "../src/i18n.js";
import { managementCopy } from "../src/management-copy.js";

test("control-plane copy switches between Chinese and English", () => {
  assert.equal(createTranslator("zh")("releaseTo", { alias: "production" }), "发布到 production");
  assert.equal(createTranslator("en")("releaseTo", { alias: "production" }), "Release to production");
  assert.equal(createTranslator("en")("openDeployment"), "Open Deployment page");
  assert.equal(createTranslator("zh")("newConversation"), "新对话");
  assert.equal(createTranslator("en")("recentConversations"), "Recent chats");
  assert.equal(createTranslator("zh")("untitledConversation"), "新对话");
});

test("deployment timestamps honor the selected locale", () => {
  const value = "2026-09-09T08:30:00.000Z";
  assert.match(formatDateTime("en", value), /2026/);
  assert.equal(formatDateTime("zh", ""), "—");
});

test("management and companion copy has both locales with matching interpolation fields", () => {
  for (const [key, pair] of Object.entries(managementCopy)) {
    assert.equal(pair.length, 2, key);
    assert.ok(pair.every((text) => typeof text === "string" && text.trim()), key);
    const fields = (text) => [...text.matchAll(/\{(\w+)\}/g)].map((match) => match[1]).sort();
    assert.deepEqual(fields(pair[0]), fields(pair[1]), key);
  }
  const en = createTranslator("en");
  assert.equal(en("agentVersionTitle", { handle: "@team/helper" }), "New version of @team/helper");
  assert.equal(en("verifySave"), "Save and verify");
  assert.equal(en("documentTitle"), "AgentKiln — Agent platform");
});
