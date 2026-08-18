import assert from "node:assert/strict";
import test from "node:test";
import { parseImportedCases } from "../src/import-cases.js";

test("normalizes JSON arrays and cases envelopes", () => {
  const list = parseImportedCases("cases.json", JSON.stringify([{ id: 7, name: "Search", prompt: "Find sources", assertion: "has citations" }]));
  const envelope = parseImportedCases("cases.json", JSON.stringify({ cases: ["Open the page"] }));
  assert.deepEqual(list[0], { id: "7", title: "Search", input: "Find sources", expected: "has citations" });
  assert.equal(envelope[0].input, "Open the page");
});

test("normalizes JSONL with line-specific validation feedback", () => {
  const cases = parseImportedCases("cases.jsonl", '{"title":"A","task":"Run A"}\n{"title":"B","input":"Run B"}');
  assert.deepEqual(cases.map((item) => item.input), ["Run A", "Run B"]);
  assert.throws(() => parseImportedCases("cases.jsonl", '{"input":"ok"}\nnot-json'), /第 2 行/);
});

test("parses quoted CSV fields", () => {
  const cases = parseImportedCases("cases.csv", 'title,input,expected\n"Search, then summarize","Find A, B and C","has citations"');
  assert.equal(cases[0].title, "Search, then summarize");
  assert.equal(cases[0].input, "Find A, B and C");
  assert.equal(cases[0].expected, "has citations");
});

test("turns non-empty TXT lines into cases and rejects invalid imports", () => {
  const cases = parseImportedCases("cases.txt", "Open page\n\nSearch docs");
  assert.deepEqual(cases.map((item) => item.input), ["Open page", "Search docs"]);
  assert.throws(() => parseImportedCases("cases.txt", "  \n"), /没有识别到用例/);
  assert.throws(() => parseImportedCases("cases.yaml", "input: hello"), /仅支持/);
  assert.throws(() => parseImportedCases("cases.json", '[{"title":"missing input"}]'), /缺少 input/);
});
