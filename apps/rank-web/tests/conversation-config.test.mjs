import assert from "node:assert/strict";
import test from "node:test";
import { agentConversationPrompt, experimentTitleFromMessage } from "../src/conversation-config.js";

test("Agent handoff uses names instead of JSON and internal ids", () => {
  const prompt = agentConversationPrompt({ runnerType: "dsh", model: "deepseek-chat", modelConnectionId: "conn-secret", preset: "headless", description: "", systemPrompt: "", tools: ["web_search"], skills: [] }, { handle: "@dsh/research", version: 1, runnerType: "dsh", model: "", modelConnectionId: "", preset: "headless", description: "", systemPrompt: "", tools: [], skills: [] }, [{ id: "conn-secret", name: "团队 DeepSeek" }]);
  assert.match(prompt, /@dsh\/research v1/);
  assert.match(prompt, /团队 DeepSeek/);
  assert.doesNotMatch(prompt, /conn-secret|\{|\}/);
  assert.doesNotMatch(prompt, /Preset|headless/);
});

test("Agent handoff asks only for missing essentials", () => {
  assert.equal(agentConversationPrompt({ runnerType: "dsh", name: "", handle: "" }), "我想创建一个 Agent，目前只缺名称和Handle。请只询问这些必要信息；其他配置暂定 Runner 为 dsh。");
});

test("Experiment titles derive from the first meaningful user request", () => {
  assert.equal(experimentTitleFromMessage("请用 @dsh/research 比较图片提示词优化。后续再看成本"), "请用 比较图片提示词优化");
  assert.equal(experimentTitleFromMessage("创建 Agent：名称为 E2E 对话 Agent，Handle 为 @e2e/test"), "E2E 对话 Agent 实验");
});
