/** Run packaged Execution roles against a real OpenSandbox and snapshot public results. */
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { randomBytes, createHash } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { setTimeout as delay } from 'node:timers/promises';
import { verifyFailedResult } from './failed-result.mjs';

for (const name of ['URL', 'API_KEY', 'IMAGE']) assert.ok(process.env[`EXECUTION_TEST_SANDBOX_${name}`], `EXECUTION_TEST_SANDBOX_${name} is required`);
const directory = dirname(fileURLToPath(import.meta.url));
const root = resolve(directory, '../../..');
const temporary = await mkdtemp(join(tmpdir(), 'rank-remote-snapshot-'));
const children = [];
const token = randomBytes(32).toString('hex');
const schema = `remote_${randomBytes(8).toString('hex')}`;
const postgres = Boolean(process.env.EXECUTION_TEST_DATABASE_URL);
const environment = Object.fromEntries(Object.entries(process.env).filter(([key]) => !/(KEY|SECRET|TOKEN|PASSWORD)/.test(key) && !key.startsWith('EXECUTION_')));
Object.assign(environment, {
  GOTOOLCHAIN: 'go1.26.0', EXECUTION_SERVICE_TOKEN: token, EXECUTION_CREDENTIAL_KEY: randomBytes(32).toString('base64'),
  EXECUTION_SANDBOX_ENDPOINT: process.env.EXECUTION_TEST_SANDBOX_URL,
  EXECUTION_SANDBOX_API_KEY: process.env.EXECUTION_TEST_SANDBOX_API_KEY,
});
if (postgres) environment.EXECUTION_DATABASE_URL = process.env.EXECUTION_TEST_DATABASE_URL;

async function command(binary, args) {
  const child = spawn(binary, args, { cwd: join(root, 'execution/backend'), env: environment, stdio: ['ignore', 'pipe', 'pipe'] });
  let output = '';
  child.stdout.on('data', chunk => { output += chunk; });
  child.stderr.on('data', chunk => { output += chunk; });
  const [code] = await once(child, 'exit');
  assert.equal(code, 0, output);
}

async function wait(check, milliseconds = 55_000) {
  const deadline = Date.now() + milliseconds;
  while (Date.now() < deadline) {
    const result = await check();
    if (result) return result;
    await delay(50);
  }
  throw new Error('Remote Execution example timed out');
}

try {
  const binary = join(temporary, 'executiond');
  await command('go', ['build', '-o', binary, './cmd/executiond']);
  const listener = createServer();
  listener.listen(0, '127.0.0.1');
  await once(listener, 'listening');
  const address = `127.0.0.1:${listener.address().port}`;
  await new Promise(done => listener.close(done));
  const args = ['-addr', address, '-db', join(temporary, 'execution.db'), '-postgres-schema', schema,
    '-executor', 'opensandbox', '-sandbox-pool', schema, '-sandbox-capacity-cpu', '2', '-sandbox-capacity-memory', '2Gi', '-sandbox-scope', schema, '-sandbox-image', process.env.EXECUTION_TEST_SANDBOX_IMAGE,
    '-repo-root', root, '-artifacts', join(temporary, 'artifacts'), '-sandboxes', join(temporary, 'sandboxes'),
    '-credentials', join(temporary, 'credentials'), '-worker-timeout', '45s', '-cleanup-grace', '3s',
    '-rescue-after', '55s', '-shutdown-grace', '5s', '-poll-interval', '25ms', '-worker-version', 'remote-snapshot',
    '-online-slots', '0', '-evaluation-slots', '1'];
  if (postgres) await command(binary, [...args, '-migrate']);
  for (const role of postgres ? ['api', 'worker', 'worker'] : ['all']) {
    const child = spawn(binary, [...args, '-role', role, '-worker-id', `${role}-${children.length}`], { env: environment, stdio: ['ignore', 'pipe', 'pipe'] });
    child.stdout.resume();
    child.stderr.resume();
    children.push({ child, exited: once(child, 'exit') });
  }
  const base = `http://${address}`;
  await wait(async () => { try { return (await fetch(base + '/v1/health')).ok; } catch { return false; } }, 10_000);
  async function request(path, body) {
    const response = await fetch(base + path, { method: body === undefined ? 'GET' : 'POST',
      headers: { authorization: `Bearer ${token}`, 'content-type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(10_000) });
    const value = await response.json();
    assert.ok(response.ok, JSON.stringify(value));
    return value;
  }
  const submit = (key, metadata = {}) => request('/v1/executions', { idempotencyKey: key,
    spec: { protocolVersion: 2, kind: 'agent', harness: 'mock', prompt: 'sandbox demo', metadata } });
  const get = id => request(`/v1/executions/${id}`);
  const first = await submit('complete');
  const running = await wait(async () => { const value = await get(first.id); return value.runtime?.handle && value; });
  const done = await wait(async () => { const value = await get(first.id); return ['completed', 'failed', 'cancelled'].includes(value.status) && value; });
  assert.equal(done.status, 'completed', done.error);
  assert.equal((await submit('complete')).id, first.id);
  const lifecycle = await fetch(`${base}/v1/executions/${first.id}/events`, { headers: { authorization: `Bearer ${token}` }, signal: AbortSignal.timeout(10_000) });
  assert.ok(lifecycle.ok);
  const events = (await lifecycle.text()).split('\n').filter(line => line.startsWith('data: ')).map(line => JSON.parse(line.slice(6)));
  const refs = done.result.artifacts;
  assert.ok(refs.length >= 2);
  for (const ref of refs) {
    const artifact = await request(`/v1/executions/${first.id}/artifacts?path=${encodeURIComponent(ref.path)}`);
    assert.equal(createHash('sha256').update(artifact.content).digest('hex'), ref.sha256);
  }
  const cancelled = await submit('queued-cancel', { responseId: 'unserved-online-queue' });
  await request(`/v1/executions/${cancelled.id}/cancel`, {});
  const stopped = await get(cancelled.id);
  assert.deepEqual(done.runtimePlan, first.runtimePlan);
  assert.equal(done.runtimePlan.endpoint, process.env.EXECUTION_TEST_SANDBOX_URL.replace(/\/$/, ''));
  const snapshot = {
    failedResult: await verifyFailedResult(request),
    runtimeResources: done.runtimePlan.resources,
    executor: done.executor, output: done.result.output, status: done.status, attempt: done.attempt,
    runtimePersistedBeforeCompletion: Boolean(running.runtime.handle), runtimeCleanupPending: done.runtimeCleanupPending ?? false,
    events: events.map(event => event.type), artifactsReadableAfterCleanup: true,
    queuedCancellation: { status: stopped.status, attempt: stopped.attempt, runtimeCreated: Boolean(stopped.runtime) },
  };
  const file = join(directory, 'remote.snapshot.json');
  if (process.argv.includes('--record')) await writeFile(file, JSON.stringify(snapshot, null, 2) + '\n');
  else assert.deepEqual(snapshot, JSON.parse(await readFile(file, 'utf8')));
  process.stdout.write(`Remote Execution snapshot passed (${postgres ? 'PostgreSQL/River, two workers' : 'SQLite'}).\n`);
} finally {
  for (const { child } of children) child.kill('SIGTERM');
  await Promise.all(children.map(async ({ child, exited }) => {
    const timeout = setTimeout(() => child.kill('SIGKILL'), 8_000);
    try { await exited; } finally { clearTimeout(timeout); }
  }));
  if (postgres) await command('go', ['run', join(directory, 'cleanup.go'), schema]);
  await rm(temporary, { recursive: true, force: true });
}
