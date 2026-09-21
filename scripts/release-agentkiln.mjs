import { createHash } from "node:crypto";
import { chmod, cp, mkdir, mkdtemp, readFile, readdir, realpath, rm, writeFile } from "node:fs/promises";
import { spawn } from "node:child_process";
import { arch as hostArch, platform as hostPlatform, tmpdir } from "node:os";
import { basename, dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const args = process.argv.slice(2);
const option = (name, fallback = "") => {
  const index = args.indexOf(`--${name}`);
  return index >= 0 ? args[index + 1] || "" : fallback;
};
const platform = option("platform", hostPlatform());
const arch = option("arch", hostArch());
if (!["darwin", "linux"].includes(platform) || !["arm64", "x64"].includes(arch)) throw new Error(`暂不支持 ${platform}-${arch}`);
if (platform !== hostPlatform() || arch !== hostArch()) throw new Error("DSH 包含原生依赖，请在目标系统和架构上构建 runtime");
const product = JSON.parse(await readFile(resolve(root, "apps/rank-web/package.json"), "utf8"));
const version = option("version", product.version);
const target = `${platform}-${arch}`;
const output = resolve(root, "dist/agentkiln");
const runtime = resolve(output, `runtime-${target}`);
const npmStage = resolve(output, "npm-package");
const archiveName = `agentkiln-${version}-${target}.tar.gz`;
const archive = resolve(output, archiveName);


function run(command, commandArgs, options = {}) {
  return new Promise((resolveRun, rejectRun) => {
    const child = spawn(command, commandArgs, { cwd: root, stdio: "inherit", ...options });
    child.once("error", rejectRun);
    child.once("exit", (code) => code === 0 ? resolveRun() : rejectRun(new Error(`${command} exited with ${code}`)));
  });
}

// pnpm's vendored overrides link outside the deployment; ship real files instead.
async function materializeLinks(directory) {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name);
    if (entry.isSymbolicLink()) {
      const source = await realpath(path);
      await rm(path);
      await cp(source, path, { recursive: true, dereference: true, filter: (item) => !["node_modules", "src", "tests", ".DS_Store"].includes(basename(item)) });
    } else if (entry.isDirectory()) {
      await materializeLinks(path);
    }
  }
}

async function stageWorkspaceRuntime() {
  const temporary = await mkdtemp(join(tmpdir(), "agentkiln-runtime-"));
  const deployed = join(temporary, "runtime");
  try {
    try {
      await run("pnpm", ["--filter", "@deepseek-ai/dsh-rank-runtime", "deploy", "--legacy", "--prod", "--config.node-linker=hoisted", "--config.auto-install-peers=false", "--config.link-workspace-packages=true", deployed]);
    } finally {
      // Deploy changes pnpm's workspace selection marker; restore development tools.
      await run("pnpm", ["install", "--offline", "--ignore-scripts", "--prod=false", "--config.confirmModulesPurge=false"]);
    }
    await materializeLinks(deployed);
    await cp(join(deployed, "node_modules"), join(runtime, "node_modules"), { recursive: true });
    await mkdir(join(runtime, "apps/execution-runtime"), { recursive: true });
    for (const name of ["history-runner.mjs", "history-input.mjs", "model-budget.mjs"]) {
      await cp(join(deployed, "node_modules/@deepseek-ai/dsh-execution-runtime", name), join(runtime, "apps/execution-runtime", name));
    }
    await writeFile(join(runtime, "package.json"), JSON.stringify({ private: true, type: "module" }) + "\n");
  } finally {
    await rm(temporary, { recursive: true, force: true });
  }
}

await rm(output, { recursive: true, force: true });
await Promise.all([mkdir(resolve(runtime, "bin"), { recursive: true }), mkdir(npmStage, { recursive: true })]);
if (!args.includes("--skip-build")) {
  await run("pnpm", ["run", "build:lib"]);
  await Promise.all([
    run("go", ["build", "-trimpath", "-ldflags", "-s -w", "-o", resolve(runtime, "bin/rankd"), "./cmd/rankd"], { cwd: resolve(root, "rank/backend"), env: { ...process.env, CGO_ENABLED: "0", GOOS: platform, GOARCH: arch === "x64" ? "amd64" : arch } }),
    run("go", ["build", "-trimpath", "-ldflags", "-s -w", "-o", resolve(runtime, "bin/executiond"), "./cmd/executiond"], { cwd: resolve(root, "execution/backend"), env: { ...process.env, CGO_ENABLED: "0", GOOS: platform, GOARCH: arch === "x64" ? "amd64" : arch } }),
    run("go", ["build", "-trimpath", "-ldflags", "-s -w", "-o", resolve(runtime, "bin/execution-worker"), "./cmd/execution-worker"], { cwd: resolve(root, "execution/backend"), env: { ...process.env, CGO_ENABLED: "0", GOOS: platform, GOARCH: arch === "x64" ? "amd64" : arch } }),
    run("pnpm", ["--filter", "@xyzbit/agentkiln-web", "build"], { env: { ...process.env, CI: process.env.CI || "true" } }),
  ]);
} else {
  await Promise.all(["rankd", "executiond", "execution-worker"].map((name) => cp(resolve(root, `rank/var/bin/${name}`), resolve(runtime, `bin/${name}`))));
}
await Promise.all([
  cp(resolve(root, "apps/rank-web/dist/client"), resolve(runtime, "web"), { recursive: true }),
  cp(resolve(root, "apps/rank-web/control-host"), resolve(runtime, "control-host"), { recursive: true }),
  cp(resolve(root, "rank/assets/skills"), resolve(runtime, "rank/assets/skills"), { recursive: true }),
  ...["rankd", "executiond", "execution-worker"].map((name) => chmod(resolve(runtime, `bin/${name}`), 0o755)),
]);
await stageWorkspaceRuntime();
await writeFile(resolve(runtime, "manifest.json"), `${JSON.stringify({ version, platform, arch, builtAt: new Date().toISOString() }, null, 2)}\n`);
await run("tar", ["-czf", archive, "-C", runtime, "."]);
const hash = createHash("sha256").update(await readFile(archive)).digest("hex");
await writeFile(`${archive}.sha256`, `${hash}  ${archiveName}\n`);

const packageJSON = {
  name: "@xyzbit/agentkiln",
  version,
  description: "Build, evaluate, and serve agents.",
  license: "MIT",
  type: "module",
  bin: { agentkiln: "cli.mjs" },
  files: ["cli.mjs"],
  engines: { node: ">=22.19.0" },
  publishConfig: { access: "public" },
  repository: { type: "git", url: "git+https://github.com/xyzbit/agentkiln.git" },
};
await Promise.all([
  cp(resolve(root, "scripts/agentkiln-cli.mjs"), resolve(npmStage, "cli.mjs")),
  writeFile(resolve(npmStage, "package.json"), `${JSON.stringify(packageJSON, null, 2)}\n`),
  writeFile(resolve(npmStage, "README.md"), `# AgentKiln\n\n\`npx -y @xyzbit/agentkiln start\` starts the local service and opens the browser. User data is stored in \`~/.agentkiln\`.\n`),
]);
await chmod(resolve(npmStage, "cli.mjs"), 0o755);
await run("npm", ["pack", npmStage, "--pack-destination", output]);

const npmArchive = resolve(output, `xyzbit-agentkiln-${version}.tgz`);
process.stdout.write(`\n发布产物已生成：\n  ${archive}\n  ${archive}.sha256\n  ${npmArchive}\n\n本机验收：\n  npx -y --package ${npmArchive} agentkiln start --runtime-archive ${archive} --home /tmp/agentkiln-clean\n\n正式发布：\n  gh release create v${version} ${archive} ${archive}.sha256\n  npm publish ${npmArchive} --access public\n`);
