import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

/** Runs the source-invocation to immutable dataset to evaluation path through real services. */
export async function verifyResponseFeedback({ baseURL, request, command, completeRun }) {
  const post = (path, body, headers = {}) => request(baseURL, path, { method: "POST", headers, body: JSON.stringify(body) });
  const bootstrap = await request(baseURL, "/api/bootstrap");
  const endpoint = bootstrap.deployments.find(d => d.name === "research-public");
  const credential = await post("/api/serving-credentials", { tenantId: "local", name: "feedback-acceptance", deploymentIds: [endpoint.id], scopes: ["responses:create"] });
  try {
    const source = await post("/v1/responses", { model: endpoint.name, input: "Contact person@example.com about shipping.", instructions: "Answer in one sentence.", metadata: { scenario: "feedback" } }, { authorization: `Bearer ${credential.apiKey}` });
    const detail = await request(baseURL, `/api/responses/${source.id}`);
    assert.equal(detail.instructions, "Answer in one sentence.");
    const page = await request(baseURL, `/api/responses?query=${source.id}`);
    assert.equal(page.items.length, 1);
    const dataset = await post(`/api/responses/${source.id}/dataset-cases`, { datasetName: "Customer feedback", title: "Delivery response", input: "Contact [email] about shipping.", expected: "Explain the shipping options.", reviewed: true });
    assert.ok(!JSON.stringify(dataset).includes("person@example.com"));
    assert.equal(dataset.cases[0].source.responseId, source.id);
    let experiment = await post("/api/experiments", { title: "Evaluate captured feedback" });
    experiment = await command(baseURL, experiment, "select_agent", { agentVersionId: source.deployment.agent_version_id });
    experiment = await command(baseURL, experiment, "select_dataset", { datasetVersionId: dataset.id });
    const started = await post(`/api/experiments/${experiment.id}/runs`, { trialCount: 1 }, { "idempotency-key": crypto.randomUUID() });
    const run = await completeRun(baseURL, started.id);
    assert.equal(run.status, "complete");
    assert.equal(run.validTrials, 1);
    assert.equal(run.datasetSnapshot.cases[0].source.responseId, source.id);
    const second = await post("/v1/responses", { model: endpoint.name, input: "Second shipping case" }, { authorization: `Bearer ${credential.apiKey}` });
    const appended = await post(`/api/responses/${second.id}/dataset-cases`, { baseDatasetVersionId: dataset.id, title: "Second shipping case", input: "Second reviewed input", expected: "Answer clearly", reviewed: true });
    const frozenRun = await request(baseURL, `/api/runs/${run.id}`);
    assert.equal(frozenRun.datasetSnapshot.cases.length, 1);
    const duplicate = await fetch(`${baseURL}/api/responses/${second.id}/dataset-cases`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ baseDatasetVersionId: appended.id, title: "duplicate", input: "duplicate", expected: "duplicate", reviewed: true }) });
    assert.equal(duplicate.status, 409);
    const transcript = {
      source: { status: source.status, searchable: page.items.length === 1, instructions: detail.instructions },
      captured: { version: dataset.version, input: dataset.cases[0].input, expected: dataset.cases[0].expected, provenance: !!dataset.cases[0].source.responseId },
      evaluation: { status: run.status, total: run.total, validTrials: run.validTrials, datasetVersion: run.datasetSnapshot.version, sourceRetained: true },
      append: { version: appended.version, cases: appended.cases.length, oldRunCases: frozenRun.datasetSnapshot.cases.length, duplicateStatus: duplicate.status },
    };
    assert.deepEqual(transcript, JSON.parse(await readFile(new URL("snapshots/response-feedback.json", import.meta.url), "utf8")));
  } finally { await post(`/api/serving-credentials/${credential.id}/revoke`, {}); }
}
