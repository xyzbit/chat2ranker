/** Verify packaged API/Workers and real DSH against an isolated PostgreSQL schema. */
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';

assert.ok(process.env.EXECUTION_TEST_DATABASE_URL, 'Set EXECUTION_TEST_DATABASE_URL to an isolated test database');
const root = resolve(import.meta.dirname, '../../..');
const temporary = await mkdtemp(join(tmpdir(), 'rank-model-quota-'));
const schema = 'quota_' + randomBytes(8).toString('hex');
const token = randomBytes(32).toString('hex');
const children = [];
const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !/(KEY|SECRET|TOKEN|PASSWORD)/.test(key) && !key.startsWith('EXECUTION_') && !key.startsWith('RANK_')));
Object.assign(env, { GOTOOLCHAIN: 'go1.26.0', EXECUTION_DATABASE_URL: process.env.EXECUTION_TEST_DATABASE_URL,
  EXECUTION_SERVICE_TOKEN: token, EXECUTION_CREDENTIAL_KEY: randomBytes(32).toString('base64'), EXECUTION_MODEL_QUOTA_KEY: randomBytes(32).toString('hex') });
let modelCalls = 0;
const provider = createServer(async (req, res) => {
  if (req.method === 'GET') { res.setHeader('Content-Type', 'application/json'); res.end(JSON.stringify({ data: [{ id: 'quota-fixture' }] })); return; }
  let data = ''; for await (const chunk of req) data += chunk;
  const body = JSON.parse(data);
  modelCalls++;
  assert.ok([1, 64].includes(body.max_tokens ?? body.max_completion_tokens), 'Provider receives a bounded output limit');
  res.setHeader('Content-Type', 'text/event-stream');
  res.end('data: ' + JSON.stringify({ id: 'quota', model: 'quota-fixture', choices: [{ index: 0, delta: { role: 'assistant', content: 'Shared quota works' }, finish_reason: null }] }) + '\n\ndata: ' + JSON.stringify({ id: 'quota', model: 'quota-fixture', choices: [{ index: 0, delta: {}, finish_reason: 'stop' }], usage: { prompt_tokens: 12, completion_tokens: 4 } }) + '\n\ndata: [DONE]\n\n');
});
provider.listen(0, '127.0.0.1'); await once(provider, 'listening');
async function command(binary, args) {
  const child = spawn(binary, args, { cwd: join(root, 'execution/backend'), env, stdio: ['ignore', 'ignore', 'pipe'] });
  let stderr = ''; child.stderr.on('data', chunk => { stderr += chunk; });
  const [code] = await once(child, 'exit'); assert.equal(code, 0, stderr);
}
async function wait(check, timeout = 20000) {
  const end = Date.now() + timeout;
  while (Date.now() < end) { const result = await check(); if (result) return result; await delay(100); }
  throw new Error('Model quota acceptance timed out');
}
let args, base;
function start(role) {
  const child = spawn(join(temporary, 'executiond'), [...args, '-role', role, '-worker-id', role + '-' + children.length], { cwd: root, env, stdio: 'ignore' });
  const owned = { child, exited: once(child, 'exit') }; children.push(owned); return owned;
}
async function request(path, body, expected = 200) {
  const response = await fetch(base + path, { method: body === undefined ? 'GET' : 'POST', headers: { authorization: 'Bearer ' + token, 'content-type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(10000) });
  const value = await response.json(); assert.equal(response.status, expected, JSON.stringify(value)); return value;
}
try {
  for (const name of ['executiond', 'execution-worker']) await command('go', ['build', '-o', join(temporary, name), './cmd/' + name]);
  const probe = createServer(); probe.listen(0, '127.0.0.1'); await once(probe, 'listening');
  const port = probe.address().port; await new Promise(done => probe.close(done)); base = `http://127.0.0.1:${port}`;
  env.EXECUTION_MODEL_QUOTA_URL = base + '/v1/model-permits';
  args = ['-addr', '127.0.0.1:' + port, '-postgres-schema', schema, '-repo-root', root, '-worker', join(temporary, 'execution-worker'),
    '-artifacts', join(temporary, 'artifacts'), '-sandboxes', join(temporary, 'sandboxes'), '-worker-timeout=15s', '-cleanup-grace=1s',
    '-persistence-timeout=3s', '-rescue-after=25s', '-shutdown-grace=5s', '-online-slots=1', '-evaluation-slots=0', '-poll-interval=50ms'];
  await command(join(temporary, 'executiond'), [...args, '-migrate']);
  const api = start('api');
  await wait(async () => { try { return (await fetch(base + '/v1/health')).ok; } catch { return false; } });
  const connections = [];
  for (const name of ['first', 'second']) {
    const connection = await request('/v1/model-connections', { name, protocol: 'openai-chat-completions', provider: 'custom', baseUrl: `http://127.0.0.1:${provider.address().port}/v1`, apiKey: 'fixture', defaultModel: 'quota-fixture' }, 201);
    await request('/v1/model-connections/' + connection.id + '/verify', {}); connections.push(connection);
  }
  const definition = { id: 'shared', connections: connections.map(c => c.id), requestsPerMinute: 1, tokensPerMinute: 1000000, maxOutputTokens: 64 };
  const configFile = join(temporary, 'quota.json');
  const configure = async () => { await writeFile(configFile, JSON.stringify(definition)); await command(join(temporary, 'executiond'), [...args, '-configure-model-quota', configFile]); };
  await configure();
  const input = (key, connection, harness = 'dsh') => ({ idempotencyKey: key, spec: { protocolVersion: 2, kind: 'agent', harness, modelConnectionId: connection.id, model: 'quota-fixture', maxModelRequests: harness === 'dsh' ? 2 : 0, messages: [{ role: 'user', content: 'Reply briefly.' }], metadata: { responseId: key } } });
  const unsupported = await request('/v1/executions', { ...input('unsupported', connections[0], 'mock'), spec: { ...input('unsupported', connections[0], 'mock').spec, messages: undefined, prompt: 'hello' } }, 400);
  assert.equal(unsupported.error.code, 'unsupported_model_quota');
  const first = await request('/v1/executions', input('first', connections[0]), 202);
  const second = await request('/v1/executions', input('second', connections[1]), 202);
  const pre = await request('/v1/capacity'); assert.equal(pre.online.ready, 1); assert.equal(pre.online.quotaBlocked, 1);
  modelCalls = 0; start('worker'); start('worker');
  await wait(async () => { const a = await request('/v1/executions/' + first.id); const b = await request('/v1/executions/' + second.id); return [a, b].some(x => x.status === 'completed'); });
  const a = await request('/v1/executions/' + first.id), b = await request('/v1/executions/' + second.id);
  const completed = a.status === 'completed' ? a : b, pending = completed.id === a.id ? b : a;
  assert.equal(modelCalls, 1);
  const requestArtifact = JSON.parse(await readFile(join(temporary, 'artifacts', completed.id, 'request.json'), 'utf8'));
  assert.equal(requestArtifact.modelQuota, undefined, 'Transient quota identity must not enter request artifacts');
  const blocked = await request('/v1/capacity'); assert.equal(blocked.online.ready, 0);
  await request('/v1/executions/' + pending.id + '/cancel', {});
  await wait(async () => (await request('/v1/executions/' + pending.id)).status === 'cancelled');
  const replayInput = completed.id === first.id ? input('first', connections[0]) : input('second', connections[1]);
  const replay = await request('/v1/executions', replayInput, 202); assert.equal(replay.id, completed.id); assert.equal(replay.attempt, 1);
  api.child.kill('SIGTERM'); await api.exited; start('api');
  await wait(async () => { try { return (await fetch(base + '/v1/health')).ok; } catch { return false; } });
  definition.requestsPerMinute = 100; definition.tokensPerMinute = 65; await configure();
  const tpm = await request('/v1/executions', input('tpm', connections[1]), 202);
  const limited = await request('/v1/capacity'); assert.equal(limited.online.ready, 0); assert.equal(limited.online.quotaBlocked, 1);
  await request('/v1/executions/' + tpm.id + '/cancel', {});
  const observed = { modelCalls, sharedConnections: connections.length, workerProcesses: 2, beforeStart: { ready: pre.online.ready, quotaBlocked: pre.online.quotaBlocked }, afterDebitReady: blocked.online.ready, tpmBlockedAfterAPIRestart: limited.online.quotaBlocked, replayAttempt: replay.attempt, unsupportedCode: unsupported.error.code, completedOutput: completed.result.output.trim() };
  assert.deepEqual(observed, JSON.parse(await readFile(new URL('./snapshot.json', import.meta.url), 'utf8')));
  console.log('Model quota packaged snapshot passed:', JSON.stringify(observed));
  console.log('Retained isolated database schema:', schema);
} finally {
  for (const owned of children.reverse()) {
    if (owned.child.exitCode === null && owned.child.signalCode === null) owned.child.kill('SIGTERM');
    await owned.exited;
  }
  await new Promise(done => provider.close(done));
  await rm(temporary, { recursive: true, force: true });
}
