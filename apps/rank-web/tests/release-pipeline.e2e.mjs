import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

/** Uses real persisted release tasks and isolated Execution workers, including serving after promotion. */
export async function verifyReleasePipeline({ baseURL, request, completeRun, controlToken }) {
  const post = (path, body, headers = {}) => request(baseURL, path, { method: "POST", headers, body: JSON.stringify(body) });
  const put = (path, body) => request(baseURL, path, { method: "PUT", body: JSON.stringify(body) });
  const dataset = await post("/api/datasets", { name: "Pipeline pass", cases: [{ id: "one", title: "Greeting", input: "pipeline", expected: { outputContains: "pipeline" } }, { id: "two", title: "Reply", input: "pipeline", expected: { outputContains: "pipeline" } }] });
  const failedDataset = await post("/api/datasets", { name: "Pipeline fail", cases: [{ id: "fail", input: "pipeline", expected: { outputContains: "impossible marker" } }] });
  let experiment = await post("/api/experiments", { title: "Pipeline conversation" });
  experiment = await request(baseURL, `/api/experiments/${experiment.id}`, { method: "PATCH", body: JSON.stringify({ agentVersionId: "agent-research-demo-v1" }) });
  const control = (type, payload, key = crypto.randomUUID()) => post("/api/internal/control/commands", { experimentId: experiment.id, controlSessionId: experiment.controlSessionId, idempotencyKey: key, type, payload }, { "x-rank-control-token": controlToken || process.env.RANK_CONTROL_TOKEN || "rank-local-control-token" });
  const created = await control("create_deployment", { name: "pipeline-e2e" }, "create-pipeline");
  const deployment = created.command.result.deployment;
  const replayCreate = await control("create_deployment", { name: "pipeline-e2e" }, "create-pipeline");
  assert.equal(replayCreate.command.id, created.command.id);
  await control("set_evaluation_defaults", { developmentDatasetId: dataset.familyId, regressionDatasetId: dataset.familyId });
  const defaults = await request(baseURL, "/api/agent-families/agent-research-demo/evaluation-defaults");
  assert.equal(defaults.developmentDatasetId, dataset.familyId);
  const endpoint = `/api/deployments/${deployment.id}`;
  const draft = (version, extra = {}) => post(`${endpoint}/release-draft`, { alias: "production", agentVersionId: version, ...extra });
  const start = async (review, extra = {}, key = crypto.randomUUID()) => {
    const body = { alias: review.alias, agentVersionId: review.agentVersionId, developmentDatasetVersionId: review.developmentDatasetVersionId, regressionDatasetVersionId: review.regressionDatasetVersionId, expectedReleaseRevisionId: review.expectedReleaseRevisionId, expectedPolicyRevision: review.policy.revision, expectedCandidateRunId: review.candidateEvidence?.runId || "", expectedCurrentRunId: review.currentEvidence?.runId || "", trialCount: 1, actor: "pipeline-e2e", ...extra };
    return post(`${endpoint}/release-tasks`, body, { "idempotency-key": key });
  };
  const settled = async task => {
    if (task.runId) await completeRun(baseURL, task.runId);
    const deadline = Date.now() + 15000;
    while (Date.now() < deadline) {
      const value = await request(baseURL, `/api/release-tasks/${task.id}`);
      if (!["ready", "regression"].includes(value.status)) return value;
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    throw new Error(`Release did not settle: ${task.id}`);
  };
  const firstReview = await draft("agent-research-demo-v1");
  assert.equal(firstReview.regressionDatasetVersionId, dataset.id);
  const firstTask = await start(firstReview, {}, "first-release");
  const duplicate = await start(firstReview, {}, "first-release");
  assert.equal(firstTask.id, duplicate.id);
  const first = await settled(firstTask);
  assert.equal(first.status, "published");
  assert.equal(first.runRegression, true);
  const v2 = await post("/api/agent-families/agent-research-demo/versions", { runnerType: "mock", model: "deterministic-demo", description: "Pipeline candidate" });
  const comparison = await post(`${endpoint}/release-comparison`, { alias: "production", agentVersionId: v2.id });
  const comparisonView = await request(baseURL, `/api/experiments/${comparison.id}`);
  assert.equal(comparisonView.runs.length, 2);
  for (const run of comparisonView.runs) await completeRun(baseURL, run.id);
  const reviewed = await draft(v2.id);
  assert.equal(reviewed.comparable, true);
  assert.equal(reviewed.candidateEvidence.trialCount, reviewed.currentEvidence.trialCount);
  assert.equal(reviewed.candidateEvidence.evaluatorDigest, reviewed.currentEvidence.evaluatorDigest);
  const credential = await post("/api/serving-credentials", { tenantId: "local", name: "pipeline-e2e", deploymentIds: [deployment.id], scopes: ["responses:create", "responses:read"] });
  try {
    const invoke = previous => post("/v1/responses", { model: "pipeline-e2e@production", input: "pipeline", ...(previous ? { previous_response_id: previous.id } : {}) }, { authorization: `Bearer ${credential.apiKey}` });
    const before = await invoke();
    const upgraded = await settled(await start(reviewed));
    assert.equal(upgraded.status, "published");
    const after = await invoke();
    const continued = await invoke(before);
    assert.equal(after.deployment.agent_version_id, v2.id);
    assert.equal(continued.deployment.agent_version_id, "agent-research-demo-v1");
    const failed = await settled(await start(await draft(v2.id, { regressionDatasetVersionId: failedDataset.id })));
    assert.equal(failed.status, "blocked");
    const unchanged = await invoke();
    assert.equal(unchanged.deployment.release_revision_id, upgraded.releaseRevisionId);
    const noReason = await fetch(`${baseURL}/api/release-tasks/${failed.id}/override`, { method: "POST", headers: { "content-type": "application/json" }, body: "{}" });
    assert.equal(noReason.status, 400);
    const override = await post(`/api/release-tasks/${failed.id}/override`, { reason: "Known failing acceptance case" });
    assert.equal(override.status, "published");
    assert.equal(override.evaluation.passed, false);
    const skipped = await settled(await start(await draft(v2.id), { runRegression: false }));
    assert.equal(skipped.status, "published");
    assert.equal(skipped.runId, undefined);
    const prepared = await control("prepare_release", { deploymentId: deployment.id, agentVersionId: v2.id, alias: "production" });
    const state = await control("release_status", {});
    assert.deepEqual(state.command.result.tasks, []);
    const chat = await request(baseURL, `/api/experiments/${experiment.id}`);
    const bootstrap = await request(baseURL, "/api/bootstrap");
    assert.ok(!bootstrap.experiments.some(item => item.title === "发布回归"));
    assert.equal(chat.datasetVersionId, undefined);
    const snapshot = {
      defaults: { development: first.developmentDatasetVersionId === dataset.id, regression: first.regressionDatasetVersionId === dataset.id },
      comparison: { versions: comparisonView.runs.length, comparable: reviewed.comparable, currentPassed: reviewed.currentEvidence.passed, candidatePassed: reviewed.candidateEvidence.passed },
      pipeline: [first.status, upgraded.status, failed.status, override.status, skipped.status],
      regressionDefault: first.runRegression, duplicateSuppressed: duplicate.id === firstTask.id,
      failurePreservesAlias: unchanged.deployment.release_revision_id === upgraded.releaseRevisionId,
      continuationPinned: continued.deployment.release_revision_id === before.deployment.release_revision_id,
      chat: { created: created.command.result.accepted, prepared: prepared.command.result.state, events: chat.controlEvents.map(e => e.type), datasetUnchanged: chat.datasetVersionId === undefined },
    };
    assert.deepEqual(snapshot, JSON.parse(await readFile(new URL("snapshots/release-pipeline.json", import.meta.url), "utf8")));
  } finally { await post(`/api/serving-credentials/${credential.id}/revoke?tenantId=local`, {}); }
}
