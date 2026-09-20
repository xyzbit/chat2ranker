import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { copyFile, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { promisify } from "node:util";
import test from "node:test";

const execute = promisify(execFile);

test("packaged launcher presents AgentKiln commands and data directory", async (t) => {
  const directory = await mkdtemp(path.join(tmpdir(), "agentkiln-launcher-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  await copyFile(new URL("../../../scripts/agentkiln-cli.mjs", import.meta.url), path.join(directory, "cli.mjs"));
  await writeFile(path.join(directory, "package.json"), JSON.stringify({ name: "@xyzbit/agentkiln", version: "1.2.3", type: "module" }));
  const { stdout, stderr } = await execute(process.execPath, [path.join(directory, "cli.mjs"), "--help"]);
  assert.equal(stderr, "");
  assert.equal(stdout, await readFile(new URL("./snapshots/launcher-help.txt", import.meta.url), "utf8"));
});
