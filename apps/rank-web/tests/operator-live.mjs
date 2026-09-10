/** Explicit, paid acceptance against a running local stack and a verified stored connection. */
import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";

const base = process.env.RANK_ACCEPTANCE_URL || "http://127.0.0.1:8787";
const stamp = Date.now().toString(36);
const report = { startedAt: new Date().toISOString(), checks: [] };
const output = resolve(import.meta.dirname, "../../../output/playwright", `operator-live-${stamp}.json`);
const pause = (ms) => new Promise((done) => setTimeout(done, ms));
async function request(path, body, headers = {}, expected = 200) {
  const response = await fetch(base + path, { method: body === undefined ? "GET" : "POST", headers: { "content-type": "application/json", ...headers }, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(180000) });
  const value = await response.json();
  assert.equal(response.status, expected, `${path}: ${JSON.stringify(value.error || {})}`);
  return value;
}
function passed(name) { report.checks.push(name); console.log(`PASS ${name}`); }
async function command(experiment, type, payload) {
  const action = experiment.a2ui.actions[type];
  const value = await request(`/api/experiments/${experiment.id}/commands`, { type: action.command, actionToken: action.token, payload }, { "idempotency-key": crypto.randomUUID(), "x-rank-action-token": action.token });
  return value.experiment;
}
let credential;
let tenant;
try {
  const bootstrap = await request("/api/bootstrap");
  tenant = bootstrap.tenantId;
  const connection = bootstrap.modelConnections.find((v) => v.provider === "deepseek" && v.status === "verified");
  assert.ok(connection, "configure a verified DeepSeek connection in Settings first");
  assert.ok(bootstrap.systemModels.some((v) => v.role === "judge" && v.connectionId === connection.id));
  passed("persisted model connection and judge binding");
  const agentInput = { name: `Acceptance ${stamp}`, handle: `@acceptance/${stamp}`, runnerType: "dsh", model: "deepseek-chat", modelConnectionId: connection.id, systemPrompt: "Answer the user's request concisely. For arithmetic return only the result. Do not use tools unless the user explicitly asks for a supplied function.", tools: [], skills: [] };
  const agent = await request("/api/agents", agentInput, {}, 201);
  report.agentId = agent.id;
  const dataset = await request("/api/datasets", { name: `Arithmetic ${stamp}`, cases: [{ id: "sum", input: "What is 2 + 2? Return only the number.", expected: { answer: "4" } }], rubric: { description: "Pass only if the answer is exactly 4." } }, {}, 201);
  let experiment = await request("/api/experiments", { title: `Acceptance ${stamp}` }, {}, 201);
  experiment = await command(experiment, "select_dataset", { datasetVersionId: dataset.id });
  experiment = await command(experiment, "select_agent", { agentVersionId: agent.id });
  report.experimentId = experiment.id;
  const started = await request(`/api/experiments/${experiment.id}/runs`, { trialCount: 1, agentVersionIds: [agent.id] }, { "idempotency-key": crypto.randomUUID() }, 202);
  const runId = started.run?.id || started.id;
  assert.ok(runId); report.runId = runId;
  let run;
  for (let i = 0; i < 150; i++) {
    run = await request(`/api/runs/${runId}`);
    if (["complete", "failed", "cancelled"].includes(run.status)) break;
    await pause(2000);
  }
  assert.equal(run.status, "complete", run.error);
  assert.equal(run.total, 1); assert.equal(run.passed, 1);
  const evaluations = await request(`/api/agent-versions/${agent.id}/evaluations`);
  assert.ok(evaluations.some((v) => v.id === runId && v.datasetVersion === dataset.version));
  report.evaluation = { passed: run.passed, total: run.total, cost: run.cost, costKnown: run.costKnown };
  passed("real Agent execution, judge evaluation and version-scoped evidence");
  const deployment = await request("/api/deployments", { tenantId: tenant, agentFamilyId: agent.familyId, name: `acceptance-${stamp}`, policy: { requestsPerMinute: 60, maxConcurrentResponses: 4, executionTimeoutMs: 120000, allowInstructions: false, allowCallerTools: true } }, {}, 201);
  report.deploymentId = deployment.id; report.deploymentName = deployment.name;
  const release = await request(`/api/deployments/${deployment.id}/aliases/production/releases`, { agentVersionId: agent.id, policyResult: "passed", evidenceRunId: runId, actor: "live-acceptance", reason: "real evaluation verified" }, {}, 201);
  credential = await request("/api/serving-credentials", { tenantId: tenant, name: `Acceptance ${stamp}`, deploymentIds: [deployment.id], scopes: ["responses:create", "responses:read", "responses:cancel"] }, {}, 201);
  const headers = { authorization: `Bearer ${credential.apiKey}` };
  const firstInput = { model: `${deployment.name}@production`, input: "Remember the codeword ORCHID. Reply only ORCHID." };
  const idempotency = crypto.randomUUID();
  const first = await request("/v1/responses", firstInput, { ...headers, "idempotency-key": idempotency });
  assert.equal(first.status, "completed");
  const repeated = await request("/v1/responses", firstInput, { ...headers, "idempotency-key": idempotency });
  assert.equal(repeated.id, first.id);
  const detail = await request(`/api/responses/${first.id}?tenantId=${encodeURIComponent(tenant)}`);
  assert.equal(detail.response.agentVersionId, agent.id); assert.equal(detail.response.releaseRevisionId, release.id);
  assert.ok(detail.response.outputText.includes("ORCHID")); assert.ok(detail.events.length > 1);
  assert.ok(detail.response.usage.totalTokens > 0);
  report.responseId = first.id;
  passed("real production invocation, idempotency and trace details");
  const version2 = await request(`/api/agent-families/${agent.familyId}/versions`, { ...agentInput, description: "second immutable version" }, {}, 201);
  await request(`/api/deployments/${deployment.id}/aliases/production/releases`, { agentVersionId: version2.id, expectedReleaseRevisionId: release.id, policyResult: "manual_override", actor: "live-acceptance", reason: "verify conversation pinning across release" }, {}, 201);
  const continued = await request("/v1/responses", { model: `${deployment.name}@production`, previous_response_id: first.id, input: "What was the codeword? Reply only that word." }, headers);
  const continuedDetail = await request(`/api/responses/${continued.id}?tenantId=${encodeURIComponent(tenant)}`);
  assert.equal(continuedDetail.response.agentVersionId, agent.id);
  assert.ok(continuedDetail.response.outputText.includes("ORCHID"));
  const exact = await request("/v1/responses", { model: `${deployment.name}@v2`, input: "Return only the number 7." }, headers);
  const exactDetail = await request(`/api/responses/${exact.id}?tenantId=${encodeURIComponent(tenant)}`);
  assert.equal(exactDetail.response.agentVersionId, version2.id);
  assert.ok(!exactDetail.response.releaseRevisionId);
  const history = await request(`/api/deployments/${deployment.id}/aliases/production/releases`);
  assert.equal(history.items.length, 2); assert.equal(history.items[0].previousAgentVersionId, agent.id);
  passed("release history, exact versions and continuation pinned across alias movement");
  const structured = await request("/v1/responses", { model: `${deployment.name}@latest`, input: "Return a JSON object with answer equal to 4.", text: { format: { type: "json_schema", name: "arithmetic", strict: true, schema: { type: "object", properties: { answer: { type: "integer" } }, required: ["answer"], additionalProperties: false } } } }, headers);
  const structuredDetail = await request(`/api/responses/${structured.id}?tenantId=${encodeURIComponent(tenant)}`);
  assert.deepEqual(JSON.parse(structuredDetail.response.outputText), { answer: 4 });
  passed("real structured output through latest alias");
  const list = await request(`/api/responses?tenantId=${encodeURIComponent(tenant)}&deploymentId=${deployment.id}&limit=2`);
  assert.equal(list.items.length, 2); assert.ok(list.nextCursor);
  assert.ok(!JSON.stringify(list).includes("ORCHID"));
  const older = await request(`/api/responses?tenantId=${encodeURIComponent(tenant)}&deploymentId=${deployment.id}&limit=2&before=${list.nextCursor}`);
  assert.ok(older.items.every((v) => !list.items.some((item) => item.id === v.id)));
  const keys = await request(`/api/serving-credentials?tenantId=${encodeURIComponent(tenant)}`);
  assert.ok(!JSON.stringify(keys).includes(credential.apiKey));
  await request(`/api/serving-credentials/${credential.id}/revoke?tenantId=${encodeURIComponent(tenant)}`, {});
  await request("/v1/responses", firstInput, headers, 401);
  passed("bounded history, redacted credentials and revocation denies new calls");
  report.status = "passed";
} catch (error) {
  report.status = "failed"; report.error = error.message; console.error(error.message); process.exitCode = 1;
} finally {
  if (credential?.id) await request(`/api/serving-credentials/${credential.id}/revoke?tenantId=${encodeURIComponent(tenant)}`, {}).catch(() => { console.error("Acceptance key could not be revoked; inspect credential management."); });
  await mkdir(resolve(output, ".."), { recursive: true });
  await writeFile(output, JSON.stringify(report, null, 2) + "\n");
  console.log(`Report: ${output}`);
}
