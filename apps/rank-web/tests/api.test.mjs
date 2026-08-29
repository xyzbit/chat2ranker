import assert from "node:assert/strict";
import test from "node:test";

import { rankApi } from "../src/api.js";

test("executeCommand generates an idempotency key without an optional method call", async (t) => {
  const originalFetch = globalThis.fetch;
  let request;
  globalThis.fetch = async (path, options) => {
    request = { path, options };
    return new Response(JSON.stringify({ experiment: { id: "exp-1" } }), {
      status: 202,
      headers: { "content-type": "application/json" },
    });
  };
  t.after(() => { globalThis.fetch = originalFetch; });

  await rankApi.executeCommand("exp-1", { command: "start_run", token: "action-token" }, { trialCount: 1 });

  const body = JSON.parse(request.options.body);
  assert.equal(request.path, "/api/experiments/exp-1/commands");
  assert.equal(typeof body.idempotencyKey, "string");
  assert.ok(body.idempotencyKey);
  assert.equal(request.options.headers["Idempotency-Key"], body.idempotencyKey);
});
