import { createHash } from "node:crypto";
import { chmod, cp, mkdir, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { spawn } from "node:child_process";
import { arch as hostArch, platform as hostPlatform } from "node:os";
import { dirname, resolve } from "node:path";
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
const output = resolve(root, "dist/chat2ranker");
const runtime = resolve(output, `runtime-${target}`);
const npmStage = resolve(output, "npm-package");
const archiveName = `chat2ranker-${version}-${target}.tar.gz`;
const archive = resolve(output, archiveName);
const dshVersion = "0.1.0-rc.8";

function run(command, commandArgs, options = {}) {
  return new Promise((resolveRun, rejectRun) => {
    const child = spawn(command, commandArgs, { cwd: root, stdio: "inherit", ...options });
    child.once("error", rejectRun);
    child.once("exit", (code) => code === 0 ? resolveRun() : rejectRun(new Error(`${command} exited with ${code}`)));
  });
}

async function packageVersion(path) {
  return JSON.parse(await readFile(resolve(root, path), "utf8")).version;
}

async function dshOverrides() {
  const groups = await readdir(resolve(root, "packages"));
  const manifests = (await Promise.all(groups.map((group) => readdir(resolve(root, "packages", group)).then((names) => names.map((name) => resolve(root, "packages", group, name, "package.json")), () => [])))).flat();
  const packages = await Promise.all(manifests.map((path) => readFile(path, "utf8").then(JSON.parse, () => null)));
  return Object.fromEntries(packages.filter((item) => item?.name?.startsWith("@deepseek-ai/dsh-")).map((item) => [item.name, dshVersion]));
}

async function verifyDSHRuntime() {
  const store = resolve(runtime, "node_modules/.pnpm");
  const manifests = (await Promise.all((await readdir(store)).map((entry) => readdir(resolve(store, entry, "node_modules/@deepseek-ai")).then((names) => names.filter((name) => name.startsWith("dsh")).map((name) => resolve(store, entry, "node_modules/@deepseek-ai", name, "package.json")), () => [])))).flat();
  const packages = await Promise.all(manifests.map((path) => readFile(path, "utf8").then(JSON.parse)));
  const versions = new Map();
  for (const item of packages.filter((item) => item.name?.startsWith("@deepseek-ai/dsh"))) versions.set(item.name, new Set([...(versions.get(item.name) || []), item.version]));
  const mixed = [...versions].filter(([, installed]) => installed.size !== 1 || !installed.has(dshVersion));
  if (mixed.length) throw new Error(`DSH Runtime 版本不一致：${mixed.map(([name, installed]) => `${name}@${[...installed].join("|")}`).join(", ")}`);
}

await rm(output, { recursive: true, force: true });
await Promise.all([mkdir(resolve(runtime, "bin"), { recursive: true }), mkdir(npmStage, { recursive: true })]);
if (!args.includes("--skip-build")) {
  await Promise.all([
    run("go", ["build", "-trimpath", "-ldflags", "-s -w", "-o", resolve(runtime, "bin/rankd"), "./cmd/rankd"], { cwd: resolve(root, "rank/backend"), env: { ...process.env, CGO_ENABLED: "0", GOOS: platform, GOARCH: arch === "x64" ? "amd64" : arch } }),
    run("go", ["build", "-trimpath", "-ldflags", "-s -w", "-o", resolve(runtime, "bin/executiond"), "./cmd/executiond"], { cwd: resolve(root, "execution/backend"), env: { ...process.env, CGO_ENABLED: "0", GOOS: platform, GOARCH: arch === "x64" ? "amd64" : arch } }),
    run("go", ["build", "-trimpath", "-ldflags", "-s -w", "-o", resolve(runtime, "bin/execution-worker"), "./cmd/execution-worker"], { cwd: resolve(root, "execution/backend"), env: { ...process.env, CGO_ENABLED: "0", GOOS: platform, GOARCH: arch === "x64" ? "amd64" : arch } }),
    run("pnpm", ["--filter", "@xyzbit/chat2ranker-web", "build"], { env: { ...process.env, CI: process.env.CI || "true" } }),
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
await writeFile(resolve(runtime, "package.json"), `${JSON.stringify({
  private: true,
  type: "module",
  dependencies: {
    "@deepseek-ai/cordis": await packageVersion("vendor/cordis/package.json"),
    "@deepseek-ai/dsh": dshVersion,
    "@deepseek-ai/dsh-app-boot": dshVersion,
    "@deepseek-ai/dsh-llm": dshVersion,
    "@deepseek-ai/dsh-session": dshVersion,
  },
}, null, 2)}\n`);
const overrides = await dshOverrides();
await writeFile(resolve(runtime, "pnpm-workspace.yaml"), `packages: []\noverrides:\n${Object.keys(overrides).sort().map((name) => `  '${name}': ${dshVersion}`).join("\n")}\nallowBuilds:\n  '@deepseek-ai/dsh-subprocess-local': true\n  koffi: true\n  node-pty: true\n  '@google/genai': false\n  protobufjs: false\n  node-addon-require-builtin: false\n`);
await run("pnpm", ["install", "--prod", "--dir", runtime], { env: { ...process.env, CI: process.env.CI || "true" } });
await verifyDSHRuntime();
await writeFile(resolve(runtime, "manifest.json"), `${JSON.stringify({ version, platform, arch, builtAt: new Date().toISOString() }, null, 2)}\n`);
await run("tar", ["-czf", archive, "-C", runtime, "."]);
const hash = createHash("sha256").update(await readFile(archive)).digest("hex");
await writeFile(`${archive}.sha256`, `${hash}  ${archiveName}\n`);

const packageJSON = {
  name: "@xyzbit/chat2ranker",
  version,
  description: "Conversation-first local Agent evaluation platform",
  license: "MIT",
  type: "module",
  bin: { chat2ranker: "cli.mjs" },
  files: ["cli.mjs"],
  engines: { node: ">=22.19.0" },
  publishConfig: { access: "public" },
  repository: { type: "git", url: "git+https://github.com/xyzbit/chat2ranker.git" },
};
await Promise.all([
  cp(resolve(root, "scripts/chat2ranker-cli.mjs"), resolve(npmStage, "cli.mjs")),
  writeFile(resolve(npmStage, "package.json"), `${JSON.stringify(packageJSON, null, 2)}\n`),
  writeFile(resolve(npmStage, "README.md"), `# Chat2Ranker\n\n\`npx -y @xyzbit/chat2ranker start\` starts the local service and opens the browser. User data is stored in \`~/.chat2ranker\`.\n`),
]);
await chmod(resolve(npmStage, "cli.mjs"), 0o755);
await run("npm", ["pack", npmStage, "--pack-destination", output]);

const npmArchive = resolve(output, `xyzbit-chat2ranker-${version}.tgz`);
process.stdout.write(`\n发布产物已生成：\n  ${archive}\n  ${archive}.sha256\n  ${npmArchive}\n\n本机验收：\n  npx -y --package ${npmArchive} chat2ranker start --runtime-archive ${archive} --home /tmp/chat2ranker-clean\n\n正式发布：\n  gh release create v${version} ${archive} ${archive}.sha256\n  npm publish ${npmArchive} --access public\n`);
