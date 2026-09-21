import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { endpointExamples } from "../src/endpoint-guide.js";

/** Exercises release gates through real Rank, Execution Service and worker processes. */
export async function verifyReleaseLifecycle({ baseURL, request, command, completeRun, controlToken }) {
  const post = (url, body) => request(baseURL, url, { method: "POST", body: JSON.stringify(body) });
  async function rejected(url, body, status) {
    const response = await fetch(baseURL + url, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
    assert.equal(response.status, status, await response.clone().text());
    return (await response.json()).error.code;
  }
  const dataset = await post("/api/datasets", { name: "Release smoke", cases: [
    { id: "one", input: "release smoke", expected: { outputContains: "release smoke" } },
    { id: "two", input: "release smoke", expected: { outputContains: "release smoke" } },
    { id: "three", input: "release smoke", expected: { outputContains: "absent marker" } },
  ] });
  const deployment = await post("/api/deployments", { tenantId: "local", name: "release-lifecycle", agentFamilyId: "agent-research-demo", policy: { requestsPerMinute: 120, maxConcurrentResponses: 4, executionTimeoutMs: 30000 } });
  const endpoint = `/api/deployments/${deployment.id}`;
  const alias = `${endpoint}/aliases/production`;
  let revision = 0;
  const save = async (minPassRate) => {
    const policy = await request(baseURL, `${endpoint}/release-policy`, { method: "PUT", body: JSON.stringify({ expectedRevision: revision, datasetVersionId: dataset.id, minPassRate, minValidTrialRate: 100, maxEvidenceAgeHours: 24 }) });
    revision = policy.revision;
    return policy;
  };
  async function evaluate(agentVersionId) {
    let experiment = await post("/api/experiments", { title: "Release lifecycle" });
    experiment = await command(baseURL, experiment, "select_dataset", { datasetVersionId: dataset.id });
    experiment = await command(baseURL, experiment, "select_agent", { agentVersionId });
    const started = await request(baseURL, `/api/experiments/${experiment.id}/runs`, { method: "POST", headers: { "idempotency-key": crypto.randomUUID() }, body: JSON.stringify({ trialCount: 1, agentVersionIds: [agentVersionId] }) });
    const run = await completeRun(baseURL, started.run?.id || started.id);
    assert.equal(run.status, "complete");
    assert.equal(run.validTrials, 3);
    assert.equal(run.passed, 2);
    return run;
  }
  const preflight = (version, evidence) => post(`${alias}/preflight`, { agentVersionId: version, evidenceRunId: evidence });
  const input = (review, extra = {}) => ({ agentVersionId: review.agentVersionId, evidenceRunId: review.evaluation.evidenceRunId, expectedReleaseRevisionId: review.expectedReleaseRevisionId, expectedPolicyRevision: review.evaluation.policy.revision, policyResult: "passed", actor: "release-e2e", ...extra });
  const empty = await preflight("agent-research-demo-v1");
  assert.equal(empty.evaluation.passed, false);
  const unconfigured = await rejected(`${alias}/releases`, input(empty), 422);
  await save(67);
  const run1 = await evaluate("agent-research-demo-v1");
  const below = await preflight("agent-research-demo-v1", run1.id);
  assert.equal(below.evaluation.passed, false);
  assert.ok(below.evaluation.checks.find(v => v.code === "pass_rate").actual < 67);
  const thresholdRejection = await rejected(`${alias}/releases`, input(below), 422);
  await save(66);
  const stale = await preflight("agent-research-demo-v1", run1.id);
  assert.equal(stale.evaluation.passed, true);
  await save(67);
  const policyConflict = await rejected(`${alias}/releases`, input(stale), 409);
  await save(66);
  const reviewed1 = await preflight("agent-research-demo-v1", run1.id);
  const release1 = await post(`${alias}/releases`, input(reviewed1));
  const credential = await post("/api/serving-credentials", { tenantId: "local", name: "release lifecycle", deploymentIds: [deployment.id], scopes: ["responses:create", "responses:read"] });
  try {
    const invoke = (previous) => request(baseURL, "/v1/responses", { method: "POST", headers: { authorization: `Bearer ${credential.apiKey}` }, body: JSON.stringify({ model: "release-lifecycle", input: "release smoke", ...(previous ? { previous_response_id: previous.id } : {}) }) });
    const first = await invoke();
    assert.equal(first.status, "completed");
    assert.equal(first.deployment.release_revision_id, release1.id);
    const v2 = await post("/api/agent-families/agent-research-demo/versions", { runnerType: "mock", model: "deterministic-demo", description: "release lifecycle v2" });
    const wrongEvidence = await preflight(v2.id, run1.id);
    assert.equal(wrongEvidence.evaluation.checks.find(v => v.code === "agent_version").passed, false);
    const run2 = await evaluate(v2.id);
    const reviewed2 = await preflight(v2.id, run2.id);
    assert.equal(reviewed2.currentAgentVersionId, "agent-research-demo-v1");
    const release2 = await post(`${alias}/releases`, input(reviewed2));
    await rejected(`${alias}/releases`, input(reviewed2), 409);
    const second = await invoke();
    assert.equal(second.deployment.agent_version_id, v2.id);
    assert.equal(second.deployment.release_revision_id, release2.id);
    const oldContinued = await invoke(first);
    const rollback = await post(`${alias}/releases`, input(await preflight("agent-research-demo-v1", run1.id)));
    const afterRollback = await invoke();
    const secondContinued = await invoke(second);
    assert.equal(afterRollback.deployment.agent_version_id, first.deployment.agent_version_id);
    assert.equal(afterRollback.deployment.release_revision_id, rollback.id);
    assert.equal(oldContinued.deployment.release_revision_id, release1.id);
    assert.equal(secondContinued.deployment.release_revision_id, release2.id);
    assert.equal(secondContinued.deployment.agent_version_id, v2.id);
    const manual = input(await preflight(v2.id), { policyResult: "manual_override" });
    const reasonRequired = await rejected(`${alias}/releases`, manual, 400);
    await post(`${alias}/releases`, { ...manual, reason: "emergency recovery without fresh evidence" });
    const decisions = await request(baseURL, `${alias}/decisions`);
    const history = await request(baseURL, `${alias}/releases`);
    const snapshot = {
      unconfigured, thresholdRejection, policyConflict, reasonRequired,
      validTrials: run1.validTrials, passedTrials: run1.passed,
      checks: below.evaluation.checks.map(({ code, passed }) => ({ code, passed })),
      releaseRevisions: history.items.map(v => v.revision),
      decisions: decisions.items.map(v => ({ result: v.result, policyRevision: v.evaluation.policy.revision, minPassRate: v.evaluation.policy.minPassRate, passed: v.evaluation.passed, reason: v.reason, published: Boolean(v.releaseRevisionId) })),
      continuation: { beforeRollbackPinned: oldContinued.deployment.agent_version_id === first.deployment.agent_version_id, afterRollbackPinned: secondContinued.deployment.agent_version_id === second.deployment.agent_version_id, sameConversation: secondContinued.conversation.id === second.conversation.id },
    };
    assert.deepEqual(snapshot, JSON.parse(await readFile(new URL("snapshots/release-lifecycle.json", import.meta.url), "utf8")));
    if (process.env.RANK_RELEASE_SDK === "1") {
      const { runExample } = await import("../../../rank/examples/responses-client/client.mjs");
      const result = await runExample({ baseURL: `${baseURL}/v1`, apiKey: credential.apiKey, model: "release-lifecycle" });
      assert.equal(result.status, "completed");
      const examples = endpointExamples("release-lifecycle", baseURL);
      const curlExample = endpointExamples("release-lifecycle", baseURL, credential.apiKey);
      const invoked = await promisify(execFile)("sh", ["-c", curlExample.curlCommand], { timeout: 20000 });
      assert.equal(JSON.parse(invoked.stdout).status, "completed");
      for (const source of [examples.javascript, examples.instructions, examples.tools, examples.stream]) {
        const { stdout } = await promisify(execFile)(process.execPath, ["--input-type=module", "-e", source], {
          cwd: new URL("../../../rank/examples/responses-client/", import.meta.url),
          env: { ...process.env, RANK_API_KEY: credential.apiKey }, timeout: 20000,
        });
        assert.ok(stdout.trim(), "Documentation example produced no output");
      }
    }
    const headers = { "x-rank-control-token": controlToken || process.env.RANK_CONTROL_TOKEN || "rank-local-control-token" };
    const conversation = await request(baseURL, `/api/experiments/${run2.experimentId}`, { method: "PATCH", body: JSON.stringify({ title: "Release conversation" }) });
    const binding = { experimentId: run2.experimentId, controlSessionId: conversation.controlSessionId };
    await request(baseURL, "/api/internal/control/commands", { method: "POST", headers, body: JSON.stringify({ ...binding, idempotencyKey: "prepare-release-e2e", type: "prepare_release", payload: { deploymentId: deployment.id, alias: "production", agentVersionId: v2.id } }) });
  } finally {
    await post(`/api/serving-credentials/${credential.id}/revoke?tenantId=local`, {});
  }
}
