import assert from "node:assert/strict";
import { createServer } from "node:http";
import { readFile, writeFile } from "node:fs/promises";
import test from "node:test";
import OpenAI from "openai";
import { createDemoServer } from "./demo-server.mjs";
import { dailyNews } from "./demo-client.mjs";

async function listen(t, server) {
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  t.after(() => new Promise(resolve => { server.close(resolve); server.closeAllConnections(); }));
  return `http://127.0.0.1:${server.address().port}`;
}

// The real SDK and demo HTTP entry point run against a deterministic Responses transport.
async function app(t) {
  const calls = [], active = new Map();
  const upstream = await listen(t, createServer(async (req, res) => {
    let raw = ""; for await (const chunk of req) raw += chunk;
    const input = raw ? JSON.parse(raw) : {};
    calls.push({ path: req.url, input, key: req.headers.authorization, idempotency: req.headers["idempotency-key"] });
    if (req.url.endsWith("/cancel")) {
      active.get(req.url.split("/")[3])?.();
      res.writeHead(200, { "Content-Type": "application/json" }); return res.end("{}");
    }
    const id = `resp_${calls.length}`;
    const result = { id, status: "completed", model: "demo-integration", output: [{ type: "message", role: "assistant", content: [{ type: "output_text", text: input.previous_response_id ? "小林每天读10分钟。" : "收到。" }] }] };
    if (!input.stream) { res.writeHead(200, { "Content-Type": "application/json" }); return res.end(JSON.stringify(result)); }
    res.writeHead(200, { "Content-Type": "text/event-stream" });
    const event = value => res.write(`event: ${value.type}\ndata: ${JSON.stringify(value)}\n\n`);
    event({ type: "response.created", response: { ...result, status: "in_progress", output: [] } });
    if (input.input === "wait") {
      active.set(id, () => { event({ type: "response.cancelled", response: { ...result, status: "cancelled" } }); res.end(); active.delete(id); }); return;
    }
    event({ type: "response.output_text.delta", delta: result.output[0].content[0].text });
    if (input.input !== "truncate") event({ type: "response.completed", response: result });
    res.end();
  }));
  const client = new OpenAI({ baseURL: upstream + "/v1", apiKey: "server-secret", maxRetries: 0 });
  const base = await listen(t, createDemoServer({ client, model: "demo-integration", kind: "chat", platformURL: "https://rank.example.test" }));
  const init = await fetch(base + "/api/state");
  const cookie = init.headers.get("set-cookie").split(";")[0];
  const request = (path, body, headers = {}) => fetch(base + path, { method: body === undefined ? "GET" : "POST", headers: { cookie, ...(body === undefined ? {} : { "Content-Type": "application/json" }), ...headers }, body: body === undefined ? undefined : JSON.stringify(body) });
  return { request, base, calls, client };
}

test("chat continues on the server, reloads, isolates visitors and records a keyless transcript", async t => {
  const { request, base, calls } = await app(t);
  const events = async input => (await (await request("/api/chat", { input })).text()).trim().split("\n").map(JSON.parse);
  const first = await events("我叫小林，每天10分钟。");
  const second = await events("我是谁？");
  assert.equal(calls[1].input.previous_response_id, "resp_1");
  assert.ok(calls.every(call => call.key === "Bearer server-secret" && call.idempotency));
  assert.equal((await (await fetch(base + "/api/state")).json()).messages.length, 0);
  const state = await (await request("/api/state")).json();
  assert.equal(state.messages.length, 4);
  assert.ok(!JSON.stringify(state).includes("server-secret"));
  const transcript = { configuration: { model: state.model, kind: state.kind, platformURL: state.platformURL }, turns: [first, second].map(events => events.map(event => event.type === "completed" ? { type: event.type, id: event.result.id, text: event.result.text, status: event.result.status } : event)) };
  const path = new URL("snapshots/demo-chat.json", import.meta.url);
  if (process.env.DEMO_RECORD === "1") await writeFile(path, JSON.stringify(transcript, null, 2) + "\n");
  assert.deepEqual(transcript, JSON.parse(await readFile(path, "utf8")));
  assert.equal((await request("/api/reset", {}, { origin: "http://foreign.test" })).status, 403);
  assert.equal((await request("/api/chat", { input: "" })).status, 400);
  await request("/api/reset", {});
  assert.equal((await (await request("/api/state")).json()).messages.length, 0);
});

test("cancellation reaches Rank and incomplete streams require a new conversation", async t => {
  const { request, calls } = await app(t);
  const stream = await request("/api/chat", { input: "wait" });
  assert.equal((await request("/api/chat", { input: "overlap" })).status, 409);
  await request("/api/cancel", {});
  assert.match(await stream.text(), /response.cancelled/);
  assert.ok(calls.some(call => call.path.endsWith("/cancel")));
  assert.equal((await request("/api/chat", { input: "continue" })).status, 409);
  await request("/api/reset", {});
  assert.match(await (await request("/api/chat", { input: "truncate" })).text(), /Stream disconnected/);
  assert.equal((await (await request("/api/state")).json()).broken, true);
});

test("daily headlines make one model request and stop when the source fails", async t => {
  const { client, calls } = await app(t);
  const now = Date.now();
  const hits = [2, 1].map(n => ({ objectID: String(n), title: `Title ${n}`, points: n * 10, num_comments: n, created_at: new Date(now - 1000).toISOString(), created_at_i: Math.floor(now / 1000) - 1, url: "https://example.org/" }));
  hits.push({ ...hits[0], created_at_i: 1, points: 9999 });
  const result = await dailyNews(client, "demo-integration", { now, request: async () => new Response(JSON.stringify({ hits })) });
  assert.equal(calls.length, 1);
  assert.deepEqual(result.sources.map(s => s.points), [20, 10]);
  assert.ok(!calls[0].input.previous_response_id && !calls[0].input.stream);
  await assert.rejects(dailyNews(client, "demo-integration", { request: async () => new Response("", { status: 503 }) }), /no model request/);
  await assert.rejects(dailyNews(client, "demo-integration", { request: async () => new Response('{"hits":[]}') }), /No headlines/);
  assert.equal(calls.length, 1);
});
