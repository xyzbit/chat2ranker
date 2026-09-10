import assert from "node:assert/strict";
import { createServer } from "node:http";
import test from "node:test";
import { runExample } from "./client.mjs";

// This fixture checks SDK transport and client assertions, not Rank execution.
async function fixture(t, { truncated = false, unauthorized = false } = {}) {
  const calls = [];
  const response = id => ({
    id, object: "response", status: "completed", model: "orders@v2",
    output: [{ id: `msg_${id}`, type: "message", role: "assistant", status: "completed",
      content: [{ type: "output_text", text: "RANK-2048", annotations: [] }] }],
  });
  const server = createServer(async (req, res) => {
    let body = "";
    for await (const chunk of req) body += chunk;
    const input = body ? JSON.parse(body) : null;
    calls.push({ method: req.method, url: req.url, authorization: req.headers.authorization, input });
    if (unauthorized) {
      res.writeHead(401, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: { message: "Invalid serving key", type: "authentication_error", code: "invalid_api_key" } }));
    } else if (input?.stream) {
      res.writeHead(200, { "Content-Type": "text/event-stream" });
      const delta = `event: response.output_text.delta\ndata: ${JSON.stringify({ type: "response.output_text.delta", delta: "RANK-2048" })}\n\n`;
      res.write(delta.slice(0, 40));
      res.write(delta.slice(40));
      if (!truncated) res.write(`event: response.completed\ndata: ${JSON.stringify({ type: "response.completed", response: response("resp_second") })}\n\n`);
      res.end();
    } else {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify(response(req.method === "GET" ? "resp_second" : "resp_first")));
    }
  });
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  t.after(() => new Promise(resolve => { server.close(resolve); server.closeAllConnections(); }));
  return { calls, options: { baseURL: `http://127.0.0.1:${server.address().port}/v1`, apiKey: "fixture-key", model: "orders@production" } };
}

test("SDK sends scoped authentication and continues before retrieving the streamed response", async t => {
  const { calls, options } = await fixture(t);
  const result = await runExample(options);
  assert.equal(result.model, "orders@v2");
  assert.deepEqual(calls.map(c => [c.method, c.url]), [["POST", "/v1/responses"], ["POST", "/v1/responses"], ["GET", "/v1/responses/resp_second"]]);
  assert.ok(calls.every(c => c.authorization === "Bearer fixture-key"));
  assert.equal(calls[1].input.previous_response_id, "resp_first");
});

test("A disconnected stream without completion fails verification", async t => {
  const { options } = await fixture(t, { truncated: true });
  await assert.rejects(runExample({ ...options, prompt: "test" }), /without response.completed/);
});

test("Invalid credentials fail without retrying", async t => {
  const { calls, options } = await fixture(t, { unauthorized: true });
  await assert.rejects(runExample(options), /401/);
  assert.equal(calls.length, 1);
});
