import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

/** Runs the assembled HTTP, configured Rank storage, Execution Service and DSH CLI trial flow. */
export async function verifyTrials({ baseURL, request, restart, controlToken = process.env.RANK_CONTROL_TOKEN || "rank-local-control-token" }) {
  const post = (url, body, key = crypto.randomUUID()) => request(baseURL, url, { method: "POST", headers: { "Idempotency-Key": key }, body: JSON.stringify(body) });
  const get = url => request(baseURL, url);
  const reject = async (body, status, key) => {
    const response = await fetch(baseURL + "/api/trial-responses", { method: "POST", headers: { "content-type": "application/json", "Idempotency-Key": key || crypto.randomUUID() }, body: JSON.stringify(body) });
    assert.equal(response.status, status, await response.text());
  };
  const settled = async response => {
    const deadline = Date.now() + 20000;
    while (Date.now() < deadline) {
      const detail = await get(`/api/trials/${response.conversationId}`);
      const turn = detail.messages.find(item => item.id === response.id);
      if (!["queued", "in_progress"].includes(turn.status)) return turn;
      await new Promise(resolve => setTimeout(resolve, 40));
    }
    throw new Error("Trial did not complete");
  };
  const before = await get("/api/bootstrap");
  const agent = await post("/api/agents", { name: "Trial Agent", handle: "@test/trial", runnerType: "dsh", model: "fixture-chat", systemPrompt: "Trial system prompt", tools: [], skills: [] });
  const deploymentsBefore = await get("/api/deployments");
  const firstInput = { agentVersionId: agent.id, input: "Remember Ada" };
  const first = await post("/api/trial-responses", firstInput, "trial-first");
  const duplicate = await post("/api/trial-responses", firstInput, "trial-first");
  assert.equal(duplicate.id, first.id);
  await reject({ ...firstInput, input: "different" }, 409, "trial-first");
  await reject({ agentVersionId: "missing", input: "hello" }, 404);
  await reject({ agentVersionId: agent.id, input: "" }, 400);
  const output = await settled(first);
  assert.equal(output.outputText, "Reply: Remember Ada");
  const next = input => ({ conversationId: first.conversationId, previousResponseId: input.id, input: "Who?" });
  const followup = await post("/api/trial-responses", next(first));
  assert.equal((await settled(followup)).outputText, "Reply: Who? | First: Remember Ada");
  await reject(next(first), 409);
  const v2 = await post(`/api/agent-families/${agent.familyId}/versions`, { runnerType: "dsh", model: "fixture-chat-v2", systemPrompt: "New version" });
  await reject({ ...next(followup), agentVersionId: v2.id }, 400);
  const pinned = await post("/api/trial-responses", next(followup));
  assert.equal((await settled(pinned)).agentVersionId, agent.id);
  const failure = await post("/api/trial-responses", { ...next(pinned), input: "fail" });
  assert.equal((await settled(failure)).status, "failed");
  const retry = await post("/api/trial-responses", next(failure));
  assert.equal((await settled(retry)).outputText, "Reply: Who? | First: Remember Ada");
  const slow = await post("/api/trial-responses", { ...next(retry), input: "slow" });
  const stream = await fetch(baseURL + `/api/trial-responses/${slow.id}/events`);
  const reader = stream.body.getReader();
  const decoder = new TextDecoder();
  let live = "";
  while (!live.includes('"text":"Reply: "')) {
    const { value, done } = await reader.read();
    assert.equal(done, false, "Expected stdout before completion");
    live += decoder.decode(value, { stream: true });
  }
  assert.ok(!live.includes('"type":"response.completed"'));
  await reader.cancel();
  const concurrent = await fetch(baseURL + "/api/trial-responses", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(next(slow)) });
  assert.equal(concurrent.status, 409);
  await post(`/api/trial-responses/${slow.id}/cancel`, {});
  assert.equal((await settled(slow)).status, "cancelled");
  const afterStop = await post("/api/trial-responses", next(slow));
  assert.equal((await settled(afterStop)).outputText, "Reply: Who? | First: Remember Ada");
  const recover = await post("/api/trial-responses", { agentVersionId: v2.id, input: "slow" });
  await new Promise(resolve => setTimeout(resolve, 450));
  await restart();
  assert.equal((await settled(recover)).outputText, "Reply: slow");
  const recovered = await get(`/api/trials/${first.conversationId}`);
  assert.equal(recovered.agent.id, agent.id);
  const listing = await get("/api/trials");
  assert.equal(listing.find(item => item.id === first.conversationId).title, "Remember Ada");
  const after = await get("/api/bootstrap");
  assert.equal(after.experiments.length, before.experiments.length);
  assert.equal(after.datasets.length, before.datasets.length);
  assert.equal((await get("/api/deployments")).length, deploymentsBefore.length);
  const native = await post("/api/agents", { name: "Native Trial", handle: "@test/native-trial", runnerType: "codex", model: "fixture-native" });
  const nativeReply = await post("/api/trial-responses", { agentVersionId: native.id, input: "hello" });
  const nativeEvents = await fetch(baseURL + `/api/trial-responses/${nativeReply.id}/events`).then(response => response.text());
  assert.ok(!nativeEvents.includes("raw-protocol-marker"), "Native protocol must not render as assistant text");
  assert.equal((await settled(nativeReply)).outputText, "Native answer");
  const nativeBefore = await get(`/api/trials/${nativeReply.conversationId}`);
  await reject({ conversationId: nativeReply.conversationId, previousResponseId: nativeReply.id, input: "continue" }, 409);
  const nativeAfter = await get(`/api/trials/${nativeReply.conversationId}`);
  assert.deepEqual(nativeAfter.messages.map(item => item.id), nativeBefore.messages.map(item => item.id), "Unsupported history must reject before admission");
  const deployment = await post("/api/deployments", { tenantId: "local", name: "trial-published", agentFamilyId: agent.familyId, policy: { requestsPerMinute: 60, maxConcurrentResponses: 4, executionTimeoutMs: 20000 } });
  assert.equal(deployment.aliases.length, 1);
  const unavailable = await fetch(baseURL + "/api/trial-responses", { method: "POST", headers: { "content-type": "application/json", "Idempotency-Key": crypto.randomUUID() }, body: JSON.stringify({ deploymentReference: deployment.name, input: "unpublished" }) });
  assert.equal(unavailable.status, 409);
  await post(`/api/deployments/${deployment.id}/aliases/production/releases`, { agentVersionId: v2.id, expectedPolicyRevision: 0, policyResult: "manual_override", actor: "e2e", reason: "Reviewed fixture publication" });
  const webhook = await post("/api/webhook-endpoints", { tenantId: "local", name: "Trial isolation", url: "https://example.com/events", deploymentIds: [deployment.id], events: ["response.completed"] });
  const deployed = await post("/api/trial-responses", { deploymentReference: "trial-published", input: "release" });
  await settled(deployed);
  assert.equal(deployed.agentVersionId, v2.id);
  assert.ok(deployed.releaseRevisionId);
  const publishedTargets = await get(`/api/deployments?agentVersionId=${v2.id}`);
  assert.deepEqual(publishedTargets.map(item => item.id), [deployment.id]);
  assert.deepEqual(await get(`/api/deployments?agentVersionId=${agent.id}`), []);
  assert.deepEqual(await get(`/api/webhook-endpoints/${webhook.id}/deliveries`), []);
  const familyHistory = await get(`/api/trials?agentFamilyId=${agent.familyId}`);
  assert.equal(familyHistory.length, 3);
  assert.ok(familyHistory.every(item => [agent.id, v2.id].includes(item.agentVersionId)));
  assert.deepEqual(await get("/api/trials?agentFamilyId=missing"), []);
  const versionHistory = await get(`/api/trials?agentVersionId=${v2.id}&directOnly=true`);
  assert.deepEqual(versionHistory.map(item => item.id), [recover.conversationId]);
  const olderHistory = await get(`/api/trials?agentFamilyId=${agent.familyId}&before=${deployed.conversationId}`);
  assert.deepEqual(olderHistory.map(item => item.id), [recover.conversationId, first.conversationId]);
  const key = await post("/api/serving-credentials", { tenantId: "local", name: "Trial separation", deploymentIds: [deployment.id], scopes: ["responses:create", "responses:read"] });
  const external = await fetch(baseURL + `/v1/responses/${deployed.id}`, { headers: { authorization: `Bearer ${key.apiKey}` } });
  assert.equal(external.status, 404);
  const publicReply = await request(baseURL, "/v1/responses", { method: "POST", headers: { authorization: `Bearer ${key.apiKey}` }, body: JSON.stringify({ model: "trial-published", input: "external" }) });
  const privateRead = await fetch(baseURL + `/api/trials/${publicReply.conversation.id}`);
  assert.equal(privateRead.status, 404);
  const chat = await post("/api/experiments", { title: "Control handoff" });
  const handoff = await request(baseURL, "/api/internal/control/commands", { method: "POST", headers: { "x-rank-control-token": controlToken }, body: JSON.stringify({ experimentId: chat.id, controlSessionId: chat.controlSessionId, idempotencyKey: "handoff", type: "open_trial", payload: { agentVersionId: agent.id, draft: "Remember Ada" } }) });
  const handed = await get(`/api/experiments/${chat.id}`);
  const snapshot = {
    transcript: recovered.messages.map(item => ({ input: item.input, status: item.status, output: item.status === "completed" ? item.outputText : "" })),
    duplicateSuppressed: first.id === duplicate.id,
    pinnedVersion: recovered.agent.version,
    noEvaluationCreated: after.experiments.length === before.experiments.length,
    resumed: (await settled(recover)).outputText,
    scopedHistory: { familyVersions: familyHistory.map(item => item.agentVersionId === v2.id ? 2 : 1), directVersionCount: versionHistory.length, olderCount: olderHistory.length },
    publishingEntry: { version: 2, targets: publishedTargets.map(item => item.name), unpublishedVersion: 1, unpublishedStatus: unavailable.status, reference: deployed.requestedReference },
    handoff: handed.controlEvents.map(event => ({ type: event.type, label: event.payload.label, draft: event.payload.draft })),
  };
  assert.ok(handoff);
  assert.deepEqual(snapshot, JSON.parse(await readFile(new URL("snapshots/trials.json", import.meta.url), "utf8")));
}
