import { createServer } from "node:http";
import { mkdir, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { boot, healProfilesModuleFallback, initProfile, loadProfile, resolveProfileDir } from "@deepseek-ai/dsh-app-boot";
import { createUserMessage } from "@deepseek-ai/dsh-llm";
import { SessionId } from "@deepseek-ai/dsh-session";

const here = dirname(fileURLToPath(import.meta.url));
const appRoot = resolve(here, "../..");
const projectRoot = resolve(here, "../../../..");
const require = createRequire(import.meta.url);
const rankApiURL = (process.env.RANK_API_URL || "http://127.0.0.1:8787").replace(/\/$/, "");
const controlToken = process.env.RANK_CONTROL_TOKEN || "rank-local-control-token";
const address = process.env.RANK_CONTROL_ADDR || "127.0.0.1";
const port = Number(process.env.RANK_CONTROL_PORT || 8788);
const dshHome = resolve(process.env.RANK_DSH_HOME || resolve(projectRoot, "rank/var/dsh-control"));
const demo = !process.env.DEEPSEEK_API_KEY;

process.env.DSH_HOME = dshHome;
process.env.DSH_TELEMETRY_DISABLED ||= "1";
process.env.DSH_PERMISSION_MODE ||= "read-only";

async function bootHarness() {
  await mkdir(dshHome, { recursive: true });
  const profileDir = resolveProfileDir("rank-control", dshHome);
  initProfile(profileDir, ["@deepseek-ai/dsh-base"]);
  const installAnchor = require.resolve("@deepseek-ai/dsh/package.json");
  healProfilesModuleFallback(installAnchor, dshHome);
  const rootConfig = resolve(profileDir, "cordis.root.yml");
  await writeFile(rootConfig, "[]\n");
  const profile = loadProfile("rank-control-host", "rank-control", installAnchor, dshHome);
  const pluginPath = resolve(appRoot, "control-host/plugins/rank-control.mjs");
  const patches = [
    ...profile.layers.flatMap((layer) => layer.patches),
    ...profile.patches,
    { id: "agent-default-model", config: { provider: demo ? "rank-demo" : "deepseek-official", model: demo ? "rank-demo" : (process.env.RANK_CONTROL_MODEL || "deepseek-v4-flash") } },
    { id: "system-prompt", config: { persona: "You are the Rank experiment control agent. Maintain the dataset and Agent configuration through Rank tools. Never claim that a run started unless the user explicitly used the run-confirmation action." } },
    { id: "sandbox-policy", config: { mode: "read-only", workspaceRoot: projectRoot } },
    { id: "skill-filesystem", config: { customSkillDirs: [resolve(projectRoot, "rank/assets/skills")] } },
    { insert: [{
      id: "rank-control",
      name: pluginPath,
      config: { rankApiURL, controlToken, demo },
    }] },
  ];
  return boot("rank-control-host", rootConfig, structuredClone(patches));
}

const ctx = await bootHarness();
const records = new Map();

async function rankRequest(path, options = {}) {
  const response = await fetch(rankApiURL + path, options);
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(payload.error?.message || `rankd ${response.status}`);
  return payload;
}

async function experimentBinding(experimentId) {
  let experiment = await rankRequest(`/api/experiments/${encodeURIComponent(experimentId)}`);
  if (!experiment.controlSessionId) {
    experiment = await rankRequest("/api/internal/control/sessions/bind", {
      method: "POST",
      headers: { "content-type": "application/json", "x-rank-control-token": controlToken },
      body: JSON.stringify({ experimentId, controlSessionId: `control-${experimentId}` }),
    });
  }
  return experiment;
}

async function persisted(sessionId) {
  const headers = await ctx.sessionPersistence.list();
  return headers.some((header) => String(header.id) === sessionId);
}

async function ensureAgent(experiment) {
  const sessionId = experiment.controlSessionId;
  const live = records.get(sessionId);
  if (live) return live;
  const setup = (agentCtx) => ctx.rankControl.setupAgent(agentCtx, {
    experimentId: experiment.id,
    controlSessionId: sessionId,
  });
  const agentOptions = {
    provider: demo ? "rank-demo" : "deepseek-official",
    model: demo ? "rank-demo" : (process.env.RANK_CONTROL_MODEL || "deepseek-v4-flash"),
  };
  const handle = await (await persisted(sessionId)
    ? ctx.agents.resume({ resumeSessionId: SessionId(sessionId), agentOptions, setup })
    : ctx.agents.create({ sessionId: SessionId(sessionId), meta: { cwd: projectRoot }, agentOptions, setup }));
  const record = { handle, queue: Promise.resolve() };
  records.set(sessionId, record);
  return record;
}

function messageText(message) {
  return message?.content?.filter((block) => block.type === "text").map((block) => block.text).join("") || "";
}

function transcript(events, sessionId) {
  const messages = [];
  for (const event of events) {
    if (event.type === "user/message" && event.data?.source?.kind === "user") {
      messages.push({ id: `dsh-${sessionId}-${event.seq}`, role: "user", content: messageText(event.data), createdAt: new Date(event.time).toISOString() });
    }
    if (event.type === "assistant/message") {
      const content = messageText(event.data?.message);
      if (content) messages.push({ id: `dsh-${sessionId}-${event.seq}`, role: "assistant", content, createdAt: new Date(event.time).toISOString() });
    }
  }
  return messages;
}

function finalResponse(events) {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    if (events[index].type === "assistant/message") return messageText(events[index].data?.message);
  }
  return "";
}

async function runTurn(experimentId, content) {
  const experiment = await experimentBinding(experimentId);
  const record = await ensureAgent(experiment);
  const operation = record.queue.then(async () => {
    const agent = record.handle.agent;
    const start = agent.session.events.length;
    agent.followup(createUserMessage({ content: [{ type: "text", text: content }], source: { kind: "user" } }));
    await agent.whenIdle();
    await ctx.sessions.flush(agent.session);
    const events = agent.session.events.slice(start);
    // Reconcile the complete durable DSH transcript on every turn. Rank's
    // repository inserts these deterministic event-derived ids idempotently,
    // so a prior callback failure is repaired without making either store
    // depend on the other's transaction.
    const messages = transcript(agent.session.events, experiment.controlSessionId);
    const view = await rankRequest("/api/internal/control/transcript", {
      method: "POST",
      headers: { "content-type": "application/json", "x-rank-control-token": controlToken },
      body: JSON.stringify({ experimentId, controlSessionId: experiment.controlSessionId, messages }),
    });
    return { experiment: view, response: finalResponse(events), sessionId: experiment.controlSessionId, eventCount: agent.session.events.length };
  });
  record.queue = operation.catch(() => {});
  return operation;
}

function json(response, status, value) {
  response.writeHead(status, { "content-type": "application/json; charset=utf-8", "cache-control": "no-store", "access-control-allow-origin": "*" });
  response.end(JSON.stringify(value));
}

async function body(request) {
  const chunks = [];
  let size = 0;
  for await (const chunk of request) {
    size += chunk.length;
    if (size > 2 * 1024 * 1024) throw new Error("request body too large");
    chunks.push(chunk);
  }
  return chunks.length ? JSON.parse(Buffer.concat(chunks).toString("utf8")) : {};
}

const server = createServer(async (request, response) => {
  try {
    if (request.method === "OPTIONS") {
      response.writeHead(204, { "access-control-allow-origin": "*", "access-control-allow-headers": "content-type,idempotency-key", "access-control-allow-methods": "GET,POST,OPTIONS" });
      response.end();
      return;
    }
    if (request.method === "GET" && request.url === "/control/v1/health") {
      json(response, 200, { ok: true, runtime: "deepseek-harness", mode: demo ? "deterministic-fallback" : "deepseek", persistentSessions: true });
      return;
    }
    const messageMatch = request.url?.match(/^\/control\/v1\/experiments\/([^/]+)\/messages$/);
    if (request.method === "POST" && messageMatch) {
      const input = await body(request);
      const content = String(input.content || "").trim();
      if (!content) return json(response, 400, { error: { code: "empty_message", message: "消息不能为空" } });
      const result = await runTurn(decodeURIComponent(messageMatch[1]), content);
      json(response, 201, result);
      return;
    }
    const sessionMatch = request.url?.match(/^\/control\/v1\/experiments\/([^/]+)\/session$/);
    if (request.method === "GET" && sessionMatch) {
      const experiment = await experimentBinding(decodeURIComponent(sessionMatch[1]));
      const record = await ensureAgent(experiment);
      json(response, 200, { sessionId: experiment.controlSessionId, eventCount: record.handle.agent.session.events.length, resumed: await persisted(experiment.controlSessionId) });
      return;
    }
    json(response, 404, { error: { code: "not_found", message: "Control Host 路由不存在" } });
  } catch (error) {
    json(response, 500, { error: { code: "control_host_error", message: error instanceof Error ? error.message : String(error) } });
  }
});

server.listen(port, address, () => process.stderr.write(`rank-control-host listening on http://${address}:${port} (${demo ? "deterministic fallback" : "deepseek"})\n`));

let stopping = false;
async function stop(code) {
  if (stopping) return;
  stopping = true;
  await new Promise((resolveClose) => server.close(resolveClose));
  await ctx.fiber.dispose();
  process.exit(code);
}
process.on("SIGTERM", () => void stop(0));
process.on("SIGINT", () => void stop(130));
