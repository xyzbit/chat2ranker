import assert from "node:assert/strict";
import test from "node:test";
import { filterMentionItems, mentionSearchQuery } from "../src/mention-search.js";

test("extracts and filters the active mention query", () => {
  assert.equal(mentionSearchQuery("参考 @dsh/pro"), "dsh/pro");
  const agents = [
    { handle: "@dsh/prompt-optimizer", runnerType: "dsh", model: "deepseek-chat" },
    { handle: "@codex/reviewer", runnerType: "codex", model: "gpt-5" },
  ];
  assert.deepEqual(filterMentionItems(agents, "dsh/pro", (item) => [item.handle, item.runnerType, item.model]), [agents[0]]);
  assert.deepEqual(filterMentionItems(agents, "gpt-5", (item) => [item.handle, item.runnerType, item.model]), [agents[1]]);
});
