import { randomBytes } from "node:crypto";
import { access, mkdir } from "node:fs/promises";
import { spawn } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");

function option(name) {
  const index = process.argv.indexOf(`--${name}`);
  return index >= 0 ? process.argv[index + 1] : "";
}

const variableRoot = resolve(option("home") || option("data-dir") || process.env.CHAT2RANKER_HOME || resolve(root, "rank/var"));
const binaryRoot = resolve(variableRoot, "bin");
const rankdBinary = resolve(binaryRoot, "rankd");
const executiondBinary = resolve(binaryRoot, "executiond");
const workerBinary = resolve(binaryRoot, "execution-worker");
const controlToken = process.env.RANK_CONTROL_TOKEN || `local-${randomBytes(24).toString("hex")}`;
const actionSecret = process.env.RANK_ACTION_SECRET || `local-${randomBytes(24).toString("hex")}`;
const children = new Set();

const startup = {
  provider: option("provider") || process.env.RANK_CONTROL_PROVIDER || (process.env.DEEPSEEK_API_KEY ? "deepseek" : ""),
  model: option("model") || process.env.RANK_CONTROL_MODEL || "",
  apiKey: option("api-key") || process.env.RANK_CONTROL_API_KEY || process.env.DEEPSEEK_API_KEY || "",
  baseURL: option("base-url") || process.env.RANK_CONTROL_BASE_URL || "",
  judgeProvider: option("judge-provider") || process.env.RANK_JUDGE_PROVIDER || "",
  judgeModel: option("judge-model") || process.env.RANK_JUDGE_MODEL || "",
  judgeAPIKey: option("judge-api-key") || process.env.RANK_JUDGE_API_KEY || "",
  judgeBaseURL: option("judge-base-url") || process.env.RANK_JUDGE_BASE_URL || "",
  reconfigure: process.argv.includes("--reconfigure"),
};

async function run(command, args, options = {}) {
  await new Promise((resolveRun, rejectRun) => {
    const child = spawn(command, args, { cwd: root, stdio: "inherit", ...options });
    child.once("error", rejectRun);
    child.once("exit", (code) => code === 0 ? resolveRun() : rejectRun(new Error(`${command} exited with ${code}`)));
  });
}

function launch(name, command, args, options = {}) {
  const child = spawn(command, args, { cwd: root, stdio: "inherit", ...options });
  child.rankName = name;
  children.add(child);
  child.once("exit", (code, signal) => {
    children.delete(child);
    if (!stopping) {
      process.stderr.write(`${name} stopped unexpectedly (${signal || code})\n`);
      void stop(1);
    }
  });
  return child;
}

async function waitFor(url, name) {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      if (response.ok) return;
    } catch {
      // The service can refuse connections while its listener is starting.
    }
    await new Promise((resolveWait) => setTimeout(resolveWait, 100));
  }
  throw new Error(`${name} did not become ready at ${url}`);
}

async function api(path, options = {}) {
  const response = await fetch(`http://127.0.0.1:8790${path}`, { ...options, headers: { "content-type": "application/json", ...options.headers } });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(payload.error?.message || `executiond ${response.status}`);
  return payload;
}

async function configureRole(role, config, catalog, existing) {
  if (!config.provider || !config.model || !config.apiKey) throw new Error(`${role} 初始化需要 provider、model 和 api-key`);
  const template = catalog.find((item) => item.id === config.provider);
  const baseURL = config.baseURL || template?.baseUrl;
  if (!baseURL) throw new Error(`未知 Provider ${config.provider}，请同时提供 --base-url`);
  const connection = await api("/v1/model-connections", { method: "POST", body: JSON.stringify({ name: `${template?.name || config.provider} · ${role}`, provider: config.provider, protocol: template?.protocol || "openai-chat-completions", baseUrl: baseURL, apiKey: config.apiKey, defaultModel: config.model }) });
  const verified = await api(`/v1/model-connections/${encodeURIComponent(connection.id)}/verify`, { method: "POST", body: "{}" });
  return api(`/v1/system-model-bindings/${role}`, { method: "PUT", body: JSON.stringify({ connectionId: verified.id, model: config.model }) });
}

async function bootstrapSystemModels() {
  const [catalog, bindings] = await Promise.all([api("/v1/model-catalog"), api("/v1/system-model-bindings")]);
  const current = Object.fromEntries(bindings.map((item) => [item.role, item]));
  if (current.control && !startup.reconfigure) {
    if (!current.judge) current.judge = await api("/v1/system-model-bindings/judge", { method: "PUT", body: JSON.stringify({ connectionId: current.control.connectionId, model: current.control.model }) });
    return current;
  }
  if (!startup.provider && !startup.apiKey && !startup.model) return current;
  const control = await configureRole("control", { provider: startup.provider, model: startup.model || catalog.find((item) => item.id === startup.provider)?.models[0]?.id, apiKey: startup.apiKey, baseURL: startup.baseURL }, catalog, current.control);
  const judge = startup.judgeProvider || startup.judgeModel || startup.judgeAPIKey
    ? await configureRole("judge", { provider: startup.judgeProvider || startup.provider, model: startup.judgeModel || control.model, apiKey: startup.judgeAPIKey || startup.apiKey, baseURL: startup.judgeBaseURL }, catalog, current.judge)
    : await api("/v1/system-model-bindings/judge", { method: "PUT", body: JSON.stringify({ connectionId: control.connectionId, model: control.model }) });
  return { control, judge };
}

let stopping = false;
async function stop(code) {
  if (stopping) return;
  stopping = true;
  for (const child of children) child.kill("SIGTERM");
  await Promise.all([...children].map((child) => new Promise((resolveExit) => {
    const timer = setTimeout(() => { child.kill("SIGKILL"); resolveExit(); }, 5_000);
    child.once("exit", () => { clearTimeout(timer); resolveExit(); });
  })));
  process.exit(code);
}

await mkdir(binaryRoot, { recursive: true });
try {
  await Promise.all([
    access(resolve(root, "apps/cli/lib/bin.js")),
    access(resolve(root, "packages/typert/registry/lib/index.js")),
    access(resolve(root, "packages/api/gateway/lib/index.js")),
  ]);
} catch {
  await run("pnpm", ["run", "build:lib"]);
}
await Promise.all([
  run("go", ["build", "-o", rankdBinary, "./cmd/rankd"], { cwd: resolve(root, "rank/backend") }),
  run("go", ["build", "-o", executiondBinary, "./cmd/executiond"], { cwd: resolve(root, "execution/backend") }),
  run("go", ["build", "-o", workerBinary, "./cmd/execution-worker"], { cwd: resolve(root, "execution/backend") }),
]);

const sharedEnv = {
  ...process.env,
  RANK_REPO_ROOT: root,
  EXECUTION_WORKER_BIN: workerBinary,
  EXECUTION_REPO_ROOT: root,
  RANK_EXECUTION_URL: "http://127.0.0.1:8790",
  RANK_CONTROL_TOKEN: controlToken,
  RANK_ACTION_SECRET: actionSecret,
  RANK_API_URL: "http://127.0.0.1:8787",
  RANK_CONTROL_URL: "http://127.0.0.1:8788",
};

launch("executiond", executiondBinary, [
  "-addr", "127.0.0.1:8790",
  "-db", resolve(variableRoot, "execution.db"),
  "-worker", workerBinary,
  "-repo-root", root,
  "-artifacts", resolve(variableRoot, "artifacts"),
  "-sandboxes", resolve(variableRoot, "sandboxes"),
], { env: sharedEnv });
await waitFor("http://127.0.0.1:8790/v1/health", "executiond");
const systemModels = await bootstrapSystemModels();
launch("rankd", rankdBinary, [
  "-addr", "127.0.0.1:8787",
  "-db", resolve(variableRoot, "rank.db"),
  "-execution-url", "http://127.0.0.1:8790",
], { env: sharedEnv });
launch("control-dsh", process.execPath, ["apps/rank-web/control-host/src/main.mjs"], { env: { ...sharedEnv, RANK_DSH_HOME: resolve(variableRoot, "dsh-control") } });
launch("rank-web", "pnpm", ["--filter", "@xyzbit/chat2ranker-web", "dev", "--host", "127.0.0.1", "--port", "4173"], { env: sharedEnv });

process.stdout.write("\nRank is starting at http://127.0.0.1:4173\n");
process.stdout.write(`Models: ${systemModels.control ? `${systemModels.control.connection.provider}/${systemModels.control.model}` : "browser setup required"}\n\n`);
process.on("SIGINT", () => void stop(130));
process.on("SIGTERM", () => void stop(0));
await new Promise(() => {});
