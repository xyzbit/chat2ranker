import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { createClient, requireCompleted, textOf } from "./demo-client.mjs";
import { createDemoServer } from "./demo-server.mjs";

const client = createClient();
const server = createDemoServer({ client, model: process.env.RANK_MODEL });
await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
const base = `http://127.0.0.1:${server.address().port}`;
const evidence = { checkedAt: new Date().toISOString(), model: process.env.RANK_MODEL, checks: [] };
try {
  const init = await fetch(base + "/api/state");
  const cookie = init.headers.get("set-cookie").split(";")[0];
  async function request(path, body) {
    const response = await fetch(base + path, { method: body === undefined ? "GET" : "POST", headers: { cookie, ...(body === undefined ? {} : { "Content-Type": "application/json" }) }, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(190_000) });
    if (!response.ok) throw new Error(`${path}: HTTP ${response.status} ${await response.text()}`);
    return response;
  }
  async function chat(input) {
    const response = await request("/api/chat", { input });
    const events = (await response.text()).trim().split("\n").map(JSON.parse);
    assert.ok(!events.some(event => event.type === "error"), JSON.stringify(events));
    const result = events.find(event => event.type === "completed")?.result;
    assert.ok(result);
    assert.equal(events.filter(event => event.type === "delta").map(event => event.text).join(""), result.text);
    const stored = requireCompleted(await client.responses.retrieve(result.id));
    assert.equal(textOf(stored), result.text);
    return result;
  }
  evidence.news = await (await request("/api/news", {})).json();
  assert.equal(evidence.news.status, "completed");
  assert.ok(evidence.news.sources.length > 0);
  assert.match(evidence.news.text, /\[[1-9]\d?\]/);
  const storedNews = requireCompleted(await client.responses.retrieve(evidence.news.id));
  assert.equal(textOf(storedNews), evidence.news.text);
  evidence.checks.push("Live news source, one synchronous Responses creation, stored output and numbered citations");
  console.log("PASS: live daily-news request", evidence.news.id);

  const first = await chat("我叫小林，喜欢科技新闻，每天有10分钟。请记住这三点，只用一句话确认。");
  const second = await chat("我叫什么，喜欢什么，每天多久？给我一个简短安排。");
  assert.match(second.text, /小林/); assert.match(second.text, /科技/); assert.match(second.text, /10|十/);
  assert.equal(second.deployment.agent_version_id, first.deployment.agent_version_id);
  evidence.chat = [first, second];
  const restored = await (await request("/api/state")).json();
  assert.equal(restored.messages.length, 4);
  assert.equal(restored.messages.at(-1).id, second.id);
  const other = await (await fetch(base + "/api/state")).json();
  assert.equal(other.messages.length, 0);
  evidence.checks.push("Two-turn real-model recall, SSE text, retrieval, frozen Agent version, reload and visitor isolation");
  console.log("PASS: chat, recall, persistence and isolation", second.id);

  const running = await request("/api/chat", { input: "写一篇详细的科技阅读方法，分20个章节，每章200字。" });
  await request("/api/cancel", {});
  const cancelled = (await running.text()).trim().split("\n").map(JSON.parse);
  const cancelledEvent = cancelled.find(event => event.type === "error" && event.status === "cancelled");
  assert.ok(cancelledEvent, JSON.stringify(cancelled));
  assert.equal((await client.responses.retrieve(cancelledEvent.responseId)).status, "cancelled");
  assert.equal((await (await request("/api/state")).json()).broken, true);
  await request("/api/reset", {});
  assert.equal((await (await request("/api/state")).json()).messages.length, 0);
  evidence.checks.push("Cancellation reaches Rank and a new conversation clears the failed continuation");
  console.log("PASS: cancellation and new conversation");
  const directory = new URL("./output/", import.meta.url);
  await mkdir(directory, { recursive: true });
  await writeFile(new URL("integration-demos-live.json", directory), JSON.stringify(evidence, null, 2) + "\n");
  console.log("Evidence: output/integration-demos-live.json");
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
} finally {
  await new Promise(resolve => { server.close(resolve); server.closeAllConnections(); });
}
