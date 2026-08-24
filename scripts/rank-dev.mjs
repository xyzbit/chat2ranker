import { randomBytes } from "node:crypto";
import { access, mkdir } from "node:fs/promises";
import { spawn } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const variableRoot = resolve(root, "rank/var");
const binaryRoot = resolve(variableRoot, "bin");
const rankdBinary = resolve(binaryRoot, "rankd");
const executiondBinary = resolve(binaryRoot, "executiond");
const workerBinary = resolve(binaryRoot, "execution-worker");
const controlToken = process.env.RANK_CONTROL_TOKEN || `local-${randomBytes(24).toString("hex")}`;
const actionSecret = process.env.RANK_ACTION_SECRET || `local-${randomBytes(24).toString("hex")}`;
const children = new Set();

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
launch("rankd", rankdBinary, [
  "-addr", "127.0.0.1:8787",
  "-db", resolve(variableRoot, "rank.db"),
  "-execution-url", "http://127.0.0.1:8790",
], { env: sharedEnv });
launch("control-dsh", process.execPath, ["apps/rank-web/control-host/src/main.mjs"], { env: { ...sharedEnv, RANK_DSH_HOME: resolve(variableRoot, "dsh-control") } });
launch("rank-web", "pnpm", ["--filter", "@xyzbit/chat2ranker-web", "dev", "--host", "127.0.0.1", "--port", "4173"], { env: sharedEnv });

process.stdout.write("\nRank is starting at http://127.0.0.1:4173\n");
process.stdout.write(`Mode: ${process.env.DEEPSEEK_API_KEY ? "real DeepSeek" : "keyless deterministic Control + isolated Demo Runner"}\n\n`);
process.on("SIGINT", () => void stop(130));
process.on("SIGTERM", () => void stop(0));
await new Promise(() => {});
