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

test("deployment API encodes selectors and sends release evidence", async (t) => {
  const originalFetch = globalThis.fetch;
  const requests = [];
  globalThis.fetch = async (path, options = {}) => {
    requests.push({ path, options });
    return new Response(JSON.stringify({ id: "release-1" }), {
      status: 201,
      headers: { "content-type": "application/json" },
    });
  };
  t.after(() => { globalThis.fetch = originalFetch; });

  await rankApi.listDeployments("tenant/local");
  await rankApi.promoteDeployment("deployment one", "staging", {
    agentVersionId: "agent-v2",
    expectedReleaseRevisionId: "release-0",
    evidenceRunId: "run-1",
    policyResult: "passed",
    actor: "operator",
  });

  assert.equal(requests[0].path, "/api/deployments?tenantId=tenant%2Flocal");
  assert.equal(requests[1].path, "/api/deployments/deployment%20one/aliases/staging/releases");
  assert.deepEqual(JSON.parse(requests[1].options.body), {
    agentVersionId: "agent-v2",
    expectedReleaseRevisionId: "release-0",
    evidenceRunId: "run-1",
    policyResult: "passed",
    actor: "operator",
  });
});

test("Webhook API scopes endpoint creation to Deployments", async (t) => {
  const originalFetch = globalThis.fetch;
  const requests = [];
  globalThis.fetch = async (path, options = {}) => {
    requests.push({ path, options });
    return new Response(JSON.stringify({ id: "wh-1", enabled: true }), {
      status: 201,
      headers: { "content-type": "application/json" },
    });
  };
  t.after(() => { globalThis.fetch = originalFetch; });

  await rankApi.listWebhookEndpoints("tenant/local");
  await rankApi.createWebhookEndpoint({
    tenantId: "tenant/local",
    name: "CRM",
    url: "https://example.com/rank",
    events: ["response.completed"],
    deploymentIds: ["deploy-1"],
  });
  await rankApi.setWebhookEndpointEnabled("wh/1", false);

  assert.equal(requests[0].path, "/api/webhook-endpoints?tenantId=tenant%2Flocal");
  assert.deepEqual(JSON.parse(requests[1].options.body).deploymentIds, ["deploy-1"]);
  assert.equal(requests[2].path, "/api/webhook-endpoints/wh%2F1/disable");
});

test("only management authorization failures return the workspace to login", async (t) => {
  const previousFetch = globalThis.fetch;
  const previousDispatch = globalThis.dispatchEvent;
  const events = [];
  globalThis.dispatchEvent = event => { events.push(event.type); return true; };
  t.after(() => { globalThis.fetch = previousFetch; globalThis.dispatchEvent = previousDispatch; });
  for (const code of ["management_unauthorized", "invalid_api_key", "management_auth_unavailable"]) {
    globalThis.fetch = async () => new Response(JSON.stringify({ error: { code } }), { status: code === "management_auth_unavailable" ? 503 : 401 });
    await assert.rejects(rankApi.bootstrap(), error => error.code === code);
  }
  assert.deepEqual(events, ["rank:unauthorized"]);
});
