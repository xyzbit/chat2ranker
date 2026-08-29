#!/usr/bin/env node

import { createHash, randomBytes } from "node:crypto";
import { createReadStream, createWriteStream, openSync } from "node:fs";
import { access, chmod, mkdir, mkdtemp, readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import { createServer, request as httpRequest } from "node:http";
import { homedir, platform as hostPlatform, arch as hostArch } from "node:os";
import { basename, dirname, extname, join, relative, resolve, sep } from "node:path";
import { pipeline } from "node:stream/promises";
import { Readable } from "node:stream";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";

const cliPath = fileURLToPath(import.meta.url);
const packageRoot = dirname(cliPath);
const argv = process.argv.slice(2);
const command = argv[0] || "start";

function option(name, fallback = "") {
  const index = argv.indexOf(`--${name}`);
  return index >= 0 ? argv[index + 1] || "" : fallback;
}

function has(name) {
  return argv.includes(`--${name}`);
}

async function packageMetadata() {
  try {
    return JSON.parse(await readFile(resolve(packageRoot, "package.json"), "utf8"));
  } catch {
    return { name: "@xyzbit/chat2ranker", version: "0.0.0-dev" };
  }
}

const metadata = await packageMetadata();
const home = resolve(option("home") || option("data-dir") || process.env.CHAT2RANKER_HOME || join(homedir(), ".chat2ranker"));
const pidPath = resolve(home, "chat2ranker.pid");
const webPort = Number(option("port", process.env.CHAT2RANKER_PORT || "4173"));
const url = `http://127.0.0.1:${webPort}`;

function run(commandName, args, options = {}) {
  return new Promise((resolveRun, rejectRun) => {
    const child = spawn(commandName, args, { stdio: "inherit", ...options });
    child.once("error", rejectRun);
    child.once("exit", (code) => code === 0 ? resolveRun() : rejectRun(new Error(`${commandName} exited with ${code}`)));
  });
}

async function exists(path) {
  try {
    await access(path);
    return true;
  } catch {
    return false;
  }
}

async function download(source, target) {
  const response = await fetch(source);
  if (!response.ok || !response.body) throw new Error(`下载运行包失败：${response.status} ${source}`);
  await pipeline(Readable.fromWeb(response.body), createWriteStream(target, { mode: 0o600 }));
}

async function sha256(path) {
  const hash = createHash("sha256");
  await pipeline(createReadStream(path), hash);
  return hash.digest("hex");
}

async function verifyChecksum(archive, checksumSource) {
  if (!checksumSource) return;
  let expected = "";
  if (/^https?:/.test(checksumSource)) {
    const response = await fetch(checksumSource);
    if (!response.ok) throw new Error(`下载校验文件失败：${response.status}`);
    expected = (await response.text()).trim().split(/\s+/)[0];
  } else if (await exists(checksumSource)) {
    expected = (await readFile(checksumSource, "utf8")).trim().split(/\s+/)[0];
  }
  if (expected && expected !== await sha256(archive)) throw new Error("运行包 SHA256 校验失败");
}

function runtimeTarget() {
  const platform = hostPlatform();
  const arch = hostArch();
  if (!["darwin", "linux"].includes(platform) || !["arm64", "x64"].includes(arch)) {
    throw new Error(`暂不支持 ${platform}-${arch}，当前支持 macOS/Linux arm64/x64`);
  }
  return { platform, arch, id: `${platform}-${arch}` };
}

async function validateRuntime(root, target) {
  const manifest = JSON.parse(await readFile(resolve(root, "manifest.json"), "utf8"));
  if (manifest.platform !== target.platform || manifest.arch !== target.arch) throw new Error(`运行包平台不匹配：需要 ${target.id}`);
  for (const file of ["bin/rankd", "bin/executiond", "bin/execution-worker", "web/index.html", "control-host/src/main.mjs", "node_modules/@deepseek-ai/dsh/lib/bin.js"]) {
    if (!await exists(resolve(root, file))) throw new Error(`运行包缺少 ${file}`);
  }
  await Promise.all(["rankd", "executiond", "execution-worker"].map((name) => chmod(resolve(root, "bin", name), 0o755)));
  return root;
}

async function ensureRuntime() {
  const target = runtimeTarget();
  const explicit = option("runtime-dir") || process.env.CHAT2RANKER_RUNTIME_DIR;
  if (explicit) return validateRuntime(resolve(explicit), target);
  const destination = resolve(home, "runtime", metadata.version, target.id);
  if (await exists(resolve(destination, "manifest.json"))) return validateRuntime(destination, target);
  const base = resolve(home, "runtime", metadata.version);
  await mkdir(base, { recursive: true });
  const temp = await mkdtemp(resolve(base, ".install-"));
  const archiveOption = option("runtime-archive") || process.env.CHAT2RANKER_RUNTIME_ARCHIVE;
  const archiveURL = option("runtime-url") || process.env.CHAT2RANKER_RUNTIME_URL || `https://github.com/xyzbit/chat2ranker/releases/download/v${metadata.version}/chat2ranker-${metadata.version}-${target.id}.tar.gz`;
  const archive = archiveOption ? resolve(archiveOption) : resolve(temp, basename(archiveURL));
  try {
    if (!archiveOption) {
      process.stdout.write(`正在下载 Chat2Ranker ${metadata.version} (${target.id})…\n`);
      await download(archiveURL, archive);
    }
    await verifyChecksum(archive, archiveOption ? `${archive}.sha256` : `${archiveURL}.sha256`);
    const extracted = resolve(temp, "content");
    await mkdir(extracted);
    await run("tar", ["-xzf", archive, "-C", extracted], { stdio: "ignore" });
    await validateRuntime(extracted, target);
    await mkdir(dirname(destination), { recursive: true });
    await rename(extracted, destination);
    return destination;
  } finally {
    await rm(temp, { recursive: true, force: true });
  }
}

function proxy(clientRequest, clientResponse, port) {
  const upstream = httpRequest({ hostname: "127.0.0.1", port, path: clientRequest.url, method: clientRequest.method, headers: { ...clientRequest.headers, host: `127.0.0.1:${port}` } }, (response) => {
    clientResponse.writeHead(response.statusCode || 502, response.headers);
    response.pipe(clientResponse);
  });
  upstream.once("error", (error) => {
    if (!clientResponse.headersSent) clientResponse.writeHead(502, { "content-type": "application/json" });
    clientResponse.end(JSON.stringify({ error: { message: error.message } }));
  });
  clientRequest.pipe(upstream);
}

const mime = { ".css": "text/css; charset=utf-8", ".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".json": "application/json; charset=utf-8", ".svg": "image/svg+xml", ".png": "image/png", ".ico": "image/x-icon" };

async function serveFile(response, webRoot, requestURL) {
  let pathname;
  try {
    pathname = decodeURIComponent(new URL(requestURL, "http://localhost").pathname);
  } catch {
    response.writeHead(400).end();
    return;
  }
  const candidate = resolve(webRoot, pathname === "/" ? "index.html" : pathname.slice(1));
  const safe = relative(webRoot, candidate);
  if (safe === ".." || safe.startsWith(`..${sep}`)) {
    response.writeHead(403).end();
    return;
  }
  const path = await stat(candidate).then((value) => value.isFile() ? candidate : resolve(webRoot, "index.html")).catch(() => resolve(webRoot, "index.html"));
  response.writeHead(200, { "content-type": mime[extname(path)] || "application/octet-stream", "cache-control": path.endsWith("index.html") ? "no-cache" : "public, max-age=31536000, immutable" });
  createReadStream(path).pipe(response);
}

function staticServer(webRoot) {
  return createServer((request, response) => {
    if (request.url?.startsWith("/api")) return proxy(request, response, 8787);
    if (request.url?.startsWith("/control")) return proxy(request, response, 8788);
    void serveFile(response, webRoot, request.url || "/").catch((error) => response.writeHead(500).end(error.message));
  });
}

async function waitFor(targetURL, label, timeout = 30_000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(targetURL);
      if (response.ok) return;
    } catch {
      // A local listener can refuse connections while its process is starting.
    }
    await new Promise((resolveWait) => setTimeout(resolveWait, 120));
  }
  throw new Error(`${label} 未能启动：${targetURL}`);
}

async function executionAPI(path, options = {}) {
  const response = await fetch(`http://127.0.0.1:8790${path}`, { ...options, headers: { "content-type": "application/json", ...options.headers } });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(payload.error?.message || `executiond ${response.status}`);
  return payload;
}

async function configureRole(role, config, catalog) {
  if (!config.provider || !config.model || !config.apiKey) throw new Error(`${role} 初始化需要 provider、model 和 api-key`);
  const template = catalog.find((item) => item.id === config.provider);
  const baseUrl = config.baseUrl || template?.baseUrl;
  if (!baseUrl) throw new Error(`未知 Provider ${config.provider}，请同时提供 --base-url`);
  const connection = await executionAPI("/v1/model-connections", { method: "POST", body: JSON.stringify({ name: `${template?.name || config.provider} · ${role}`, provider: config.provider, protocol: template?.protocol || "openai-chat-completions", baseUrl, apiKey: config.apiKey, defaultModel: config.model }) });
  const verified = await executionAPI(`/v1/model-connections/${encodeURIComponent(connection.id)}/verify`, { method: "POST", body: "{}" });
  return executionAPI(`/v1/system-model-bindings/${role}`, { method: "PUT", body: JSON.stringify({ connectionId: verified.id, model: config.model }) });
}

async function bootstrapModels() {
  const startup = {
    provider: option("provider") || process.env.RANK_CONTROL_PROVIDER || (process.env.DEEPSEEK_API_KEY ? "deepseek" : ""),
    model: option("model") || process.env.RANK_CONTROL_MODEL || "",
    apiKey: option("api-key") || process.env.RANK_CONTROL_API_KEY || process.env.DEEPSEEK_API_KEY || "",
    baseUrl: option("base-url") || process.env.RANK_CONTROL_BASE_URL || "",
    judgeProvider: option("judge-provider") || process.env.RANK_JUDGE_PROVIDER || "",
    judgeModel: option("judge-model") || process.env.RANK_JUDGE_MODEL || "",
    judgeAPIKey: option("judge-api-key") || process.env.RANK_JUDGE_API_KEY || "",
    judgeBaseUrl: option("judge-base-url") || process.env.RANK_JUDGE_BASE_URL || "",
  };
  const [catalog, bindings] = await Promise.all([executionAPI("/v1/model-catalog"), executionAPI("/v1/system-model-bindings")]);
  const current = Object.fromEntries(bindings.map((item) => [item.role, item]));
  if (current.control && !has("reconfigure")) {
    if (!current.judge) current.judge = await executionAPI("/v1/system-model-bindings/judge", { method: "PUT", body: JSON.stringify({ connectionId: current.control.connectionId, model: current.control.model }) });
    return current;
  }
  if (!startup.provider && !startup.apiKey && !startup.model) return current;
  const control = await configureRole("control", { provider: startup.provider, model: startup.model || catalog.find((item) => item.id === startup.provider)?.models[0]?.id, apiKey: startup.apiKey, baseUrl: startup.baseUrl }, catalog);
  const judge = startup.judgeProvider || startup.judgeModel || startup.judgeAPIKey
    ? await configureRole("judge", { provider: startup.judgeProvider || startup.provider, model: startup.judgeModel || control.model, apiKey: startup.judgeAPIKey || startup.apiKey, baseUrl: startup.judgeBaseUrl }, catalog)
    : await executionAPI("/v1/system-model-bindings/judge", { method: "PUT", body: JSON.stringify({ connectionId: control.connectionId, model: control.model }) });
  return { control, judge };
}

async function openBrowser(target = url) {
  const spec = hostPlatform() === "darwin" ? ["open", [target]] : hostPlatform() === "win32" ? ["cmd", ["/c", "start", "", target]] : ["xdg-open", [target]];
  const child = spawn(spec[0], spec[1], { detached: true, stdio: "ignore" });
  child.once("error", () => {});
  child.unref();
}

async function readPID() {
  try {
    return JSON.parse(await readFile(pidPath, "utf8"));
  } catch {
    return null;
  }
}

function processAlive(pid) {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

async function serve() {
  const existing = await readPID();
  if (existing && existing.pid !== process.pid && processAlive(existing.pid)) throw new Error(`Chat2Ranker 已在运行：${existing.url}`);
  await Promise.all(["data", "credentials", "artifacts", "sandboxes", "logs"].map((name) => mkdir(resolve(home, name), { recursive: true, mode: 0o700 })));
  const runtime = await ensureRuntime();
  const binary = (name) => resolve(runtime, "bin", name);
  const controlToken = process.env.RANK_CONTROL_TOKEN || `local-${randomBytes(24).toString("hex")}`;
  const actionSecret = process.env.RANK_ACTION_SECRET || `local-${randomBytes(24).toString("hex")}`;
  const sharedEnv = { ...process.env, RANK_REPO_ROOT: runtime, EXECUTION_REPO_ROOT: runtime, RANK_DSH_BIN: resolve(runtime, "node_modules/@deepseek-ai/dsh/lib/bin.js"), EXECUTION_WORKER_BIN: binary("execution-worker"), RANK_EXECUTION_URL: "http://127.0.0.1:8790", RANK_CONTROL_TOKEN: controlToken, RANK_ACTION_SECRET: actionSecret, RANK_API_URL: "http://127.0.0.1:8787", RANK_CONTROL_URL: "http://127.0.0.1:8788" };
  const children = new Set();
  let stopping = false;
  const launch = (name, executable, args, env = sharedEnv) => {
    const child = spawn(executable, args, { env, stdio: "inherit" });
    child.rankName = name;
    children.add(child);
    child.once("exit", (code, signal) => {
      children.delete(child);
      if (!stopping) void stop(1, `${name} 意外退出（${signal || code}）`);
    });
    return child;
  };
  let web;
  const stop = async (code, message = "") => {
    if (stopping) return;
    stopping = true;
    if (message) process.stderr.write(`${message}\n`);
    web?.close();
    for (const child of children) child.kill("SIGTERM");
    await Promise.all([...children].map((child) => new Promise((resolveExit) => {
      const timer = setTimeout(() => { child.kill("SIGKILL"); resolveExit(); }, 5_000);
      child.once("exit", () => { clearTimeout(timer); resolveExit(); });
    })));
    const saved = await readPID();
    if (saved?.pid === process.pid) await rm(pidPath, { force: true });
    process.exitCode = code;
  };
  launch("executiond", binary("executiond"), ["-addr", "127.0.0.1:8790", "-db", resolve(home, "data/execution.db"), "-worker", binary("execution-worker"), "-repo-root", runtime, "-artifacts", resolve(home, "artifacts"), "-sandboxes", resolve(home, "sandboxes"), "-credentials", resolve(home, "credentials")]);
  await waitFor("http://127.0.0.1:8790/v1/health", "executiond");
  const models = await bootstrapModels();
  launch("rankd", binary("rankd"), ["-addr", "127.0.0.1:8787", "-db", resolve(home, "data/rank.db"), "-execution-url", "http://127.0.0.1:8790"]);
  launch("control-dsh", process.execPath, [resolve(runtime, "control-host/src/main.mjs")], { ...sharedEnv, RANK_DSH_HOME: resolve(home, "dsh-control") });
  web = staticServer(resolve(runtime, "web"));
  await new Promise((resolveListen, rejectListen) => web.once("error", rejectListen).listen(webPort, "127.0.0.1", resolveListen));
  await Promise.all([waitFor("http://127.0.0.1:8787/api/health", "rankd"), waitFor("http://127.0.0.1:8788/control/v1/health", "Control DSH"), waitFor(url, "Web")]);
  await writeFile(pidPath, `${JSON.stringify({ pid: process.pid, url, version: metadata.version, startedAt: new Date().toISOString() })}\n`, { mode: 0o600 });
  process.stdout.write(`\nChat2Ranker 已启动：${url}\n模型：${models.control ? `${models.control.connection.provider}/${models.control.model}` : "请在浏览器完成首次配置"}\n数据：${home}\n\n`);
  if (!has("no-open")) await openBrowser();
  process.once("SIGINT", () => void stop(0));
  process.once("SIGTERM", () => void stop(0));
  await new Promise(() => {});
}

async function detachedStart() {
  const existing = await readPID();
  if (existing && processAlive(existing.pid)) {
    process.stdout.write(`Chat2Ranker 已在运行：${existing.url}\n`);
    return;
  }
  await mkdir(resolve(home, "logs"), { recursive: true, mode: 0o700 });
  const logPath = resolve(home, "logs/chat2ranker.log");
  const log = openSync(logPath, "a", 0o600);
  const childArgs = argv.slice(1).filter((value) => value !== "--detach");
  const child = spawn(process.execPath, [cliPath, "_serve", ...childArgs], { detached: true, stdio: ["ignore", log, log], env: process.env });
  child.unref();
  const deadline = Date.now() + 120_000;
  let ready = false;
  while (Date.now() < deadline && processAlive(child.pid)) {
    const record = await readPID();
    if (record?.pid === child.pid && await fetch(record.url).then((response) => response.ok).catch(() => false)) {
      ready = true;
      break;
    }
    await new Promise((resolveWait) => setTimeout(resolveWait, 150));
  }
  if (!ready) {
    const output = await readFile(logPath, "utf8").catch(() => "");
    throw new Error(`Chat2Ranker 后台实例启动失败\n${output.slice(-2000)}`);
  }
  process.stdout.write(`Chat2Ranker 已在后台启动：${url}\n日志：${logPath}\n`);
  if (!has("no-open")) await openBrowser();
}

async function stopService() {
  const record = await readPID();
  if (!record || !processAlive(record.pid)) {
    await rm(pidPath, { force: true });
    process.stdout.write("Chat2Ranker 未运行\n");
    return;
  }
  process.kill(record.pid, "SIGTERM");
  const deadline = Date.now() + 10_000;
  while (Date.now() < deadline && processAlive(record.pid)) await new Promise((resolveWait) => setTimeout(resolveWait, 100));
  if (processAlive(record.pid)) throw new Error(`停止超时，请检查 PID ${record.pid}`);
  process.stdout.write("Chat2Ranker 已停止\n");
}

async function status() {
  const record = await readPID();
  if (!record || !processAlive(record.pid)) {
    process.stdout.write("Chat2Ranker 未运行\n");
    process.exitCode = 1;
    return;
  }
  const healthy = await fetch(record.url).then((response) => response.ok).catch(() => false);
  process.stdout.write(`Chat2Ranker ${healthy ? "运行中" : "进程存在但服务未就绪"}\n地址：${record.url}\nPID：${record.pid}\n数据：${home}\n`);
}

function help() {
  process.stdout.write(`Chat2Ranker ${metadata.version}\n\n用法：\n  chat2ranker start [--detach] [--home ~/.chat2ranker]\n  chat2ranker stop\n  chat2ranker status\n  chat2ranker open\n\n首次启动会自动下载当前平台运行包并打开浏览器。\n`);
}

try {
  if (command === "start") await (has("detach") ? detachedStart() : serve());
  else if (command === "_serve") await serve();
  else if (command === "stop") await stopService();
  else if (command === "status") await status();
  else if (command === "open") await openBrowser((await readPID())?.url || url);
  else if (["help", "--help", "-h"].includes(command)) help();
  else if (["version", "--version", "-v"].includes(command)) process.stdout.write(`${metadata.version}\n`);
  else throw new Error(`未知命令：${command}`);
} catch (error) {
  process.stderr.write(`${error.message}\n`);
  process.exitCode = 1;
}
