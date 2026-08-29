import assert from "node:assert/strict";
import test from "node:test";
import { messageContentToMarkdown, parseMessageContent } from "../src/message-format.js";

test("parses Rank Message JSONL and exports Markdown", () => {
  const content = [
    JSON.stringify({ type: "summary", text: "运行准备完成" }),
    JSON.stringify({ type: "facts", items: [{ label: "Agent", value: "@dsh/research v2" }, { label: "用例", value: "20" }] }),
    JSON.stringify({ type: "list", ordered: true, items: ["确认配置", "开始运行"] }),
  ].join("\n");
  const parsed = parseMessageContent(content);
  assert.equal(parsed.format, "rank-jsonl");
  assert.deepEqual(parsed.blocks.map((block) => block.type), ["summary", "facts", "list"]);
  assert.equal(messageContentToMarkdown(content), "**运行准备完成**\n\n- **Agent：** @dsh/research v2\n- **用例：** 20\n\n1. 确认配置\n2. 开始运行");
});

test("keeps incomplete streaming JSON out of the visible message", () => {
  const parsed = parseMessageContent('{"type":"summary","text":"完成"}\n{"type":"paragraph","text":"仍在', { streaming: true });
  assert.equal(parsed.format, "rank-jsonl");
  assert.deepEqual(parsed.blocks, [{ type: "summary", text: "完成" }]);
});

test("renders existing Markdown messages through the legacy fallback", () => {
  const content = "配置已就绪：\n\n- **测试集**：基准集 v1\n- **Agent**：@dsh/research v2\n\n| Agent | 通过率 |\n|---|---|\n| v2 | 93% |";
  const parsed = parseMessageContent(content);
  assert.equal(parsed.format, "legacy-markdown");
  assert.deepEqual(parsed.blocks.map((block) => block.type), ["paragraph", "list", "table"]);
  assert.equal(messageContentToMarkdown(content), content);
});

test("parses durable control errors and exports their detail", () => {
  const content = JSON.stringify({ type: "error", text: "这次处理没有完成，请重试。", detail: "tool scheduler unavailable" });
  assert.deepEqual(parseMessageContent(content).blocks, [{ type: "error", text: "这次处理没有完成，请重试。", detail: "tool scheduler unavailable" }]);
  assert.equal(messageContentToMarkdown(content), "> 这次处理没有完成，请重试。\n>\n> 技术信息：tool scheduler unavailable");
});
