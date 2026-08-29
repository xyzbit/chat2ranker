import { createServer } from "node:http";
import { randomUUID } from "node:crypto";
import { mkdir, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { boot, healProfilesModuleFallback, initProfile, loadProfile, resolveProfileDir } from "@deepseek-ai/dsh-app-boot";
import { createUserMessage } from "@deepseek-ai/dsh-llm";
import { SessionId } from "@deepseek-ai/dsh-session";

const here = dirname(fileURLToPath(import.meta.url));
const appRoot = resolve(here, "../..");
const projectRoot = resolve(process.env.RANK_REPO_ROOT || resolve(here, "../../../.."));
const require = createRequire(import.meta.url);
const rankApiURL = (process.env.RANK_API_URL || "http://127.0.0.1:8787").replace(/\/$/, "");
const executionApiURL = (process.env.RANK_EXECUTION_URL || "http://127.0.0.1:8790").replace(/\/$/, "");
const controlToken = process.env.RANK_CONTROL_TOKEN || "rank-local-control-token";
const address = process.env.RANK_CONTROL_ADDR || "127.0.0.1";
const port = Number(process.env.RANK_CONTROL_PORT || 8788);
const dshHome = resolve(process.env.RANK_DSH_HOME || resolve(projectRoot, "rank/var/dsh-control"));
let demo = true;
let controlRuntime = null;
const messageProtocol = `Return every final user-visible reply as newline-delimited JSON objects with no Markdown or code fence. Allowed objects are: {"type":"summary","text":"one concise conclusion"}, {"type":"paragraph","text":"supporting text"}, {"type":"list","ordered":false,"items":["item"]}, {"type":"facts","items":[{"label":"label","value":"value"}]}, {"type":"note","tone":"info|success|warning","text":"note"}, and {"type":"code","language":"json","code":"content"}. Start with summary, omit empty blocks, use at most six blocks, and keep Markdown syntax out of text fields. Tool calls are unchanged; apply this format only to the final reply after tool use.`;

process.env.DSH_HOME = dshHome;
process.env.DSH_TELEMETRY_DISABLED ||= "1";
process.env.DSH_PERMISSION_MODE ||= "read-only";

function dshAPI(protocol) {
  if (protocol === "openai-responses") return "openai-responses";
  if (protocol === "anthropic-messages") return "anthropic-messages";
  return "openai-completions";
}

async function resolveControlRuntime() {
  let response;
  try {
    response = await fetch(`${executionApiURL}/v1/internal/system-model-bindings/control/runtime`, { headers: { "x-rank-control-token": controlToken } });
  } catch {
    return null;
  }
  if (response.status === 404) return null;
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(payload.error?.message || `executiond ${response.status}`);
  return payload;
}

async function bootHarness(runtime) {
  await mkdir(dshHome, { recursive: true });
  demo = !runtime;
  controlRuntime = runtime;
  if (runtime) {
    const { connection } = runtime.binding;
    process.env.RANK_CONTROL_MODEL_API_KEY = runtime.credential;
    await writeFile(resolve(dshHome, "settings.yaml"), `llm-pi-ai:\n  providers:\n    rank-control:\n      displayName: Rank Control\n      apiKeyEnv: RANK_CONTROL_MODEL_API_KEY\n      api: ${dshAPI(connection.protocol)}\n      baseURL: ${JSON.stringify(connection.baseUrl)}\n      models:\n        - id: ${JSON.stringify(runtime.binding.model)}\n          name: ${JSON.stringify(runtime.binding.model)}\n`);
  }
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
    { id: "agent-default-model", config: { provider: demo ? "rank-demo" : "rank-control", model: demo ? "rank-demo" : runtime.binding.model } },
    { id: "system-prompt", config: { persona: `You are the Rank experiment control agent. Use Rank tools to list, create, and select immutable dataset and Agent versions. Use rank_add_dataset_cases for case additions and rank_create_agent_version for Agent configuration changes; both create a new version and never mutate the base. Use rank_create_model_connection and rank_update_model_connection when the user provides non-secret Provider, endpoint, model, or price information. If the user provides all required information, call the matching tool immediately. If only one or two required values are missing, ask only for those values in conversation; do not open or describe a large form. Prefer A2UI or secure UI only for browsing several candidates, reviewing many structured fields, bulk editing, or entering secrets. Never expose or ask the user to repeat internal IDs or JSON payloads. Use existing verified modelConnectionId values when configuring an Agent, but never ask for, receive, or handle API keys in conversation; secret entry stays in the secure UI. When the user asks to view experiment data, results, performance, comparisons, or previous runs, always call rank_show_experiment_results and treat the answer as the current experiment's overall comparison across all Runs and Agent versions. A multi-Agent comparison is supported: one RunGroup creates one independent child Run per selected Agent version on the same dataset and repeat policy. Never claim that multi-Agent comparison is unsupported or that several Agents share one Run. When the user asks to run or evaluate, call rank_prepare_run with any named Agent version IDs and repeat count so the UI can present that exact confirmation card. You may use browser, web, and file tools to prepare cases. Keep replies concise. Never claim that a run started unless the user explicitly used the run-confirmation action. ${messageProtocol}` } },
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

let ctx = await bootHarness(await resolveControlRuntime());
const records = new Map();
const turns = new Map();

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
    provider: demo ? "rank-demo" : "rank-control",
    model: demo ? "rank-demo" : controlRuntime.binding.model,
  };
  const handle = await (await persisted(sessionId)
    ? ctx.agents.resume({ resumeSessionId: SessionId(sessionId), agentOptions, setup })
    : ctx.agents.create({ sessionId: SessionId(sessionId), meta: { cwd: projectRoot }, agentOptions, setup }));
  const record = { handle, queue: Promise.resolve() };
  records.set(sessionId, record);
  return record;
}

async function reloadHarness() {
  const runtime = await resolveControlRuntime();
  if (!runtime) throw new Error("Control 模型尚未配置");
  records.clear();
  await ctx.fiber.dispose();
  ctx = await bootHarness(runtime);
}

function reloadWhenIdle() {
  if ([...turns.values()].some((turn) => !turn.done)) return setTimeout(reloadWhenIdle, 200);
  void reloadHarness().catch((error) => process.stderr.write(`rank-control-host reload failed: ${error.message}\n`));
}

function messageText(message) {
  return message?.content?.filter((block) => block.type === "text").map((block) => block.text).join("") || "";
}

function withoutThinking(text) {
  return text.replace(/<think>[\s\S]*?(?:<\/think>|$)/gi, "").replace(/<t(?:h(?:i(?:n(?:k(?:>)?)?)?)?)?$/i, "").trimStart();
}

function assistantText(message) {
  return message?.content?.some((block) => block.type === "tool-call") ? "" : withoutThinking(messageText(message)).trim();
}

function visibleUserText(message) {
  return messageText(message).replace(/\n\n<rank-references>[\s\S]*?<\/rank-references>\s*$/, "");
}

function transcript(events, sessionId) {
  const messages = [];
  for (const event of events) {
    if (event.type === "user/message" && event.data?.source?.kind === "user") {
      messages.push({ id: `dsh-${sessionId}-${event.seq}`, role: "user", content: visibleUserText(event.data), createdAt: new Date(event.time).toISOString() });
    }
    if (event.type === "assistant/message") {
      const content = assistantText(event.data?.message);
      if (content) messages.push({ id: `dsh-${sessionId}-${event.seq}`, role: "assistant", content, createdAt: new Date(event.time).toISOString() });
    }
  }
  return messages;
}

function finalResponse(events) {
  for (let index = events.length - 1; index >= 0; index -= 1) {
    if (events[index].type === "assistant/message") return assistantText(events[index].data?.message);
  }
  return "";
}

function terminalFailure(events) {
  const reason = events.findLast((event) => event.type === "turn/end")?.data?.reason;
  if (reason?.kind === "error") return new Error(reason.error?.message || "模型处理失败");
  return finalResponse(events) ? null : new Error("模型没有返回最终回复");
}

function failedResponse(sessionId, events, error) {
  const terminal = events.findLast((event) => event.type === "turn/end") || events.at(-1);
  const detail = String(error instanceof Error ? error.message : error).replace(/sk-[A-Za-z0-9_-]{8,}/g, "[REDACTED]").slice(0, 1_000);
  return {
    id: `control-error-${sessionId}-${terminal?.seq ?? "unknown"}`,
    role: "assistant",
    content: JSON.stringify({ type: "error", text: "这次处理没有完成，请重试。", detail }),
    createdAt: new Date(terminal?.time || Date.now()).toISOString(),
  };
}

async function reconcileTurn(experiment, session, start, error) {
  await ctx.sessions.flush(session);
  const events = session.events.slice(start);
  const messages = transcript(session.events, experiment.controlSessionId);
  if (error) messages.push(failedResponse(experiment.controlSessionId, events, error));
  const view = await rankRequest("/api/internal/control/transcript", {
    method: "POST",
    headers: { "content-type": "application/json", "x-rank-control-token": controlToken },
    body: JSON.stringify({ experimentId: experiment.id, controlSessionId: experiment.controlSessionId, messages }),
  });
  return { events, view };
}

function publish(turn, type, data = {}) {
  const event = { sequence: turn.events.length + 1, type, at: new Date().toISOString(), ...data };
  turn.events.push(event);
  const frame = `id: ${event.sequence}\nevent: ${type}\ndata: ${JSON.stringify(event)}\n\n`;
  for (const response of turn.listeners) response.write(frame);
  return event;
}

function projectHarnessEvent(turn, event) {
  const chunk = event.data?.chunk;
  if (event.type === "assistant/chunk" && chunk?.type === "reasoning-delta" && !turn.thinking) {
    turn.thinking = true;
    publish(turn, "assistant.status", { label: "正在思考…" });
  } else if (event.type === "tool/call") {
    turn.tools.set(String(event.data.callId), event.data.name);
    publish(turn, "tool.started", { name: event.data.name });
  } else if (event.type === "tool/result") {
    publish(turn, "tool.completed", { name: turn.tools.get(String(event.data?.message?.source?.callId)) || "tool" });
  }
}

async function runTurn(experimentId, content, mentions, turn) {
  const experiment = await experimentBinding(experimentId);
  const record = await ensureAgent(experiment);
  const operation = record.queue.then(async () => {
    const agent = record.handle.agent;
    const start = agent.session.events.length;
    const references = mentions.length ? `\n\n<rank-references>\n${mentions.map((item) => `${item.kind}: ${item.id} (${item.label})`).join("\n")}\n</rank-references>` : "";
    publish(turn, "turn.started");
    agent.followup(createUserMessage({ content: [{ type: "text", text: content + references }], source: { kind: "user" } }));
    let cursor = start;
    const drain = () => {
      while (cursor < agent.session.events.length) {
        const event = agent.session.events[cursor++];
        if (event.type === "assistant/chunk" && event.data?.chunk?.type === "text-delta") {
          turn.rawText += event.data.chunk.text;
          const visible = withoutThinking(turn.rawText);
          if (visible.length > turn.visibleText.length) publish(turn, "assistant.delta", { text: visible.slice(turn.visibleText.length) });
          turn.visibleText = visible;
        } else {
          projectHarnessEvent(turn, event);
        }
      }
    };
    let failure;
    try {
      await new Promise((resolveIdle, rejectIdle) => {
        const timer = setInterval(drain, 30);
        agent.whenIdle().then(() => { clearInterval(timer); drain(); resolveIdle(); }, (error) => { clearInterval(timer); drain(); rejectIdle(error); });
      });
    } catch (error) {
      failure = error;
    }
    failure ||= terminalFailure(agent.session.events.slice(start));
    const { events, view } = await reconcileTurn(experiment, agent.session, start, failure);
    if (failure) throw failure;
    return { experiment: view, response: finalResponse(events), sessionId: experiment.controlSessionId, eventCount: agent.session.events.length };
  });
  record.queue = operation.catch(() => {});
  return operation;
}

function startTurn(experimentId, content, mentions) {
  const turn = { id: randomUUID(), experimentId, events: [], listeners: new Set(), tools: new Map(), thinking: false, rawText: "", visibleText: "", done: false };
  turns.set(turn.id, turn);
  void runTurn(experimentId, content, mentions, turn).then((result) => {
    turn.result = result;
    turn.done = true;
    publish(turn, "turn.completed", { response: result.response });
  }, (error) => {
    turn.done = true;
    publish(turn, "turn.failed", { error: error instanceof Error ? error.message : String(error) });
  }).finally(() => {
    for (const response of turn.listeners) response.end();
    turn.listeners.clear();
    setTimeout(() => turns.delete(turn.id), 5 * 60_000).unref();
  });
  return turn;
}

function streamTurn(request, response, turn) {
  const after = Number(new URL(request.url, "http://rank.local").searchParams.get("after") || 0);
  response.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-store", connection: "keep-alive", "access-control-allow-origin": "*" });
  for (const event of turn.events.filter((item) => item.sequence > after)) response.write(`id: ${event.sequence}\nevent: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`);
  if (turn.done) return response.end();
  turn.listeners.add(response);
  request.on("close", () => turn.listeners.delete(response));
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
      json(response, 200, { ok: true, runtime: "deepseek-harness", mode: demo ? "setup-required" : "configured", model: controlRuntime?.binding?.model || "", persistentSessions: true });
      return;
    }
    if (request.method === "POST" && request.url === "/control/v1/reload") {
      if (request.headers["x-rank-control-token"] !== controlToken) return json(response, 401, { error: { code: "unauthorized", message: "控制令牌无效" } });
      json(response, 202, { ok: true, reloading: true });
      reloadWhenIdle();
      return;
    }
    const messageMatch = request.url?.match(/^\/control\/v1\/experiments\/([^/]+)\/messages$/);
    if (request.method === "POST" && messageMatch) {
      const input = await body(request);
      const content = String(input.content || "").trim();
      if (!content) return json(response, 400, { error: { code: "empty_message", message: "消息不能为空" } });
      const mentions = Array.isArray(input.mentions) ? input.mentions.filter((item) => ["dataset", "agent"].includes(item?.kind) && item.id).map((item) => ({ kind: item.kind, id: String(item.id), label: String(item.label || item.id) })) : [];
      const turn = startTurn(decodeURIComponent(messageMatch[1]), content, mentions);
      json(response, 202, { turnId: turn.id });
      return;
    }
    const turnMatch = request.url?.match(/^\/control\/v1\/experiments\/([^/]+)\/turns\/([^/?]+)/);
    if (request.method === "GET" && turnMatch) {
      const turn = turns.get(decodeURIComponent(turnMatch[2]));
      if (!turn || turn.experimentId !== decodeURIComponent(turnMatch[1])) return json(response, 404, { error: { code: "turn_not_found", message: "对话轮次不存在" } });
      streamTurn(request, response, turn);
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

server.listen(port, address, () => process.stderr.write(`rank-control-host listening on http://${address}:${port} (${demo ? "setup required" : controlRuntime.binding.model})\n`));

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
