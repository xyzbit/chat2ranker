import { spawn } from "node:child_process";
import { mkdir } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const binaryRoot = resolve(root, "rank/var/bin");

async function run(command, args, cwd = root) {
  await new Promise((resolveRun, rejectRun) => {
    const child = spawn(command, args, { cwd, stdio: "inherit" });
    child.once("error", rejectRun);
    child.once("exit", (code) => code === 0 ? resolveRun() : rejectRun(new Error(`${command} exited with ${code}`)));
  });
}

await mkdir(binaryRoot, { recursive: true });
await run("pnpm", ["run", "build:lib"]);
await Promise.all([
  run("go", ["build", "-o", resolve(binaryRoot, "executiond"), "./cmd/executiond"], resolve(root, "execution/backend")),
  run("go", ["build", "-o", resolve(binaryRoot, "execution-worker"), "./cmd/execution-worker"], resolve(root, "execution/backend")),
  run("go", ["build", "-o", resolve(binaryRoot, "rankd"), "./cmd/rankd"], resolve(root, "rank/backend")),
]);
await run("pnpm", ["--filter", "@xyzbit/chat2ranker-web", "build"]);
