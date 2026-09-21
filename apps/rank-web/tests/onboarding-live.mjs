import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtemp, mkdir, readFile, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { parseEnv } from "node:util";
import { unzipSync, strFromU8 } from "fflate";

const repo = path.resolve(import.meta.dirname, "../../..");
const artifacts = path.join(repo, "output/playwright");
const platform = process.env.ONBOARDING_URL || "http://127.0.0.1:4173";
const model = process.env.ONBOARDING_MODEL || "demo-integration";
const root = await mkdtemp(path.join(os.tmpdir(), "rank-onboarding-"));
const evidence = { root, checks: [], rejected: [] };
const keys = [];
async function operator(route, body) {
  const response = await fetch(platform + route, { method: body ? "POST" : "GET", headers: { "Content-Type": "application/json" }, body: body ? JSON.stringify(body) : undefined });
  assert.ok(response.ok, `${route}: HTTP ${response.status}`);
  return response.json();
}
async function command(cwd, args) {
  const child = spawn("npm", args, { cwd, env: process.env, stdio: ["ignore", "pipe", "pipe"] });
  let output = "";
  child.stdout.on("data", data => { output += data; });
  child.stderr.on("data", data => { output += data; });
  await new Promise((resolve, reject) => { child.once("error", reject); child.once("exit", code => code === 0 ? resolve() : reject(new Error(`npm ${args.join(" ")} failed: ${output}`))); });
  console.log(`PASS: ${path.basename(cwd)} npm ${args.join(" ")}`);
}
try {
  const deployments = await operator("/api/deployments?tenantId=local");
  const deployment = deployments.find(item => item.name === model);
  assert.ok(deployment, "Publish the demo endpoint before acceptance");
  async function key(scopes) {
    const value = await operator("/api/serving-credentials", { tenantId: deployment.tenantId, name: `onboarding-${Date.now()}`, deploymentIds: [deployment.id], scopes });
    keys.push(value);
    return value.apiKey;
  }
  const secret = await key(["responses:create", "responses:read", "responses:cancel"]);
  const readonly = await key(["responses:read"]);
  for (const kind of ["news", "chat"]) {
    const archive = unzipSync(await readFile(path.join(artifacts, `rank-${kind}.zip`)));
    const names = Object.keys(archive);
    assert.ok(names.length > 10);
    assert.ok(names.every(name => name.startsWith(`rank-${kind}/`) && !name.includes("..") && !name.includes("node_modules/") && !name.endsWith("/.env")));
    for (const [name, data] of Object.entries(archive)) {
      assert.ok(!strFromU8(data).includes("rnk_live_"), `Secret-like material in ${name}`);
      await mkdir(path.dirname(path.join(root, name)), { recursive: true });
      await writeFile(path.join(root, name), data);
    }
    const directory = path.join(root, `rank-${kind}`);
    const template = await readFile(path.join(directory, ".env.example"), "utf8");
    const env = parseEnv(template);
    assert.equal(env.RANK_MODEL, model); assert.equal(env.RANK_BASE_URL, platform + "/v1");
    assert.equal(env.RANK_API_KEY, ""); assert.equal(env.DEMO_KIND, kind);
    await writeFile(path.join(directory, ".env"), template.replace("RANK_API_KEY=", `RANK_API_KEY=${secret}`), { mode: 0o600 });
    await command(directory, ["ci", "--no-audit", "--no-fund"]);
    await command(directory, ["test"]);
    evidence.checks.push(`${kind}: browser ZIP, blank key, endpoint prefill, clean npm ci, keyless snapshot`);
  }
  await command(path.join(root, "rank-news"), ["run", "news"]);
  await command(path.join(root, "rank-chat"), ["run", "verify:demos"]);
  evidence.live = JSON.parse(await readFile(path.join(root, "rank-chat/output/integration-demos-live.json"), "utf8"));
  evidence.checks.push("Real news, two-turn recall, streaming, persistence, isolation and cancel from extracted packages");
  for (const [label, token, target, expected] of [["invalid key", "invalid-key", model, 401], ["missing create permission", readonly, model, 403], ["unknown endpoint", secret, "missing-onboarding-endpoint", 404]]) {
    const result = await fetch(platform + "/v1/responses", { method: "POST", headers: { "Content-Type": "application/json", authorization: `Bearer ${token}` }, body: JSON.stringify({ model: target, input: "hello" }) });
    const body = await result.json();
    assert.equal(result.status, expected, JSON.stringify(body));
    assert.ok(!body.id);
    evidence.rejected.push({ label, status: result.status, error: body.error });
    console.log(`PASS: ${label} (${result.status})`);
  }
  const stream = await fetch(platform + "/v1/responses", { method: "POST", headers: { "Content-Type": "application/json", authorization: `Bearer ${secret}` }, body: JSON.stringify({ model, input: "用中文分五点介绍如何阅读科技新闻。", stream: true }) });
  assert.equal(stream.status, 200);
  const reader = stream.body.getReader();
  let buffer = "", accepted;
  const decoder = new TextDecoder();
  while (!accepted) {
    const chunk = await reader.read(); assert.ok(!chunk.done, "Stream ended before acceptance");
    buffer += decoder.decode(chunk.value, { stream: true });
    const frames = buffer.split(/\r?\n\r?\n/); buffer = frames.pop();
    for (const frame of frames) {
      const data = frame.split(/\r?\n/).find(line => line.startsWith("data: "));
      if (!data) continue;
      const event = JSON.parse(data.slice(6));
      if (event.response?.id && !accepted) accepted = event;
    }
  }
  await reader.cancel();
  const resumed = await fetch(`${platform}/v1/responses/${accepted.response.id}?stream=true&starting_after=${accepted.sequence_number}`, { headers: { authorization: `Bearer ${secret}` }, signal: AbortSignal.timeout(180_000) });
  assert.equal(resumed.status, 200);
  const events = (await resumed.text()).split(/\r?\n/).filter(line => line.startsWith("data: ")).map(line => JSON.parse(line.slice(6)));
  assert.ok(events.every(event => event.sequence_number > accepted.sequence_number));
  assert.ok(events.some(event => event.type === "response.completed"));
  evidence.resume = { responseId: accepted.response.id, after: accepted.sequence_number, completed: true };
  const detail = await operator(`/api/responses/${evidence.live.chat[1].id}?tenantId=${deployment.tenantId}`);
  assert.equal(detail.response.id, evidence.live.chat[1].id);
  evidence.checks.push("Aborted SSE resumes without a second creation; real Response ID resolves in invocation records");
  console.log("PASS: interrupted stream recovery and invocation lookup");
} finally {
  for (const value of keys) await operator(`/api/serving-credentials/${value.id}/revoke?tenantId=local`, {});
  evidence.temporaryKeysRevoked = keys.length;
  await writeFile(path.join(artifacts, "onboarding-live.json"), JSON.stringify(evidence, null, 2) + "\n");
  console.log("Evidence: output/playwright/onboarding-live.json");
}
