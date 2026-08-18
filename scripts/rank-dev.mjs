import { randomBytes } from "node:crypto";
import { access, mkdir } from "node:fs/promises";
import { spawn } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const variableRoot = resolve(root, "rank/var");
const binaryRoot = resolve(variableRoot, "bin");
const rankdBinary = resolve(binaryRoot, "rankd");
const workerBinary = resolve(binaryRoot, "rank-worker");
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
await run("go", ["build", "-o", rankdBinary, "./cmd/rankd"], { cwd: resolve(root, "rank/backend") });
await run("go", ["build", "-o", workerBinary, "./cmd/rank-worker"], { cwd: resolve(root, "rank/backend") });

const sharedEnv = {
  ...process.env,
  RANK_REPO_ROOT: root,
  RANK_WORKER_BIN: workerBinary,
  RANK_CONTROL_TOKEN: controlToken,
  RANK_ACTION_SECRET: actionSecret,
  RANK_API_URL: "http://127.0.0.1:8787",
  RANK_CONTROL_URL: "http://127.0.0.1:8788",
};

launch("rankd", rankdBinary, [
  "-addr", "127.0.0.1:8787",
  "-db", resolve(variableRoot, "rank.db"),
  "-worker", workerBinary,
  "-repo-root", root,
  "-artifacts", resolve(variableRoot, "artifacts"),
  "-sandboxes", resolve(variableRoot, "sandboxes"),
], { env: sharedEnv });
launch("control-dsh", process.execPath, ["apps/rank-web/control-host/src/main.mjs"], { env: { ...sharedEnv, RANK_DSH_HOME: resolve(variableRoot, "dsh-control") } });
launch("rank-web", "pnpm", ["--filter", "@xyzbit/chat2ranker-web", "dev", "--host", "127.0.0.1", "--port", "4173"], { env: sharedEnv });

process.stdout.write("\nRank is starting at http://127.0.0.1:4173\n");
process.stdout.write(`Mode: ${process.env.DEEPSEEK_API_KEY ? "real DeepSeek" : "keyless deterministic Control + isolated Demo Runner"}\n\n`);
process.on("SIGINT", () => void stop(130));
process.on("SIGTERM", () => void stop(0));
await new Promise(() => {});
