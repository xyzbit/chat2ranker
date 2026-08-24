import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtemp, mkdir, readFile, readdir, rm } from "node:fs/promises";
import { createServer } from "node:net";
import os from "node:os";
import path from "node:path";
import test from "node:test";

const appRoot = path.resolve(import.meta.dirname, "..");
const repositoryRoot = path.resolve(appRoot, "../..");
const backendRoot = path.join(repositoryRoot, "rank/backend");
const executionBackendRoot = path.join(repositoryRoot, "execution/backend");

async function freePort() {
  const server = createServer();
  await new Promise((resolve, reject) => server.listen(0, "127.0.0.1", resolve).once("error", reject));
  const { port } = server.address();
  await new Promise((resolve) => server.close(resolve));
  return port;
}

function launch(command, args, options) {
  const child = spawn(command, args, { ...options, detached: process.platform !== "win32", stdio: ["ignore", "pipe", "pipe"] });
  let output = "";
  child.stdout.on("data", (chunk) => { output += chunk; });
  child.stderr.on("data", (chunk) => { output += chunk; });
  child.output = () => output;
  return child;
}

async function stop(child) {
  if (!child || child.exitCode != null) return;
  if (process.platform === "win32") child.kill("SIGTERM");
  else process.kill(-child.pid, "SIGTERM");
  await Promise.race([
    new Promise((resolve) => child.once("close", resolve)),
    new Promise((_, reject) => setTimeout(() => reject(new Error(`process did not stop:\n${child.output()}`)), 8_000)),
  ]);
}

async function build(output, target, cwd = backendRoot) {
  await new Promise((resolve, reject) => {
    const child = spawn("go", ["build", "-o", output, target], { cwd, stdio: ["ignore", "pipe", "pipe"] });
    let details = "";
    child.stdout.on("data", (chunk) => { details += chunk; });
    child.stderr.on("data", (chunk) => { details += chunk; });
    child.once("exit", (code) => code === 0 ? resolve() : reject(new Error(details)));
    child.once("error", reject);
  });
}

async function request(baseURL, pathname, options = {}) {
  const response = await fetch(baseURL + pathname, { ...options, headers: { "content-type": "application/json", ...options.headers } });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(payload.error?.message || `HTTP ${response.status}`);
  return payload;
}

async function waitFor(baseURL, pathname, processForError) {
  const deadline = Date.now() + 20_000;
  let lastError;
  while (Date.now() < deadline) {
    try {
      return await request(baseURL, pathname);
    } catch (error) {
      lastError = error;
      await new Promise((resolve) => setTimeout(resolve, 80));
    }
  }
  throw new Error(`${lastError?.message || "endpoint unavailable"}\n${processForError.output()}`);
}

async function command(baseURL, experiment, type, payload = {}) {
  const action = experiment.a2ui.actions[type];
  const value = await request(baseURL, `/api/experiments/${experiment.id}/commands`, {
    method: "POST",
    headers: { "idempotency-key": `${type}-${crypto.randomUUID()}`, "x-rank-action-token": action.token },
    body: JSON.stringify({ type: action.command, actionToken: action.token, payload }),
  });
  return value.experiment;
}

async function completeRun(baseURL, runID) {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    const run = await request(baseURL, `/api/runs/${runID}`);
    if (!["queued", "preparing", "running", "scoring"].includes(run.status)) return run;
    await new Promise((resolve) => setTimeout(resolve, 80));
  }
  throw new Error("run did not reach a terminal state");
}

async function collectEvents(baseURL, pathname) {
  const response = await fetch(baseURL + pathname, { headers: { accept: "text/event-stream" } });
  if (!response.ok) throw new Error(`event stream returned HTTP ${response.status}`);
  const text = await response.text();
  return text.split(/\r?\n/).filter((line) => line.startsWith("data: ")).map((line) => JSON.parse(line.slice(6)));
}

async function fileCount(directory) {
  let count = 0;
  for (const entry of await readdir(directory, { withFileTypes: true }).catch(() => [])) {
    if (entry.isDirectory()) count += await fileCount(path.join(directory, entry.name));
    else count += 1;
  }
  return count;
}

test("Rank runs candidate and Judge through executiond with isolated workers and readable artifacts", { timeout: 90_000 }, async () => {
  const temporaryRoot = await mkdtemp(path.join(os.tmpdir(), "chat2ranker-stage5-"));
  const binaryRoot = path.join(temporaryRoot, "bin");
  await mkdir(binaryRoot);
  const rankdBinary = path.join(binaryRoot, "rankd");
  const executiondBinary = path.join(binaryRoot, "executiond");
  const workerBinary = path.join(binaryRoot, "execution-worker");
  await Promise.all([
    build(rankdBinary, "./cmd/rankd"),
    build(executiondBinary, "./cmd/executiond", executionBackendRoot),
    build(workerBinary, "./cmd/execution-worker", executionBackendRoot),
  ]);
  const [port, executionPort] = await Promise.all([freePort(), freePort()]);
  const baseURL = `http://127.0.0.1:${port}`;
  const executionURL = `http://127.0.0.1:${executionPort}`;
  const artifactRoot = path.join(temporaryRoot, "artifacts");
  const sandboxRoot = path.join(temporaryRoot, "sandboxes");
  const environment = { ...process.env, EXECUTION_REPO_ROOT: repositoryRoot };
  delete environment.DEEPSEEK_API_KEY;
  const executiond = launch(executiondBinary, [
    "-addr", `127.0.0.1:${executionPort}`,
    "-db", path.join(temporaryRoot, "execution.db"),
    "-worker", workerBinary,
    "-repo-root", repositoryRoot,
    "-artifacts", artifactRoot,
    "-sandboxes", sandboxRoot,
  ], { cwd: repositoryRoot, env: environment });
  const rankd = launch(rankdBinary, [
    "-addr", `127.0.0.1:${port}`,
    "-db", path.join(temporaryRoot, "rank.db"),
    "-execution-url", executionURL,
  ], { cwd: repositoryRoot, env: environment });
  try {
    await waitFor(executionURL, "/v1/health", executiond);
    await waitFor(baseURL, "/api/health", rankd);
    const bootstrap = await request(baseURL, "/api/bootstrap");
    const demo = bootstrap.agents.find((agent) => agent.id === "agent-research-demo-v1");
    const dsh = bootstrap.agents.find((agent) => agent.id === "agent-dsh-research-v1");
    assert.equal(demo.runtime.available, true);
    assert.equal(dsh.runtime.available, false);

    let experiment = await request(baseURL, "/api/experiments", { method: "POST", body: JSON.stringify({ title: "Stage 5 E2E" }) });
    experiment = await command(baseURL, experiment, "select_dataset", { datasetVersionId: "dataset-web-research-v3" });
    experiment = await command(baseURL, experiment, "select_agent", { agentVersionId: "agent-research-demo-v1" });
    const started = await request(baseURL, `/api/experiments/${experiment.id}/commands`, {
      method: "POST",
      headers: { "idempotency-key": `start-${crypto.randomUUID()}`, "x-rank-action-token": experiment.a2ui.actions.start_run.token },
      body: JSON.stringify({ type: "start_run", actionToken: experiment.a2ui.actions.start_run.token, payload: { trialCount: 5 } }),
    });
    const runEventsPromise = collectEvents(baseURL, `/api/runs/${started.run.id}/events?after=0`);
    const run = await completeRun(baseURL, started.run.id);
    const streamedRunEvents = await runEventsPromise;
    assert.equal(run.status, "complete", run.error);
    assert.equal(run.passed, 50);
    assert.equal(run.total, 60);
    assert.equal(run.passRate, 83);
    assert.equal(run.reliableCases, 10);
    assert.equal(run.caseCount, 12);
    assert.equal(run.passHat3, 83.3);
    assert.equal(run.evaluationComplete, true);
    assert.equal(run.costKnown, true);
    assert.equal(run.results.length, 12);
    const executionIDs = run.results.flatMap((result) => result.trials.flatMap((trial) => [trial.candidateExecutionId, ...trial.judgeExecutionIds]));
    const sourceEventStreams = await Promise.all(executionIDs.map((executionID) => collectEvents(executionURL, `/v1/executions/${executionID}/events?after=0`)));
    const sourceTerminalEvents = sourceEventStreams.filter((events) => events.at(-1)?.type === "execution.completed").length;
    assert.equal(sourceTerminalEvents, 120);
    const eventCounts = Object.fromEntries(["candidate.completed", "candidate.harness.output", "judge.completed", "judge.verdict"].map((type) => [type, streamedRunEvents.filter((event) => event.type === type).length]));
    const persistedCounts = Object.fromEntries(["candidate.completed", "candidate.harness.output", "judge.completed", "judge.verdict"].map((type) => [type, run.events.filter((event) => event.type === type).length]));
    const eventDiagnostics = JSON.stringify({ eventCounts, persistedCounts, sourceTerminalEvents });
    assert.equal(eventCounts["candidate.completed"], 60, eventDiagnostics);
    assert.equal(eventCounts["candidate.harness.output"], 60, eventDiagnostics);
    assert.equal(eventCounts["judge.completed"], 60, eventDiagnostics);
    assert.equal(eventCounts["judge.verdict"], 60, eventDiagnostics);
    assert.equal(streamedRunEvents.at(-1)?.type, "run.completed");
    assert.deepEqual(streamedRunEvents.map((event) => event.sequence), [...streamedRunEvents.keys()].map((index) => index + 1));
    for (const result of run.results) {
      assert.match(result.executionId, /^exec-/);
      assert.match(result.judgeExecutionId, /^exec-/);
      assert.notEqual(result.executionId, result.judgeExecutionId);
      assert.equal(result.trials.length, 5);
      assert.ok(result.artifacts.length >= 20);
    }
    const snapshot = {
      summary: {
        status: run.status,
        passed: run.passed,
        total: run.total,
        passRate: run.passRate,
        reliableCases: run.reliableCases,
        caseCount: run.caseCount,
        passHat3: run.passHat3,
        cost: Number(run.cost.toFixed(3)),
        costKnown: run.costKnown,
      },
      failures: run.results.filter((result) => !result.passed).map((result) => ({
        caseId: result.caseId,
        title: result.title,
        reason: result.reason,
        cost: result.cost,
      })),
      isolation: {
        candidateWorkers: run.results.flatMap((result) => result.trials).filter((trial) => /^exec-/.test(trial.candidateExecutionId)).length,
        judgeWorkers: run.results.flatMap((result) => result.trials).filter((trial) => trial.judgeExecutionIds.some((id) => /^exec-/.test(id))).length,
        artifactsPerCase: run.results.map((result) => result.artifacts.length),
      },
    };
    const expectedSnapshot = JSON.parse(await readFile(path.join(appRoot, "tests/snapshots/stage5-demo-run.json"), "utf8"));
    assert.deepEqual(snapshot, expectedSnapshot);
    const failure = run.results.find((result) => !result.passed);
    const artifact = failure.artifacts.find((item) => item.kind === "result");
    const content = await request(baseURL, `/api/runs/${run.id}/artifacts?caseId=${encodeURIComponent(failure.caseId)}&path=${encodeURIComponent(artifact.path)}`);
    assert.match(content.content, /"protocolVersion":1/);
    const executionEvents = await collectEvents(executionURL, `/v1/executions/${failure.executionId}/events?after=0`);
    assert.deepEqual(executionEvents.map((event) => event.type), ["execution.queued", "execution.running", "harness.started", "harness.output", "execution.completed"]);
    assert.equal(await fileCount(sandboxRoot), 0);

    let cancelledExperiment = await request(baseURL, "/api/experiments", { method: "POST", body: JSON.stringify({ title: "Cancellation" }) });
    cancelledExperiment = await command(baseURL, cancelledExperiment, "select_dataset", { datasetVersionId: "dataset-web-research-v3" });
    cancelledExperiment = await command(baseURL, cancelledExperiment, "select_agent", { agentVersionId: "agent-research-demo-v1" });
    const cancelledStart = await request(baseURL, `/api/experiments/${cancelledExperiment.id}/runs`, { method: "POST", headers: { "idempotency-key": `cancel-${crypto.randomUUID()}` }, body: "{}" });
    await request(baseURL, `/api/runs/${cancelledStart.id}/cancel`, { method: "POST", body: "{}" });
    const cancelled = await completeRun(baseURL, cancelledStart.id);
    assert.equal(cancelled.status, "cancelled");
  } finally {
    await stop(rankd).catch(() => {});
    await stop(executiond).catch(() => {});
    await rm(temporaryRoot, { recursive: true, force: true });
  }
});
