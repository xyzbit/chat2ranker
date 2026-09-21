import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { createServer } from 'node:net';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { setTimeout as delay } from 'node:timers/promises';
import { verifyFailedResult } from './failed-result.mjs';

const directory = dirname(fileURLToPath(import.meta.url));
const root = resolve(directory, '../../..');
const temporary = await mkdtemp(join(tmpdir(), 'rank-execution-snapshot-'));
const children = [];
const token = randomBytes(32).toString('hex');
const environment = Object.fromEntries(Object.entries(process.env).filter(([name]) => !/(KEY|SECRET|TOKEN|PASSWORD)/.test(name) && !name.startsWith('EXECUTION_')));
Object.assign(environment, {
  EXECUTION_SERVICE_TOKEN: token,
  EXECUTION_CREDENTIAL_KEY: randomBytes(32).toString('base64'),
  EXECUTION_HARNESS_DSH_ARGV: JSON.stringify([process.execPath, join(directory, 'harness.mjs'), '{prompt}', '{workspace}']),
});
const postgres = Boolean(process.env.EXECUTION_TEST_DATABASE_URL);
if (postgres) environment.EXECUTION_DATABASE_URL = process.env.EXECUTION_TEST_DATABASE_URL;
const s3 = Boolean(process.env.EXECUTION_TEST_S3_ENDPOINT);
if (s3) for (const name of ['ENDPOINT', 'BUCKET', 'ACCESS_KEY', 'SECRET_KEY']) environment[`EXECUTION_S3_${name}`] = process.env[`EXECUTION_TEST_S3_${name}`];
const schema = `snapshot_${randomBytes(8).toString('hex')}`;

async function command(binary, args) {
  const child = spawn(binary, args, { cwd: join(root, 'execution/backend'), env: environment, stdio: ['ignore', 'pipe', 'pipe'] });
  let output = '';
  child.stdout.on('data', (chunk) => { output += chunk; });
  child.stderr.on('data', (chunk) => { output += chunk; });
  const [code] = await once(child, 'exit');
  assert.equal(code, 0, output);
}

async function wait(check, milliseconds = 10_000) {
  const deadline = Date.now() + milliseconds;
  while (Date.now() < deadline) {
    const result = await check();
    if (result) return result;
    await delay(25);
  }
  throw new Error('Execution example timed out');
}

try {
  for (const name of ['executiond', 'execution-worker']) {
    await command('go', ['build', '-o', join(temporary, name), `./cmd/${name}`]);
  }
  const listener = createServer();
  listener.listen(0, '127.0.0.1');
  await once(listener, 'listening');
  const address = `127.0.0.1:${listener.address().port}`;
  await new Promise((done) => listener.close(done));
  const args = ['-addr', address, '-db', join(temporary, 'execution.db'), '-postgres-schema', schema,
    '-worker', join(temporary, 'execution-worker'), '-worker-version', 'snapshot',
    '-artifacts', join(temporary, 'artifacts'), '-sandboxes', join(temporary, 'sandboxes'),
    '-credentials', join(temporary, 'credentials'), '-worker-timeout', '5s', '-cleanup-grace', '1s',
    '-persistence-timeout', '2s', '-rescue-after', '9s', '-shutdown-grace', '3s',
    '-online-slots', '1', '-evaluation-slots', '0', '-poll-interval', '25ms', '-reconcile-interval', '25ms'];
  if (s3) args.push('-s3-tls=false');
  if (postgres) await command(join(temporary, 'executiond'), [...args, '-migrate']);
  function start(role, extraArgs = []) {
    const child = spawn(join(temporary, 'executiond'), [...args, '-role', role, '-worker-id', `${role}-${children.length}`, ...extraArgs], { env: environment, stdio: ['ignore', 'pipe', 'pipe'] });
    child.stdout.resume();
    let log = '';
    child.stderr.on('data', (chunk) => { log += chunk; });
    children.push({ child, exited: once(child, 'exit'), log: () => log });
  }
  for (const role of postgres ? ['api', 'worker', 'worker'] : ['all']) start(role);
  const base = `http://${address}`;
  await wait(async () => {
    try { return (await fetch(`${base}/v1/health`)).ok; } catch { return false; }
  });
  async function request(path, body, headers = {}) {
    const response = await fetch(base + path, {
      method: body === undefined ? 'GET' : 'POST',
      headers: { authorization: `Bearer ${token}`, 'content-type': 'application/json', ...headers },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(10_000),
    });
    const value = await response.json();
    assert.ok(response.ok, JSON.stringify(value));
    return value;
  }
  const submit = (key, prompt, online = true) => request('/v1/executions', { idempotencyKey: key, spec: { protocolVersion: 2, kind: 'agent', harness: 'dsh', prompt, metadata: online ? { responseId: key } : {} } });
  const get = (id) => request(`/v1/executions/${id}`);
  const terminal = (id) => wait(async () => { const value = await get(id); return ['completed', 'cancelled', 'failed'].includes(value.status) && value; });
  const created = await submit('quick', 'quick');
  const complete = await terminal(created.id);
  assert.equal(complete.status, 'completed');
  assert.equal((await submit('quick', 'quick')).id, created.id);
  const response = await fetch(`${base}/v1/executions/${created.id}/events`, { headers: { authorization: `Bearer ${token}` }, signal: AbortSignal.timeout(10_000) });
  assert.ok(response.ok);
  const frames = (await response.text()).split('\n\n').filter((frame) => frame.includes('data: '));
  const events = frames.map((frame) => JSON.parse(frame.split('\n').find((line) => line.startsWith('data: ')).slice(6)));
  events.forEach((event, index) => assert.equal(event.sequence, index + 1));
  const after = events[1].sequence;
  const replay = await fetch(`${base}/v1/executions/${created.id}/events`, { headers: { authorization: `Bearer ${token}`, 'last-event-id': String(after) }, signal: AbortSignal.timeout(10_000) });
  const replayFrames = (await replay.text()).split('\n\n').filter((frame) => frame.includes('data: '));
  assert.deepEqual(replayFrames, frames.slice(2));
  const stdout = complete.result.artifacts.find((artifact) => artifact.kind === 'stdout');
  assert.ok(stdout);
  if (s3) {
    assert.match(stdout.sha256, /^[a-f0-9]{64}$/);
    await rm(join(temporary, 'artifacts'), { recursive: true, force: true });
    children[0].child.kill('SIGTERM');
    await children[0].exited;
    start(postgres ? 'api' : 'all');
    await wait(async () => { try { return (await fetch(`${base}/v1/health`)).ok; } catch { return false; } });
  }
  const artifact = await request(`/v1/executions/${created.id}/artifacts?path=${encodeURIComponent(stdout.path)}`);
  const queued = await submit('queued', 'quick', false);
  const capacity = await request('/v1/capacity');
  assert.ok(capacity.observedAt);
  assert.equal((await fetch(`${base}/v1/capacity`)).status, 401);
  const queueCancelled = await request(`/v1/executions/${queued.id}/cancel`, {});
  const active = await submit('running', 'hold');
  const running = await wait(async () => { const value = await get(active.id); return value.status === 'running' && value; });
  assert.equal(running.runtimeCleanupPending, true);
  await request(`/v1/executions/${active.id}/cancel`, {});
  const cancelled = await terminal(active.id);
  const expiringRequest = { idempotencyKey: 'deadline', spec: { protocolVersion: 2, kind: 'agent', harness: 'dsh', prompt: 'quick', queueTimeoutMs: 2000 } };
  const expiring = await request('/v1/executions', expiringRequest);
  assert.equal(expiring.status, 'queued');
  const currentAPI = children.findLast(({ child }) => child.exitCode === null && child.signalCode === null && (postgres ? child.spawnargs.includes('api') : child.spawnargs.includes('all')));
  currentAPI.child.kill('SIGTERM');
  await currentAPI.exited;
  start(postgres ? 'api' : 'all', ['-queue-timeout', '1h']);
  await wait(async () => { try { return (await fetch(`${base}/v1/health`)).ok; } catch { return false; } });
  const expired = await terminal(expiring.id);
  assert.equal(expired.queueExpiresAt, expiring.queueExpiresAt);
  assert.equal(expired.status, 'failed');
  assert.equal(expired.attempt, 0);
  assert.equal(expired.startedAt, undefined);
  assert.equal(expired.runtime, undefined);
  const sameExpired = await request('/v1/executions', expiringRequest);
  assert.equal(sameExpired.id, expired.id);
  assert.equal(sameExpired.status, 'failed');
  const expiryStream = await fetch(`${base}/v1/executions/${expired.id}/events`, { headers: { authorization: `Bearer ${token}` }, signal: AbortSignal.timeout(10_000) });
  const expiredEvents = (await expiryStream.text()).split('\n').filter(line => line.startsWith('data: ')).map(line => JSON.parse(line.slice(6)));
  assert.deepEqual(expiredEvents.map(event => event.type), ['execution.queued', 'execution.failed']);
  const snapshot = {
    failedResult: await verifyFailedResult(request, { responseId: 'failed-result' }),
    complete: { status: complete.status, attempt: complete.attempt, output: complete.result.output, costKnown: complete.result.costKnown },
    lifecycle: events.filter((event) => event.type.startsWith('execution.')).map(({ type, status, attempt }) => ({ type, status, attempt })),
    stream: events.filter((event) => event.type === 'harness.stdout').map((event) => event.message).join(''),
    artifact: artifact.content,
    artifactEncoding: artifact.encoding,
    queueExpiry: { status: expired.status, attempt: expired.attempt, error: expired.error, events: expiredEvents.map(({ type, status, attempt }) => ({ type, status, attempt })) },
    queuedCancellation: { status: queueCancelled.status, attempt: queueCancelled.attempt },
    runningCancellation: { status: cancelled.status, attempt: cancelled.attempt },
    runtimeCleanup: { running: Boolean(running.runtimeCleanupPending), completed: Boolean(complete.runtimeCleanupPending), cancelled: Boolean(cancelled.runtimeCleanupPending) },
    replayFromSequence: after,
    capacity: { executor: capacity.executor, online: capacity.online, evaluation: capacity.evaluation },
  };
  const path = join(directory, 'snapshot.json');
  if (process.argv.includes('--record')) await writeFile(path, JSON.stringify(snapshot, null, 2) + '\n');
  else assert.deepEqual(snapshot, JSON.parse(await readFile(path, 'utf8')));
  process.stdout.write(`Execution lifecycle snapshot passed (${postgres ? 'PostgreSQL + River, two workers' : 'SQLite'}${s3 ? ', S3 artifacts' : ''}).\n`);
} catch (error) {
  for (const child of children) process.stderr.write(child.log());
  throw error;
} finally {
  for (const { child } of children) child.kill('SIGTERM');
  await Promise.all(children.map(async ({ child, exited }) => {
    const timer = setTimeout(() => child.kill('SIGKILL'), 8000);
    try { await exited; } finally { clearTimeout(timer); }
  }));
  if (postgres) {
    // Only this invocation's random schema is removed; no shared test data is reset.
    const cleanup = spawn('go', ['run', join(directory, 'cleanup.go'), schema], { cwd: join(root, 'execution/backend'), env: environment, stdio: 'inherit' });
    const [code] = await once(cleanup, 'exit');
    if (code !== 0) process.exitCode = 1;
  }
  await rm(temporary, { recursive: true, force: true });
}
