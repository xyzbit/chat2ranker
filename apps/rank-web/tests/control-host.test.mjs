import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { createServer } from "node:net";
import os from "node:os";
import path from "node:path";
import test from "node:test";

const appRoot = path.resolve(import.meta.dirname, "..");
const root = path.resolve(appRoot, "../..");

async function freePort() {
  const server = createServer();
  await new Promise((resolve, reject) => server.listen(0, "127.0.0.1", resolve).once("error", reject));
  const address = server.address();
  await new Promise((resolve) => server.close(resolve));
  return address.port;
}

function launch(command, args, options) {
  const child = spawn(command, args, {
    ...options,
    detached: process.platform !== "win32",
    stdio: ["ignore", "pipe", "pipe"],
  });
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

async function waitJSON(url, options = {}, processForError) {
  const deadline = Date.now() + 20_000;
  let last;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url, options);
      const payload = await response.json();
      if (response.ok) return payload;
      last = new Error(payload.error?.message || `HTTP ${response.status}`);
    } catch (error) {
      last = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(`${last?.message || "endpoint did not become ready"}\n${processForError?.output?.() || ""}`);
}

test("Control DSH resumes one persistent experiment session and executes Rank tools", { timeout: 60_000 }, async () => {
  const temp = await mkdtemp(path.join(os.tmpdir(), "rank-control-host-"));
  const rankPort = await freePort();
  const controlPort = await freePort();
  const rankURL = `http://127.0.0.1:${rankPort}`;
  const controlURL = `http://127.0.0.1:${controlPort}`;
  const token = "control-host-test-token";
  const rankd = launch("go", ["run", "./cmd/rankd", "-db", path.join(temp, "rank.db"), "-addr", `127.0.0.1:${rankPort}`], {
    cwd: path.join(root, "rank/backend"),
    env: { ...process.env, GOTOOLCHAIN: "local", RANK_CONTROL_TOKEN: token, RANK_ACTION_SECRET: "control-host-action-test" },
  });
  let control;
  const controlEnv = { ...process.env,
    RANK_API_URL: rankURL,
    RANK_CONTROL_PORT: String(controlPort),
    RANK_CONTROL_ADDR: "127.0.0.1",
    RANK_CONTROL_TOKEN: token,
    RANK_DSH_HOME: path.join(temp, "dsh-home"),
  };
  delete controlEnv.DEEPSEEK_API_KEY;
  try {
    await waitJSON(`${rankURL}/api/health`, {}, rankd);
    const created = await waitJSON(`${rankURL}/api/experiments`, {
      method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ title: "DSH resume" }),
    }, rankd);
    assert.match(created.controlSessionId, /^control-exp-/);

    control = launch(process.execPath, ["control-host/src/main.mjs"], { cwd: appRoot, env: controlEnv });
    const health = await waitJSON(`${controlURL}/control/v1/health`, {}, control);
    assert.equal(health.runtime, "deepseek-harness");
    assert.equal(health.mode, "deterministic-fallback");

    await waitJSON(`${controlURL}/control/v1/experiments/${created.id}/messages`, {
      method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ content: "先了解这个实验" }),
    }, control);
    const selected = await waitJSON(`${controlURL}/control/v1/experiments/${created.id}/messages`, {
      method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ content: "选择测试集 dataset-web-research-v3" }),
    }, control);
    assert.equal(selected.experiment.datasetVersionId, "dataset-web-research-v3");
    assert.ok(selected.experiment.controlEvents.some((event) => event.type === "a2ui/select_dataset"));
    const before = await waitJSON(`${controlURL}/control/v1/experiments/${created.id}/session`, {}, control);
    assert.equal(before.sessionId, created.controlSessionId);
    assert.ok(before.eventCount > 0);

    await stop(control);
    control = launch(process.execPath, ["control-host/src/main.mjs"], { cwd: appRoot, env: controlEnv });
    await waitJSON(`${controlURL}/control/v1/health`, {}, control);
    const resumed = await waitJSON(`${controlURL}/control/v1/experiments/${created.id}/session`, {}, control);
    assert.equal(resumed.sessionId, created.controlSessionId);
    assert.equal(resumed.resumed, true);
    assert.ok(resumed.eventCount >= before.eventCount);

    const continued = await waitJSON(`${controlURL}/control/v1/experiments/${created.id}/messages`, {
      method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ content: "重启后继续同一个实验" }),
    }, control);
    const transcript = continued.experiment.messages.map((message) => message.content);
    assert.ok(transcript.includes("先了解这个实验"));
    assert.ok(transcript.includes("选择测试集 dataset-web-research-v3"));
    assert.ok(transcript.includes("重启后继续同一个实验"));
  } finally {
    await stop(control).catch(() => {});
    await stop(rankd).catch(() => {});
    await rm(temp, { recursive: true, force: true });
  }
});
