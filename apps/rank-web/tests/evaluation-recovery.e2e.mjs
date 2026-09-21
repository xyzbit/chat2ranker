import assert from "node:assert/strict";

/** Kill the packaged coordinator while real candidate/judge submission replies are lost. */
export async function verifyEvaluationRecovery({ baseURL, request, completeRun, proxy, restartRank }) {
  const post = (path, body, headers = {}) => request(baseURL, path, { method: "POST", headers, body: JSON.stringify(body) });
  const snapshot = [];
  for (const kind of ["agent", "judge"]) {
    const marker = `durable-evaluation-${kind}`;
    const dataset = await post("/api/datasets", { name: marker, rubric: { criteria: [
      { id: "quality", name: "Quality", description: "Complete the task", weight: 1, threshold: 0.7, critical: true },
    ] }, cases: [{ id: "one", input: marker, expected: { outputContains: marker } }] });
    const experiment = await post("/api/experiments", { title: marker });
    await request(baseURL, `/api/experiments/${experiment.id}`, { method: "PATCH", body: JSON.stringify({ datasetVersionId: dataset.id, agentVersionId: "agent-research-demo-v1" }) });
    const seen = proxy.arm(marker, "after_acceptance", kind);
    const run = await post(`/api/experiments/${experiment.id}/runs`, { trialCount: 1 }, { "idempotency-key": marker });
    let timer;
    try {
      await Promise.race([seen, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error(`evaluation fault not reached: ${kind}`)), 10000); })]);
    } finally { clearTimeout(timer); }
    await restartRank(() => proxy.clear());
    const final = await completeRun(baseURL, run.id);
    assert.equal(final.status, "complete");
    assert.equal(final.validTrials, 1);
    const trial = final.results[0].trials[0];
    assert.equal(trial.judgeExecutionIds.length, 1);
    const submissions = proxy.accepted.filter(item => item.spec.metadata.runId === run.id);
    const candidates = submissions.filter(item => item.spec.kind === "agent");
    const judges = submissions.filter(item => item.spec.kind === "judge");
    assert.ok(candidates.length > 0 && judges.length > 0);
    assert.ok(candidates.every(item => item.id === trial.candidateExecutionId));
    assert.ok(judges.every(item => item.id === trial.judgeExecutionIds[0]));
    const completed = final.events.filter(event => event.type === "trial.completed").length;
    const verdicts = final.events.filter(event => event.type === "judge.verdict").length;
    assert.equal(completed, 1);
    assert.equal(verdicts, 1);
    const cached = await post(`/api/experiments/${experiment.id}/runs`, { trialCount: 1 }, { "idempotency-key": marker });
    assert.equal(cached.id, run.id);
    assert.equal(cached.cost, final.cost);
    snapshot.push({ interrupted: kind, status: final.status, validTrials: final.validTrials,
      candidateExecutions: new Set(candidates.map(item => item.id)).size,
      judgeExecutions: new Set(judges.map(item => item.id)).size, completed, verdicts,
      resultAndCostRetained: cached.id === final.id && cached.cost === final.cost });
  }
  return snapshot;
}
