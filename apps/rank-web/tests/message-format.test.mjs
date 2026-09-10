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

test("isolates an empty duplicate list field without exposing JSON or losing adjacent content", () => {
  const content = '{"type":"summary","text":"Ready"}\n{"type":"list","items":["Review"],"items":[]}\n{"type":"note","text":"No changes"}';
  const parsed=parseMessageContent(content);
  assert.equal(parsed.format,"rank-jsonl");
  assert.deepEqual(parsed.blocks.map((v)=>v.type),["summary","error","note"]);
  assert.equal(parsed.blocks[1].formatError,true);
  assert.equal(parsed.blocks[2].text,"No changes");
  assert.ok(!messageContentToMarkdown(content).includes('"items"'));
});

test("streams complete adjacent objects while withholding the unfinished object", () => {
  assert.deepEqual(parseMessageContent('{"type":"summary","text":"Ready"}{"type":"list","items":[', {streaming:true}).blocks,[{type:"summary",text:"Ready"}]);
});

test("repairs common model-generated JSON mistakes", () => {
  const content = [
    '{"type":"summary","text":"整理"热点/非热点"测试集",}',
    '{\\"type\\":\\"paragraph\\",\\"text\\":\\"继续 C:\\project 配置\\"}]',
  ].join("\n");
  assert.deepEqual(parseMessageContent(content), {
    format: "rank-jsonl",
    blocks: [
      { type: "summary", text: '整理"热点/非热点"测试集' },
      { type: "paragraph", text: "继续 C:\\project 配置" },
    ],
  });
});

test("parses adjacent Rank Message objects when a model omits a newline", () => {
  const content = '{"type":"summary","text":"已读取"}{"type":"note","tone":"info","text":"未启动运行"}';
  assert.deepEqual(parseMessageContent(content), {
    format: "rank-jsonl",
    blocks: [
      { type: "summary", text: "已读取" },
      { type: "note", tone: "info", text: "未启动运行" },
    ],
  });
});

test("renders existing Markdown messages through the legacy fallback", () => {
  const content = "配置已就绪：\n\n- **测试集**：基准集 v1\n- **Agent**：@dsh/research v2\n\n| Agent | 通过率 |\n|---|---|\n| v2 | 93% |";
  const parsed = parseMessageContent(content);
  assert.equal(parsed.format, "legacy-markdown");
  assert.deepEqual(parsed.blocks.map((block) => block.type), ["paragraph", "list", "table"]);
  assert.equal(messageContentToMarkdown(content), content);
});

test("renders a plain-text preface followed by Rank JSONL without exposing protocol text", () => {
  const content = '卡片已准备好。\n\n{"type":"summary","text":"等待确认"}\n{"type":"facts","items":[{"label":"Agent","value":"v2"}]}\n{"type":"note","text":"未执行"}';
  const parsed = parseMessageContent(content);
  assert.equal(parsed.format, "mixed");
  assert.deepEqual(parsed.blocks.map((block) => block.type), ["paragraph", "summary", "facts", "note"]);
  assert.equal(messageContentToMarkdown(content), "卡片已准备好。\n\n**等待确认**\n\n- **Agent：** v2\n\n> 未执行");
  assert.deepEqual(parseMessageContent('卡片已准备好。\n{"type":"summary","text":"等待', { streaming: true }).blocks, [{ type: "paragraph", text: "卡片已准备好。" }]);
});

test("keeps intentional code fences and non-protocol JSON in legacy replies", () => {
  const content = 'Example:\n\n```json\n{"type":"summary","text":"sample"}\n```\n\n{"type":"invoice","total":8}';
  const parsed = parseMessageContent(content);
  assert.equal(parsed.format, "legacy-markdown");
  assert.deepEqual(parsed.blocks.map((block) => block.type), ["paragraph", "code", "paragraph"]);
  assert.equal(messageContentToMarkdown(content), content);
});

test("parses durable control errors and exports their detail", () => {
  const content = JSON.stringify({ type: "error", text: "这次处理没有完成，请重试。", detail: "tool scheduler unavailable" });
  assert.deepEqual(parseMessageContent(content).blocks, [{ type: "error", text: "这次处理没有完成，请重试。", detail: "tool scheduler unavailable" }]);
  assert.equal(messageContentToMarkdown(content), "> 这次处理没有完成，请重试。\n>\n> 技术信息：tool scheduler unavailable");
});
