import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
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

async function sendTurn(controlURL, rankURL, experimentId, content, mentions = []) {
  const started = await waitJSON(`${controlURL}/control/v1/experiments/${experimentId}/messages`, {
    method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ content, mentions }),
  });
  const response = await fetch(`${controlURL}/control/v1/experiments/${experimentId}/turns/${started.turnId}`);
  const events = (await response.text()).split(/\r?\n/).filter((line) => line.startsWith("data: ")).map((line) => JSON.parse(line.slice(6)));
  assert.equal(events.at(-1)?.type, "turn.completed", JSON.stringify(events.at(-1)));
  return { experiment: await waitJSON(`${rankURL}/api/experiments/${experimentId}`), events };
}

test("Control DSH resumes one persistent experiment session and executes Rank tools", { timeout: 60_000 }, async () => {
  const temp = await mkdtemp(path.join(os.tmpdir(), "rank-control-host-"));
  const rankPort = await freePort();
  const controlPort = await freePort();
  const executionPort = await freePort();
  const rankURL = `http://127.0.0.1:${rankPort}`;
  const controlURL = `http://127.0.0.1:${controlPort}`;
  const executionURL = `http://127.0.0.1:${executionPort}`;
  const token = "control-host-test-token";
  const executiond = launch("go", ["run", "./cmd/executiond", "-db", path.join(temp, "execution.db"), "-addr", `127.0.0.1:${executionPort}`, "-repo-root", root, "-artifacts", path.join(temp, "artifacts"), "-sandboxes", path.join(temp, "sandboxes"), "-credentials", path.join(temp, "credentials")], {
    cwd: path.join(root, "execution/backend"),
    env: { ...process.env, GOTOOLCHAIN: "local", EXECUTION_REPO_ROOT: root, RANK_CONTROL_TOKEN: token },
  });
  const rankd = launch("go", ["run", "./cmd/rankd", "-db", path.join(temp, "rank.db"), "-addr", `127.0.0.1:${rankPort}`], {
    cwd: path.join(root, "rank/backend"),
    env: { ...process.env, GOTOOLCHAIN: "local", RANK_CONTROL_TOKEN: token, RANK_ACTION_SECRET: "control-host-action-test", RANK_EXECUTION_URL: executionURL },
  });
  let control;
  const controlEnv = { ...process.env,
    RANK_API_URL: rankURL,
    RANK_CONTROL_PORT: String(controlPort),
    RANK_CONTROL_ADDR: "127.0.0.1",
    RANK_CONTROL_TOKEN: token,
    RANK_EXECUTION_URL: executionURL,
    RANK_DSH_HOME: path.join(temp, "dsh-home"),
  };
  delete controlEnv.DEEPSEEK_API_KEY;
  try {
    await waitJSON(`${executionURL}/v1/health`, {}, executiond);
    await waitJSON(`${rankURL}/api/health`, {}, rankd);
    const created = await waitJSON(`${rankURL}/api/experiments`, {
      method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ title: "DSH resume" }),
    }, rankd);
    assert.match(created.controlSessionId, /^control-exp-/);

    control = launch(process.execPath, ["control-host/src/main.mjs"], { cwd: appRoot, env: controlEnv });
    const health = await waitJSON(`${controlURL}/control/v1/health`, {}, control);
    assert.equal(health.runtime, "deepseek-harness");
    assert.equal(health.mode, "setup-required");

    const introduced = await sendTurn(controlURL, rankURL, created.id, "先了解这个实验");
    const expectedMessage = await readFile(path.join(appRoot, "tests/snapshots/control-message.jsonl"), "utf8");
    assert.equal(introduced.events.at(-1).response, expectedMessage.trim());
	const listed = await sendTurn(controlURL, rankURL, created.id, "列出有哪些测试集和 Agent");
	assert.ok(listed.events.some((event) => event.type === "tool.completed" && event.name === "rank_list_assets"));
    const selected = await sendTurn(controlURL, rankURL, created.id, "使用这个测试集", [{ kind: "dataset", id: "dataset-web-research-v3", label: "Web 研究基准集 v3" }]);
    assert.equal(selected.experiment.datasetVersionId, "dataset-web-research-v3");
    assert.ok(selected.experiment.controlEvents.some((event) => event.type === "a2ui/select_dataset"));
    assert.ok(selected.events.some((event) => event.type === "tool.started" && event.name === "rank_select_dataset"));
    assert.ok(selected.events.some((event) => event.type === "tool.completed" && event.name === "rank_select_dataset"));
    const createdDataset = await sendTurn(controlURL, rankURL, created.id, "创建测试集 {\"name\":\"对话采集集\",\"source\":\"conversation\",\"cases\":[{\"id\":\"chat-1\",\"title\":\"对话用例\",\"input\":\"完成一次检索\",\"expected\":{\"demoOutcome\":\"pass\"}}]}");
    assert.equal(createdDataset.experiment.dataset.name, "对话采集集");
    assert.ok(createdDataset.experiment.controlEvents.some((event) => event.type === "control/create_dataset"));
    const added = await sendTurn(controlURL, rankURL, created.id, `追加用例 {"baseDatasetVersionId":"${createdDataset.experiment.datasetVersionId}","cases":[{"id":"chat-2","title":"第二条","input":"输出第二条","expected":{"demoOutcome":"pass"}}]}`);
    assert.equal(added.experiment.dataset.version, 2);
    assert.equal(added.experiment.dataset.caseCount, 2);
    assert.equal(createdDataset.experiment.dataset.caseCount, 1);
    const createdAgent = await sendTurn(controlURL, rankURL, created.id, "创建 Agent {\"name\":\"对话 Demo\",\"runnerType\":\"mock\",\"tools\":[\"web_search\"]}");
    assert.equal(createdAgent.experiment.agent.name, "对话 Demo");
    assert.ok(createdAgent.experiment.controlEvents.some((event) => event.type === "control/create_agent"));
    const changedAgent = await sendTurn(controlURL, rankURL, created.id, `基于当前 Agent 创建新版本 {"baseAgentVersionId":"${createdAgent.experiment.agentVersionId}","tools":["web_search","browser"],"skills":["web-research"]}`);
    assert.equal(changedAgent.experiment.agent.version, 2);
    assert.deepEqual(changedAgent.experiment.agent.tools, ["web_search", "browser"]);
    assert.deepEqual(changedAgent.experiment.agent.skills, ["web-research"]);
    assert.ok(changedAgent.events.some((event) => event.type === "tool.started" && event.name === "rank_create_agent_version"));
	const connectionTurn = await sendTurn(controlURL, rankURL, created.id, "创建模型连接 {\"name\":\"对话 DeepSeek\",\"provider\":\"deepseek\",\"defaultModel\":\"deepseek-chat\"}");
	assert.ok(connectionTurn.events.some((event) => event.type === "tool.started" && event.name === "rank_create_model_connection"));
	const connectionBootstrap = await waitJSON(`${rankURL}/api/bootstrap`);
	const conversationConnection = connectionBootstrap.modelConnections.find((connection) => connection.name === "对话 DeepSeek");
	assert.equal(conversationConnection.baseUrl, "https://api.deepseek.com");
	assert.equal(conversationConnection.status, "missing_credential");
	const prepared = await sendTurn(controlURL, rankURL, created.id, `准备运行 {"agentVersionIds":["${createdAgent.experiment.agentVersionId}","${changedAgent.experiment.agentVersionId}"],"trialCount":1}`);
	const preparedEvent = prepared.experiment.controlEvents.findLast((event) => event.type === "a2ui/prepare_run");
	assert.deepEqual(preparedEvent.payload.agentVersionIds, [createdAgent.experiment.agentVersionId, changedAgent.experiment.agentVersionId]);
	assert.ok(prepared.events.some((event) => event.type === "tool.started" && event.name === "rank_prepare_run"));
    const results = await sendTurn(controlURL, rankURL, created.id, "查看当前实验数据");
    assert.ok(results.events.some((event) => event.type === "tool.started" && event.name === "rank_show_experiment_results"));
    assert.ok(results.experiment.controlEvents.some((event) => event.type === "a2ui/show_experiment_results"));
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

    const continued = await sendTurn(controlURL, rankURL, created.id, "重启后继续同一个实验");
    const transcript = continued.experiment.messages.map((message) => message.content);
    assert.ok(transcript.includes("先了解这个实验"));
    assert.ok(transcript.includes("使用这个测试集"));
    assert.ok(transcript.includes("重启后继续同一个实验"));
  } finally {
    await stop(control).catch(() => {});
    await stop(rankd).catch(() => {});
    await stop(executiond).catch(() => {});
    await rm(temp, { recursive: true, force: true });
  }
});
