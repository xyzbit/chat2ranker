import { verifyModelResolution } from './model-resolution.e2e.mjs';
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdtemp, mkdir, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { createServer as createHTTPServer } from "node:http";
import { createServer } from "node:net";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { verifyReleaseLifecycle } from "./release-lifecycle.e2e.mjs";
import { verifyReleasePipeline } from "./release-pipeline.e2e.mjs";
import { verifyTrials } from "./trials.e2e.mjs";
import { verifyEvaluationRecovery } from "./evaluation-recovery.e2e.mjs";
import { startServingFaultProxy, verifyServingRecovery } from "./serving-recovery.e2e.mjs";
import { verifyResponseFeedback } from "./response-feedback.e2e.mjs";

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

async function prepareRankStorage(environment, rankBinary) {
  delete environment.RANK_DATABASE_URL;
  delete environment.EXECUTION_DATABASE_URL;
  for (const name of Object.keys(environment)) if (name.startsWith("EXECUTION_S3_")) delete environment[name];
  if (!process.env.RANK_TEST_DATABASE_URL) return async () => {};
  const schema = `rank_snapshot_${randomBytes(8).toString("hex")}`;
  environment.RANK_DATABASE_URL = process.env.RANK_TEST_DATABASE_URL;
  environment.RANK_POSTGRES_SCHEMA = schema;
  environment.RANK_ACTION_SECRET = randomBytes(32).toString("hex");
  environment.RANK_CONTROL_TOKEN = randomBytes(32).toString("hex");
  environment.EXECUTION_SERVICE_TOKEN = randomBytes(32).toString("hex");
  const finish = child => new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("exit", code => code === 0 ? resolve() : reject(new Error(child.output())));
  });
  await finish(launch(rankBinary, ["-migrate"], { cwd: repositoryRoot, env: environment }));
  return () => finish(launch("go", ["run", "./internal/testdb/cleanup", schema], { cwd: backendRoot, env: environment }));
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

async function verifyReleaseBrowser(baseURL, script = "release-policy.browser.js") {
  const port = await freePort();
  const url = `http://127.0.0.1:${port}`;
  const vite = launch(process.execPath, [path.join(appRoot, "node_modules/vite/bin/vite.js"), "--host", "127.0.0.1", "--port", String(port), "--strictPort"], { cwd: appRoot, env: { ...process.env, RANK_API_URL: baseURL } });
  const wrapper = path.join(os.homedir(), ".codex/skills/playwright/scripts/playwright_cli.sh");
  async function cli(...args) {
    const child = launch(wrapper, [`-s=rank-release-${port}`, ...args], { cwd: repositoryRoot, env: process.env });
    await new Promise((resolve, reject) => {
      child.once("error", reject);
      child.once("exit", code => code === 0 && !child.output().includes("### Error") ? resolve() : reject(new Error(child.output())));
    });
    console.log(child.output());
  }
  try {
    await waitFor(url, "/api/health", vite);
    await mkdir(path.join(repositoryRoot, "output/playwright"), { recursive: true });
    await cli("open", `${url}/#/deployments`);
    await cli("run-code", "--filename", path.join(appRoot, "tests", script));
  } catch (error) {
    await cli("snapshot");
    await cli("screenshot", "--filename", "output/playwright/release-error.png");
    throw error;
  } finally {
    await cli("close");
    await stop(vite);
  }
}

test("Conversation configuration forms persist changes without exposing secrets to chat", { timeout: 120_000, skip: process.env.RANK_CONFIG_BROWSER !== "1" }, async () => {
  const temporaryRoot = await mkdtemp(path.join(os.tmpdir(), "rank-configuration-"));
  const rankBinary = path.join(temporaryRoot, "rankd");
  const executionBinary = path.join(temporaryRoot, "executiond");
  await Promise.all([build(rankBinary, "./cmd/rankd"), build(executionBinary, "./cmd/executiond", executionBackendRoot)]);
  const [port, executionPort] = await Promise.all([freePort(), freePort()]);
  const baseURL = `http://127.0.0.1:${port}`;
  const executionURL = `http://127.0.0.1:${executionPort}`;
  const environment = { ...process.env, RANK_CONTROL_TOKEN: "configuration-browser-fixture", EXECUTION_REPO_ROOT: repositoryRoot };
  const execution = launch(executionBinary, ["-addr", `127.0.0.1:${executionPort}`, "-db", path.join(temporaryRoot, "execution.db"), "-repo-root", repositoryRoot, "-artifacts", path.join(temporaryRoot, "artifacts"), "-sandboxes", path.join(temporaryRoot, "sandboxes"), "-credentials", path.join(temporaryRoot, "credentials")], { cwd: repositoryRoot, env: environment });
  const rank = launch(rankBinary, ["-addr", `127.0.0.1:${port}`, "-db", path.join(temporaryRoot, "rank.db"), "-execution-url", executionURL, "-webhook-poll-interval", "1h"], { cwd: repositoryRoot, env: environment });
  try {
    await waitFor(executionURL, "/v1/health", execution);
    await waitFor(baseURL, "/api/health", rank);
    await verifyReleaseBrowser(baseURL, "conversation-configuration.browser.js");
  } finally {
    await stop(rank); await stop(execution);
    await rm(temporaryRoot, { recursive: true, force: true });
  }
});

test("Agent playground runs isolated conversations through the serving execution lifecycle", { timeout: 180_000 }, async () => {
  const temporaryRoot = await mkdtemp(path.join(os.tmpdir(), "rank-trials-"));
  const rankBinary = path.join(temporaryRoot, "rankd");
  const executionBinary = path.join(temporaryRoot, "executiond");
  const worker = path.join(temporaryRoot, "worker");
  await Promise.all([build(rankBinary, "./cmd/rankd"), build(executionBinary, "./cmd/executiond", executionBackendRoot), build(worker, "./cmd/execution-worker", executionBackendRoot)]);
  const [port, executionPort] = await Promise.all([freePort(), freePort()]);
  const baseURL = `http://127.0.0.1:${port}`;
  const executionURL = `http://127.0.0.1:${executionPort}`;
  await writeFile(path.join(temporaryRoot, "codex"), await readFile(path.join(appRoot, "tests/fixtures/trial-codex.mjs")), { mode: 0o755 });
  const environment = { ...process.env, PATH: `${temporaryRoot}${path.delimiter}${process.env.PATH}`, EXECUTION_REPO_ROOT: repositoryRoot, DEEPSEEK_API_KEY: "keyless-trial-fixture", RANK_DSH_BIN: path.join(appRoot, "tests/fixtures/trial-dsh.mjs"), DSH_HOME: path.join(temporaryRoot, "dsh"), CODEX_HOME: path.join(temporaryRoot, "codex-home") };
  const cleanupStorage = await prepareRankStorage(environment, rankBinary);
  const execution = launch(executionBinary, ["-addr", `127.0.0.1:${executionPort}`, "-db", path.join(temporaryRoot, "execution.db"), "-worker", worker, "-repo-root", repositoryRoot, "-artifacts", path.join(temporaryRoot, "artifacts"), "-sandboxes", path.join(temporaryRoot, "sandboxes"), "-credentials", path.join(temporaryRoot, "credentials")], { cwd: repositoryRoot, env: environment });
  const start = () => launch(rankBinary, ["-addr", `127.0.0.1:${port}`, "-db", path.join(temporaryRoot, "rank.db"), "-execution-url", executionURL, "-webhook-poll-interval", "1h", "-webhook-secret-dir", path.join(temporaryRoot, "webhook-secrets")], { cwd: repositoryRoot, env: environment });
  let rank = start();
  const restart = async () => { await stop(rank); rank = start(); await waitFor(baseURL, "/api/health", rank); };
  try {
    await waitFor(executionURL, "/v1/health", execution);
    const health = await waitFor(baseURL, "/api/health", rank);
    assert.equal(health.storage, process.env.RANK_TEST_DATABASE_URL ? "postgres" : "sqlite");
    await verifyTrials({ baseURL, request, restart, controlToken: environment.RANK_CONTROL_TOKEN });
    if (process.env.RANK_TRIAL_BROWSER === "1") await verifyReleaseBrowser(baseURL, "trials.browser.js");
  } finally {
    await stop(rank); await stop(execution);
    await cleanupStorage();
    await rm(temporaryRoot, { recursive: true, force: true });
  }
});

test("Rank runs candidate and Judge through executiond with isolated workers and readable artifacts", { timeout: 90_000 }, async () => {
  const temporaryRoot = await mkdtemp(path.join(os.tmpdir(), "agentkiln-stage5-"));
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
  environment.CODEX_HOME = path.join(temporaryRoot, "codex");
  environment.DSH_HOME = path.join(temporaryRoot, "dsh");
  environment.RANK_DISCOVERY_FIXTURE_KEY = "keyless-discovery-secret";
  await mkdir(environment.CODEX_HOME);
  await mkdir(environment.DSH_HOME);
  await writeFile(path.join(environment.CODEX_HOME, "config.toml"), 'model = "fixture-model"\nmodel_provider = "fixture"\n[model_providers.fixture]\nbase_url = "https://fixture.example/v1"\nenv_key = "RANK_DISCOVERY_FIXTURE_KEY"\nwire_api = "responses"\n');
  await writeFile(path.join(environment.DSH_HOME, "settings.yaml"), "broken: [yaml");
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
  let providerServer;
  try {
    await waitFor(executionURL, "/v1/health", executiond);
    await waitFor(baseURL, "/api/health", rankd);
    const bootstrap = await request(baseURL, "/api/bootstrap");
    const deepseekCatalog = bootstrap.modelCatalog.find((provider) => provider.id === "deepseek");
    assert.equal(deepseekCatalog.baseUrl, "https://api.deepseek.com");
    assert.ok(deepseekCatalog.models.some((model) => model.id === "deepseek-v4-flash" && model.price.input > 0));
    const catalogSnapshot = bootstrap.modelCatalog.map((provider) => ({
      id: provider.id,
      baseUrl: provider.baseUrl,
      protocol: provider.protocol,
      sourceUrl: provider.sourceUrl,
      defaultModel: provider.models[0]?.id,
      defaultPrice: provider.models[0]?.price,
      modelCount: provider.models.length,
    }));
    assert.deepEqual(catalogSnapshot, JSON.parse(await readFile(path.join(appRoot, "tests/snapshots/model-catalog.json"), "utf8")));
    const discovery = await request(baseURL, "/api/local-model-connections/discover", { method: "POST", body: "{}" });
    assert.equal(discovery.candidates.length, 1);
    assert.equal(JSON.stringify(discovery).includes(environment.RANK_DISCOVERY_FIXTURE_KEY), false);
    assert.equal((await request(baseURL, "/api/bootstrap")).modelConnections.length, 0);
    const candidate = discovery.candidates[0];
    const imported = await request(baseURL, `/api/local-model-connections/${candidate.id}/import`, { method: "POST", body: "{}" });
    const duplicate = await request(baseURL, `/api/local-model-connections/${candidate.id}/import`, { method: "POST", body: "{}" });
    assert.equal(imported.id, duplicate.id);
    const customImported = await request(baseURL, `/api/local-model-connections/${candidate.id}/import`, { method: "POST", body: JSON.stringify({ model: "custom-fixture-model" }) });
    assert.notEqual(customImported.id, imported.id);
    assert.equal(customImported.defaultModel, "custom-fixture-model");
    const customAgain = await request(baseURL, `/api/local-model-connections/${candidate.id}/import`, { method: "POST", body: JSON.stringify({ model: "custom-fixture-model" }) });
    assert.equal(customAgain.id, customImported.id);
    const rediscovered = await request(baseURL, "/api/local-model-connections/discover", { method: "POST", body: "{}" });
    const discoverySnapshot = {
      candidates: discovery.candidates.map(({ source, name, protocol, baseUrl, model }) => ({ source, name, protocol, baseUrl, model })),
      issues: discovery.issues,
      imported: { hasCredential: imported.hasCredential, verified: imported.status === "verified", reused: rediscovered.candidates[0].connectionId === imported.id },
      customModel: customImported.defaultModel,
      systemModels: (await request(baseURL, "/api/bootstrap")).systemModels,
    };
    assert.deepEqual(discoverySnapshot, JSON.parse(await readFile(path.join(appRoot, "tests/snapshots/local-model-discovery.json"), "utf8")));
    await assert.rejects(request(baseURL, "/api/local-model-connections/unknown/import", { method: "POST", body: "{}" }), /本地配置已变化/);
    const metadataOnlyConnection = await request(baseURL, "/api/model-connections", {
      method: "POST",
      body: JSON.stringify({ name: "E2E Gateway", provider: "custom", protocol: "openai-chat-completions", baseUrl: "https://example.invalid/v1", defaultModel: "e2e-model", prices: { "e2e-model": { input: 1, output: 2, cacheRead: .5, cacheWrite: 0 } } }),
    });
    assert.equal(metadataOnlyConnection.status, "missing_credential");
    assert.equal(metadataOnlyConnection.hasCredential, false);
    assert.equal(metadataOnlyConnection.prices["e2e-model"].output, 2);
    const afterConnection = await request(baseURL, "/api/bootstrap");
    assert.ok(afterConnection.modelConnections.some((connection) => connection.id === metadataOnlyConnection.id));
    let providerModels = ["catalog-a", "account-new"];
    let providerFailure = false;
    providerServer = createHTTPServer((incoming, outgoing) => {
      outgoing.setHeader("content-type", "application/json");
      if (providerFailure) {
        outgoing.statusCode = 503;
        outgoing.end(JSON.stringify({ error: { message: "temporary failure" } }));
      } else if (incoming.url === "/v1/models") {
        outgoing.end(JSON.stringify({ data: providerModels.map((id) => ({ id })) }));
      } else if (incoming.url === "/v1/chat/completions") {
        outgoing.end(JSON.stringify({ choices: [] }));
      } else {
        outgoing.statusCode = 404;
        outgoing.end(JSON.stringify({ error: "not found" }));
      }
    });
    await new Promise((resolve, reject) => providerServer.listen(0, "127.0.0.1", resolve).once("error", reject));
    const providerURL = `http://127.0.0.1:${providerServer.address().port}/v1`;
    const syncConnection = await request(baseURL, "/api/model-connections", { method: "POST", body: JSON.stringify({ name: "Sync E2E", provider: "custom", protocol: "openai-chat-completions", baseUrl: providerURL, apiKey: "sync-secret", defaultModel: "catalog-a" }) });
    let synced = await request(baseURL, `/api/model-connections/${syncConnection.id}/verify`, { method: "POST", body: "{}" });
    assert.deepEqual(synced.models, ["account-new", "catalog-a"]);
    providerModels = ["catalog-a", "account-latest"];
    synced = await request(baseURL, `/api/model-connections/${syncConnection.id}/verify`, { method: "POST", body: "{}" });
    assert.deepEqual(synced.models, ["account-latest", "catalog-a"]);
    const modelResolution = await verifyModelResolution({ baseURL, executionURL, request, connection: syncConnection, providerURL });
    if (process.env.RANK_RECORD_MODEL_SNAPSHOT === '1') await writeFile(path.join(appRoot, 'tests/snapshots/model-resolution.json'), JSON.stringify(modelResolution, null, 2) + '\n');
    assert.deepEqual(modelResolution, JSON.parse(await readFile(path.join(appRoot, 'tests/snapshots/model-resolution.json'), 'utf8')));
    providerFailure = true;
    await assert.rejects(request(baseURL, `/api/model-connections/${syncConnection.id}/verify`, { method: "POST", body: "{}" }), /temporary failure/);
    const afterFailedSync = await request(baseURL, "/api/bootstrap");
    assert.deepEqual(afterFailedSync.modelConnections.find((connection) => connection.id === syncConnection.id).models, ["account-latest", "catalog-a"]);
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
    assert.match(content.content, /"protocolVersion":3/);
    const executionEvents = await collectEvents(executionURL, `/v1/executions/${failure.executionId}/events?after=0`);
    assert.deepEqual(executionEvents.map((event) => event.type), ["execution.queued", "execution.running", "harness.started", "harness.output", "execution.completed"]);
    assert.equal(await fileCount(sandboxRoot), 0);

    const variant = await request(baseURL, "/api/agents", { method: "POST", body: JSON.stringify({ name: "Comparison Variant", handle: "@demo/comparison-variant", runnerType: "mock", model: "deterministic-demo" }) });
    let comparisonExperiment = await request(baseURL, "/api/experiments", { method: "POST", body: JSON.stringify({ title: "Multi-Agent Comparison" }) });
    comparisonExperiment = await command(baseURL, comparisonExperiment, "select_dataset", { datasetVersionId: "dataset-web-research-v3" });
    comparisonExperiment = await command(baseURL, comparisonExperiment, "select_agent", { agentVersionId: "agent-research-demo-v1" });
    const comparisonStart = await request(baseURL, `/api/experiments/${comparisonExperiment.id}/commands`, {
      method: "POST",
      headers: { "idempotency-key": `compare-${crypto.randomUUID()}`, "x-rank-action-token": comparisonExperiment.a2ui.actions.start_run.token },
      body: JSON.stringify({ type: "start_run", actionToken: comparisonExperiment.a2ui.actions.start_run.token, payload: { trialCount: 1, agentVersionIds: ["agent-research-demo-v1", variant.id] } }),
    });
    const comparisonRuns = await Promise.all(comparisonStart.run.runs.map((item) => completeRun(baseURL, item.id)));
    assert.equal(comparisonStart.run.runGroup.runIds.length, 2);
    assert.deepEqual(comparisonRuns.map((item) => item.groupId), [comparisonStart.run.runGroup.id, comparisonStart.run.runGroup.id]);
    assert.deepEqual(comparisonRuns.map((item) => item.agentSnapshot.id), ["agent-research-demo-v1", variant.id]);
    assert.deepEqual(comparisonRuns.map((item) => item.total), [12, 12]);

    let cancelledExperiment = await request(baseURL, "/api/experiments", { method: "POST", body: JSON.stringify({ title: "Cancellation" }) });
    cancelledExperiment = await command(baseURL, cancelledExperiment, "select_dataset", { datasetVersionId: "dataset-web-research-v3" });
    cancelledExperiment = await command(baseURL, cancelledExperiment, "select_agent", { agentVersionId: "agent-research-demo-v1" });
    const cancelledStart = await request(baseURL, `/api/experiments/${cancelledExperiment.id}/runs`, { method: "POST", headers: { "idempotency-key": `cancel-${crypto.randomUUID()}` }, body: "{}" });
    await request(baseURL, `/api/runs/${cancelledStart.id}/cancel`, { method: "POST", body: "{}" });
    const cancelled = await completeRun(baseURL, cancelledStart.id);
    assert.equal(cancelled.status, "cancelled");
  } finally {
    if (providerServer) await new Promise((resolve) => providerServer.close(resolve));
    await stop(rankd).catch(() => {});
    await stop(executiond).catch(() => {});
    await rm(temporaryRoot, { recursive: true, force: true });
  }
});

test("Serving executes Responses and evaluates, publishes, updates and rolls back pinned Agent versions", { timeout: 180_000 }, async () => {
  const temporaryRoot = await mkdtemp(path.join(os.tmpdir(), "agentkiln-serving-"));
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
  const environment = { ...process.env, EXECUTION_REPO_ROOT: repositoryRoot };
  delete environment.DEEPSEEK_API_KEY;
  const postgres = Boolean(process.env.RANK_TEST_DATABASE_URL);
  const cleanupStorage = await prepareRankStorage(environment, rankdBinary);
  const executiond = launch(executiondBinary, [
    "-addr", `127.0.0.1:${executionPort}`,
    "-db", path.join(temporaryRoot, "execution.db"),
    "-worker", workerBinary,
    "-repo-root", repositoryRoot,
    "-artifacts", path.join(temporaryRoot, "artifacts"),
    "-sandboxes", path.join(temporaryRoot, "sandboxes"),
    "-credentials", path.join(temporaryRoot, "credentials"),
  ], { cwd: repositoryRoot, env: environment });
  const proxy = await startServingFaultProxy(executionURL);
  const startRank = () => launch(rankdBinary, [
    "-addr", `127.0.0.1:${port}`,
    "-db", path.join(temporaryRoot, "rank.db"),
    "-execution-url", proxy.url,
    "-dispatch-retry-interval", "100ms",
    "-webhook-poll-interval", "1h",
    "-webhook-secret-dir", path.join(temporaryRoot, "webhook-secrets"),
  ], { cwd: repositoryRoot, env: environment });
  let rankd = startRank();
  try {
    await waitFor(executionURL, "/v1/health", executiond);
    const health = await waitFor(baseURL, "/api/health", rankd);
    assert.equal(health.storage, postgres ? "postgres" : "sqlite");
    const deployment = await request(baseURL, "/api/deployments", {
      method: "POST",
      body: JSON.stringify({
        tenantId: "tenant-serving-e2e",
        agentFamilyId: "agent-research-demo",
        name: "research-public",
        policy: { requestsPerMinute: 60, maxConcurrentResponses: 4, executionTimeoutMs: 30_000 },
      }),
    });
    const release = await request(baseURL, `/api/deployments/${deployment.id}/aliases/production/releases`, {
      method: "POST",
      body: JSON.stringify({
        agentVersionId: "agent-research-demo-v1",
        policyResult: "manual_override",
        actor: "serving-e2e",
        reason: "initial production release",
      }),
    });
    const credential = await request(baseURL, "/api/serving-credentials", {
      method: "POST",
      body: JSON.stringify({
        tenantId: "tenant-serving-e2e",
        name: "serving-e2e",
        deploymentIds: [deployment.id],
        scopes: ["responses:create", "responses:read", "responses:cancel", "responses:approve"],
      }),
    });
    const webhookEndpoint = await request(baseURL, "/api/webhook-endpoints", {
      method: "POST",
      body: JSON.stringify({
        tenantId: "tenant-serving-e2e",
        name: "serving-e2e",
        url: "https://events.example.com/rank",
        events: ["response.completed"],
        deploymentIds: [deployment.id],
      }),
    });
    const authorization = { authorization: `Bearer ${credential.apiKey}` };
    const first = await request(baseURL, "/v1/responses", {
      method: "POST",
      headers: { ...authorization, "idempotency-key": "serving-e2e-sync" },
      body: JSON.stringify({ model: "research-public@production", input: "summarize the release", instructions: "Use three sentences.", metadata: { suite: "serving-e2e" } }),
    });
    const webhookEndpoints = await request(baseURL, "/api/webhook-endpoints?tenantId=tenant-serving-e2e");
    const webhookDeliveries = await request(baseURL, `/api/webhook-endpoints/${webhookEndpoint.id}/deliveries`);
    const replay = await request(baseURL, "/v1/responses", {
      method: "POST",
      headers: { ...authorization, "idempotency-key": "serving-e2e-sync" },
      body: JSON.stringify({ model: "research-public@production", input: "summarize the release", instructions: "Use three sentences.", metadata: { suite: "serving-e2e" } }),
    });
    assert.equal(replay.id, first.id);
    const continued = await request(baseURL, "/v1/responses", {
      method: "POST",
      headers: { ...authorization, "idempotency-key": "serving-e2e-continuation" },
      body: JSON.stringify({
        model: "research-public@production",
        input: "continue from the prior answer",
        previous_response_id: first.id,
      }),
    });
    const structured = await request(baseURL, "/v1/responses", {
      method: "POST",
      headers: { ...authorization, "idempotency-key": "serving-e2e-structured" },
      body: JSON.stringify({
        model: "research-public@production",
        input: "return status",
        text: { format: {
          type: "json_schema",
          name: "status_result",
          schema: { type: "object", properties: { status: { type: "string", const: "ok" } }, required: ["status"], additionalProperties: false },
          strict: true,
        } },
      }),
    });
    const richInput = await request(baseURL, "/v1/responses", {
      method: "POST",
      headers: { ...authorization, "idempotency-key": "serving-e2e-rich-input" },
      body: JSON.stringify({
        model: "research-public@production",
        input: [{ role: "user", content: [
          { type: "input_text", text: "inspect the image and file" },
          { type: "input_image", image_url: `data:image/png;base64,${Buffer.from("image fixture").toString("base64")}`, detail: "auto" },
          { type: "input_file", filename: "brief.txt", file_data: Buffer.from("file fixture").toString("base64") },
        ] }],
      }),
    });
    const toolCall = await request(baseURL, "/v1/responses", {
      method: "POST",
      headers: { ...authorization, "idempotency-key": "serving-e2e-tool-call" },
      body: JSON.stringify({
        model: "research-public@production",
        input: "look up the order",
        tools: [{
          type: "function", name: "lookup_order", description: "Look up an order", strict: true, require_approval: true,
          parameters: { type: "object", properties: {}, additionalProperties: false },
        }],
      }),
    });
    const approvedToolCall = await request(baseURL, `/v1/responses/${toolCall.id}/approval`, {
      method: "POST",
      headers: authorization,
      body: JSON.stringify({ approved: true, reason: "fixture approval" }),
    });
    const toolResult = await request(baseURL, "/v1/responses", {
      method: "POST",
      headers: { ...authorization, "idempotency-key": "serving-e2e-tool-result" },
      body: JSON.stringify({
        model: "research-public@production",
        previous_response_id: toolCall.id,
        input: [{ type: "function_call_output", call_id: toolCall.output[0].call_id, output: "order shipped" }],
      }),
    });
    const toolDetail = await request(baseURL, `/api/responses/${toolResult.id}?tenantId=tenant-serving-e2e`);
    const toolExecution = await request(executionURL, `/v1/executions/${toolDetail.response.executionId}`, { headers: environment.EXECUTION_SERVICE_TOKEN ? { authorization: `Bearer ${environment.EXECUTION_SERVICE_TOKEN}` } : {} });
    assert.ok(!toolExecution.spec.prompt);
    assert.equal(toolExecution.spec.messages[1].role, "assistant");
    assert.equal(toolExecution.spec.messages[1].toolCalls[0].id, toolCall.output[0].call_id);
    assert.deepEqual(toolExecution.spec.messages[2], { role: "tool", toolCallId: toolCall.output[0].call_id, content: "order shipped" });
    const stream = await fetch(baseURL + "/v1/responses", {
      method: "POST",
      headers: { "content-type": "application/json", ...authorization, "idempotency-key": "serving-e2e-stream" },
      body: JSON.stringify({ model: "research-public@v1", input: "stream the exact version", stream: true }),
    });
    assert.equal(stream.status, 200);
    const streamEvents = (await stream.text()).split(/\r?\n/).filter((line) => line.startsWith("data: ")).map((line) => JSON.parse(line.slice(6)));
    const retrieved = await request(baseURL, `/v1/responses/${first.id}`, { headers: authorization });
    const history = await request(baseURL, `/api/deployments/${deployment.id}/aliases/production/releases`);
    const operatorPage = await request(baseURL, `/api/responses?tenantId=tenant-serving-e2e&deploymentId=${deployment.id}&limit=2`);
    const operatorOlder = await request(baseURL, `/api/responses?tenantId=tenant-serving-e2e&deploymentId=${deployment.id}&limit=2&before=${operatorPage.nextCursor}`);
    const operatorDetail = await request(baseURL, `/api/responses/${first.id}?tenantId=tenant-serving-e2e`);
    const firstExecution = await request(executionURL, `/v1/executions/${operatorDetail.response.executionId}`, { headers: environment.EXECUTION_SERVICE_TOKEN ? { authorization: `Bearer ${environment.EXECUTION_SERVICE_TOKEN}` } : {} });
    await stop(rankd);
    rankd = startRank();
    await waitFor(baseURL, "/api/health", rankd);
    const restored = await request(baseURL, `/v1/responses/${first.id}`, { headers: authorization });
    assert.deepEqual(restored, first);
    const restoredDeliveries = await request(baseURL, `/api/webhook-endpoints/${webhookEndpoint.id}/deliveries`);
    assert.ok(restoredDeliveries.some(item => item.id === webhookDeliveries[0].id));
    const restartRank = async beforeStart => {
        const exited = new Promise(resolve => rankd.once("close", resolve));
        if (process.platform === "win32") rankd.kill("SIGKILL");
        else process.kill(-rankd.pid, "SIGKILL");
        await exited;
        beforeStart();
        rankd = startRank();
        await waitFor(baseURL, "/api/health", rankd);
      };
    const recovery = await verifyServingRecovery({ baseURL, request, authorization, proxy, restartRank });
    const evaluationRecovery = await verifyEvaluationRecovery({ baseURL, request, completeRun, proxy, restartRank });
    assert.deepEqual(evaluationRecovery, JSON.parse(await readFile(path.join(appRoot,"tests/snapshots/evaluation-recovery.json"),"utf8")));
    assert.deepEqual(recovery, JSON.parse(await readFile(path.join(appRoot,"tests/snapshots/serving-recovery.json"),"utf8")));
    const operatorKeys = await request(baseURL, "/api/serving-credentials?tenantId=tenant-serving-e2e");
    await request(baseURL, `/api/serving-credentials/${credential.id}/revoke?tenantId=tenant-serving-e2e`, {method:"POST",body:"{}"});
    const denied = await fetch(baseURL+"/v1/responses", {method:"POST",headers:{"content-type":"application/json",...authorization},body:JSON.stringify({model:"research-public@production",input:"revoked"})});
    const snapshot = {
      operator: {
        releaseHistory: history.items.map((v)=>({revision:v.revision,agentVersionId:v.agentVersionId,actor:v.actor,reason:v.reason})),
        pageSize: operatorPage.items.length,
        pagesDisjoint: operatorOlder.items.every((v)=>!operatorPage.items.some((item)=>item.id===v.id)),
        listExcludesTranscript: !JSON.stringify(operatorPage).includes("summarize the release"),
        detailInput: operatorDetail.input,
        detailVersionPinned: operatorDetail.response.agentVersionId===first.deployment.agent_version_id,
        detailHasEvents: operatorDetail.events.length>0,
        listHidesCredential: !JSON.stringify(operatorKeys).includes(credential.apiKey),
        revokedRequestStatus: denied.status,
      },
      response: {
        instructions: first.instructions,
        instructionsAppended: firstExecution.spec.systemPrompt.endsWith("\n\nUse three sentences."),
        object: first.object,
        status: first.status,
        model: first.model,
        outputTypes: first.output.map((item) => item.type),
        text: first.output[0]?.content[0]?.text,
        resolutionKind: first.deployment.resolution_kind,
        releasePinned: first.deployment.release_revision_id === release.id,
        agentVersionId: first.deployment.agent_version_id,
        conversationAssigned: first.conversation.id.startsWith("conv_"),
        previousResponseId: first.previous_response_id,
        usage: first.usage,
        costKnown: first.cost.known,
      },
      continuation: {
        sameConversation: continued.conversation.id === first.conversation.id,
        previousLinked: continued.previous_response_id === first.id,
        versionPinned: continued.deployment.agent_version_id === first.deployment.agent_version_id,
        resolutionKind: continued.deployment.resolution_kind,
        historyIncluded: continued.output[0]?.content[0]?.text.includes("summarize the release") && continued.output[0]?.content[0]?.text.includes(first.output[0]?.content[0]?.text),
      },
      structured: {
        format: structured.text.format.type,
        name: structured.text.format.name,
        strict: structured.text.format.strict,
        output: structured.output[0]?.content[0]?.text,
      },
      richInput: {
        status: richInput.status,
        versionPinned: richInput.deployment.agent_version_id === first.deployment.agent_version_id,
        textReachedWorker: richInput.output[0]?.content[0]?.text.includes("inspect the image and file"),
      },
      callerFunction: {
        requested: toolCall.status === "requires_action" && toolCall.output[0]?.type === "function_call",
        approvalRequired: toolCall.output[0]?.approval_required,
        approved: approvedToolCall.status === "completed" && approvedToolCall.output[0]?.approval_status === "approved",
        resultContinued: toolResult.status === "completed" && toolResult.output[0]?.content[0]?.text.includes("order shipped"),
        sameConversation: toolResult.conversation.id === toolCall.conversation.id,
      },
      webhook: {
        secretReturnedOnce: webhookEndpoint.signingSecret.startsWith("whsec_"),
        listHidesSecret: !Object.hasOwn(webhookEndpoints[0], "signingSecret") && !Object.hasOwn(webhookEndpoints[0], "secretRef"),
        deploymentScoped: webhookEndpoints[0].deploymentIds.includes(deployment.id),
        outboxDurable: webhookDeliveries.length === 1 && webhookDeliveries[0].eventType === "response.completed" && webhookDeliveries[0].status === "pending",
        deliveryHidesPayload: !Object.hasOwn(webhookDeliveries[0], "payload"),
      },
      retrievalStable: retrieved.id === first.id && retrieved.output[0]?.content[0]?.text === first.output[0]?.content[0]?.text,
      streamTypes: streamEvents.map((event) => event.type),
      streamSequences: streamEvents.map((event) => event.sequence_number),
      exactVersionHasReleaseRevision: Object.hasOwn(streamEvents[0].response.deployment, "release_revision_id"),
    };
    const expectedSnapshot = JSON.parse(await readFile(path.join(appRoot, "tests/snapshots/serving-response.json"), "utf8"));
    assert.deepEqual(snapshot, expectedSnapshot);
    await verifyReleaseLifecycle({ baseURL, request, command, completeRun, controlToken: environment.RANK_CONTROL_TOKEN });
    await verifyReleasePipeline({ baseURL, request, completeRun, controlToken: environment.RANK_CONTROL_TOKEN });
    await verifyResponseFeedback({ baseURL, request, command, completeRun });
    if (process.env.RANK_FEEDBACK_BROWSER === "1") await verifyReleaseBrowser(baseURL, "response-feedback.browser.js");
    if (process.env.RANK_RELEASE_BROWSER === "1") await verifyReleaseBrowser(baseURL);
  } finally {
    await stop(rankd).catch(() => {});
    await proxy.close();
    await stop(executiond).catch(() => {});
    await cleanupStorage();
    await rm(temporaryRoot, { recursive: true, force: true });
  }
});
