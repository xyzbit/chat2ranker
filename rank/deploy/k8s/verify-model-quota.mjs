/** Verify quota admission and bounded real-provider demos through the remote platform. */
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { once } from 'node:events';
import { readFile, writeFile, chmod, mkdir } from 'node:fs/promises';
import { createServer } from 'node:net';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { createClient, dailyNews, requireCompleted, textOf } from '../../examples/responses-client/demo-client.mjs';

const state = join(homedir(), '.local/state/agentkiln/rank-k8s');
const real = process.argv.includes('--deepseek');
const prefix = (real ? 'deepseek-' : 'quota-') + Date.now();
const images = JSON.parse(await readFile(join(state, 'platform-images.json'), 'utf8'));
const mode = JSON.parse(await readFile(join(state, 'platform-mode.json'), 'utf8'));
assert.equal(mode.executor, 'opensandbox');
const executionSecrets = JSON.parse(await readFile(join(state, 'execution-lab.json'), 'utf8'));
const management = JSON.parse(await readFile(join(state, 'platform-lab.json'), 'utf8'));
const kube = ['--kubeconfig=' + join(state, 'config'), '--context=rank-dev', '-n', 'rank-dev'];
const forwards = [], pending = [];
let container, scalerEnabled = false;
const scaling = process.argv.includes('--keda');
assert.ok(!(real && scaling), 'Use deterministic quota traffic for scaling verification');
const report = { startedAt: new Date().toISOString(), provider: real ? 'DeepSeek' : 'fixture', images, checks: [] };
async function command(binary, args, input) {
  const child = spawn(binary, args, { stdio: ['pipe', 'pipe', 'pipe'] }); let out = '';
  child.stdout.on('data', chunk => { out += chunk; }); child.stderr.resume(); child.stdin.end(input);
  const [code] = await once(child, 'exit'); assert.equal(code, 0, binary + ' operation failed; diagnostics suppressed'); return out;
}
async function wait(check, timeout = 90000) {
  const end = Date.now() + timeout;
  while (Date.now() < end) { const value = await check(); if (value) return value; await delay(250); }
  throw new Error('Remote acceptance timed out');
}
async function port() { const s = createServer(); s.listen(0, '127.0.0.1'); await once(s, 'listening'); const p = s.address().port; await new Promise(done => s.close(done)); return p; }
async function forward(service, target) {
  const p = await port(); const child = spawn('kubectl', [...kube, 'port-forward', '--address=127.0.0.1', 'service/' + service, p + ':' + target], { stdio: 'ignore' });
  forwards.push({ child, exit: once(child, 'exit') });
  const base = 'http://127.0.0.1:' + p;
  await wait(async () => { try { const r = await fetch(base + (target === 8790 ? '/v1/health' : '/health'), { signal: AbortSignal.timeout(1000) }); await r.body.cancel(); return true; } catch { return false; } }, 20000);
  return base;
}
let api, execution, authorization;
async function request(base, path, body, auth, method) {
  const r = await fetch(base + path, { method: method ?? (body === undefined ? 'GET' : 'POST'), headers: { authorization: auth, 'content-type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(65000) });
  if (!r.ok) { await r.body.cancel(); throw new Error(path + ': HTTP ' + r.status); } return r.json();
}
const manage = (path, body) => request(api, path, body, authorization);
const exec = (path, body) => request(execution, path, body, 'Bearer ' + executionSecrets.EXECUTION_SERVICE_TOKEN);
async function settled(id) { const value = await exec('/v1/executions/' + id); return ['completed', 'failed', 'cancelled'].includes(value.status) && !value.runtimeCleanupPending && value; }
async function configure(definition) { await command('kubectl', [...kube, 'exec', '-i', 'deployment/execution-api', '--', '/usr/local/bin/executiond', '-configure-model-quota=/dev/stdin'], JSON.stringify(definition)); }
try {
  api = await forward('rank-gateway', 8080); execution = await forward('execution-api', 8790);
  authorization = 'Basic ' + Buffer.from('rank:' + management.GATEWAY_PASSWORD).toString('base64');
  const capacity = await exec('/v1/capacity'); assert.equal(capacity.online.active + capacity.online.ready + capacity.evaluation.active + capacity.evaluation.ready, 0, 'Use an idle lab');
  if (scaling) { scalerEnabled = true; await command('python3.12', ['-B', join(import.meta.dirname, 'autoscaling-lab.py'), 'enable']); }
  let modelURL = mode.modelURL, apiKey = 'rank-lab-fixture', model = 'rank-lab', proxy;
  if (real) {
    const configPath = join(state, 'deepseek-acceptance.json'); const config = JSON.parse(await readFile(configPath, 'utf8'));
    config.proxyToken ||= randomBytes(32).toString('hex'); await writeFile(configPath, JSON.stringify(config) + '\n', { mode: 0o600 }); await chmod(configPath, 0o600);
    const proxyState = join(state, 'deepseek-proxy'); await mkdir(proxyState, { recursive: true, mode: 0o700 });
    const p = await port(); container = 'rank-' + prefix;
    await command('docker', ['run', '-d', '--name', container, '--network', 'rank-dev', '-p', '127.0.0.1:' + p + ':8800', '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges', '--user=' + process.getuid() + ':' + process.getgid(), '--memory=128m', '--cpus=0.5', '-e', 'RANK_PROVIDER_STATE=/private', '--mount', 'type=bind,src=' + proxyState + ',dst=/private', '--mount', 'type=bind,src=' + configPath + ',dst=/config.json,readonly', '--mount', 'type=bind,src=' + join(import.meta.dirname, 'fixtures/bounded-provider.mjs') + ',dst=/proxy.mjs,readonly', '--entrypoint=node', images.images.platform, '/proxy.mjs']);
    proxy = 'http://127.0.0.1:' + p;
    await wait(async () => { try { return (await fetch(proxy + '/facts')).ok; } catch { return false; } }, 10000);
    const ip = (await command('docker', ['inspect', container, '--format', '{{(index .NetworkSettings.Networks "rank-dev").IPAddress}}'])).trim();
    modelURL = 'http://' + ip + ':8800/v1'; apiKey = config.proxyToken; model = 'deepseek-chat';
    report.budgetBefore = (await (await fetch(proxy + '/facts')).json()).attempts.length;
  }
  const connections = [];
  for (let n = 0; n < (real ? 1 : 2); n++) {
    const c = await manage('/api/model-connections', { name: prefix + '-' + n, provider: real ? 'deepseek' : 'custom', protocol: 'openai-chat-completions', baseUrl: modelURL, apiKey, defaultModel: model });
    await manage('/api/model-connections/' + c.id + '/verify', {}); connections.push(c);
  }
  const definition = { id: prefix, connections: connections.map(c => c.id), requestsPerMinute: real ? 10 : 1, tokensPerMinute: 1000000, maxOutputTokens: real ? 512 : 64 };
  await configure(definition);
  if (!real) {
    const submit = async n => { const value = await exec('/v1/executions', { idempotencyKey: prefix + '-' + n, spec: { protocolVersion: 2, kind: 'agent', harness: 'dsh', modelConnectionId: connections[n % connections.length].id, model, maxModelRequests: 1, messages: [{ role: 'user', content: 'Reply briefly.' }], metadata: { responseId: prefix + '-' + n } } }); pending.push(value.id); return value; };
    const first = await submit(0); const done = await wait(() => settled(first.id)); assert.equal(done.status, 'completed', done.error);
    const second = await submit(1); const blocked = await exec('/v1/capacity'); assert.equal(blocked.online.ready, 0); assert.equal(blocked.online.quotaBlocked, 1);
    if (scaling) {
      for (let n = 2; n < 8; n++) await submit(n);
      report.scalingSamples = [];
      const end = Date.now() + 25000;
      while (Date.now() < end) {
        const c = await exec('/v1/capacity');
        const worker = JSON.parse(await command('kubectl', [...kube, 'get', 'deployment/execution-worker', '-o', 'json']));
        assert.equal(c.online.ready, 0); assert.equal(c.online.quotaBlocked, 7); assert.ok(worker.spec.replicas <= 2, 'Quota-only backlog must not grow the Worker pool');
        report.scalingSamples.push({ desired: worker.spec.replicas, readyDemand: c.online.ready, quotaBlocked: c.online.quotaBlocked }); await delay(2000);
      }
      report.checks.push('Seven quota-blocked requests remain outside KEDA demand for 25 seconds without Worker growth');
    }
    await exec('/v1/executions/' + second.id + '/cancel', {}); assert.equal((await wait(() => settled(second.id))).attempt, 0);
    const artifact = done.result.artifacts.find(a => a.kind === 'request'); const content = await exec('/v1/executions/' + done.id + '/artifacts?path=' + encodeURIComponent(artifact.path));
    assert.equal(JSON.parse(content.content).modelQuota, undefined);
    const permitsURL = await forward('rank-model-permits', 8081);
    assert.equal((await fetch(permitsURL + '/v1/executions')).status, 404);
    assert.equal((await fetch(permitsURL + '/v1/model-permits', { method: 'POST', headers: { authorization: 'Bearer ' + executionSecrets.EXECUTION_SERVICE_TOKEN }, body: '{}' })).status, 401);
    report.checks.push('Remote DSH quota callback succeeds; shared second connection is blocked before runtime allocation; request artifacts omit the callback identity; callback ingress denies management routes and service credentials');
    report.quota = { ready: blocked.online.ready, blocked: blocked.online.quotaBlocked, completed: done.id, cancelled: second.id, attempts: done.attempt };
  } else {
    const agent = await manage('/api/agents', { name: prefix, runnerType: 'dsh', modelConnectionId: connections[0].id, model, systemPrompt: '你是一个简洁的助手。直接回答用户，只依据提供的内容，不调用任何工具。' });
    const endpoint = await manage('/api/deployments', { tenantId: 'local', name: prefix, agentFamilyId: agent.familyId, policy: { requestsPerMinute: 20, maxConcurrentResponses: 2, executionTimeoutMs: 60000 } });
    await manage('/api/deployments/' + endpoint.id + '/aliases/production/releases', { agentVersionId: agent.id, policyResult: 'manual_override', actor: 'acceptance', reason: 'Bounded real-provider integration acceptance; no quality evaluation claim' });
    const credential = await manage('/api/serving-credentials', { tenantId: 'local', name: prefix, deploymentIds: [endpoint.id], scopes: ['responses:create', 'responses:read', 'responses:cancel'] });
    const client = createClient({ RANK_BASE_URL: api + '/v1', RANK_API_KEY: credential.apiKey, RANK_MODEL: prefix });
    report.news = await dailyNews(client, prefix); assert.match(report.news.text, /\[\d+\]/);
    const start = Date.now(); const first = requireCompleted(await client.responses.create({ model: prefix, input: '我叫小林，喜欢科技新闻，每天有10分钟。请记住，简短回复。' }));
    const stream = await client.responses.create({ model: prefix, previous_response_id: first.id, input: '我叫什么，喜欢什么，每天多久？', stream: true });
    let output = '', second, firstDeltaMs;
    for await (const event of stream) { if (event.type === 'response.output_text.delta') { firstDeltaMs ??= Date.now() - start; output += event.delta; } if (event.type === 'response.completed') second = event.response; }
    requireCompleted(second); assert.match(output, /小林/); assert.match(output, /科技/); assert.match(output, /10|十/); assert.equal(output, textOf(second));
    report.chat = { first: first.id, second: second.id, text: output, elapsedMs: Date.now() - start, firstDeltaMs, usage: second.usage };
    report.executions = [];
    for (const id of [report.news.id, first.id, second.id]) {
      const detail = await manage('/api/responses/' + id + '?tenantId=local'); const value = await wait(() => settled(detail.response.executionId)); assert.equal(value.status, 'completed'); assert.equal(value.modelQuota.maxOutputTokens, 512);
      const artifact = value.result.artifacts.find(a => a.kind === 'request'); const content = await exec('/v1/executions/' + value.id + '/artifacts?path=' + encodeURIComponent(artifact.path)); assert.equal(JSON.parse(content.content).modelQuota, undefined);
      report.executions.push({ id: value.id, attempt: value.attempt, modelQuota: value.modelQuota, usage: value.result.usage, artifactCount: value.result.artifacts.length, cleanupPending: !!value.runtimeCleanupPending });
    }
    report.providerLedger = await (await fetch(proxy + '/facts')).json(); assert.ok(report.providerLedger.attempts.length <= 10); assert.ok(report.providerLedger.attempts.every(a => a.maxOutputTokens <= 512));
    report.endpoint = endpoint.name;
    report.checks.push('Actual last-24-hour HN headlines and two-turn SSE chat complete through public SDK, Rank, River, remote DSH and DeepSeek; frozen quota, positive usage and S3 evidence retained');
  }
  report.passed = true;
} catch (error) { report.failure = error.message; throw error; }
finally {
  for (const id of pending) { if (!await settled(id)) { await exec('/v1/executions/' + id + '/cancel', {}); await wait(() => settled(id)); } }
  if (scalerEnabled) await command('python3.12', ['-B', join(import.meta.dirname, 'autoscaling-lab.py'), 'disable']);
  if (container) await command('docker', ['rm', '-f', container]);
  for (const owned of forwards) { owned.child.kill('SIGTERM'); await owned.exit; }
  report.endedAt = new Date().toISOString(); const path = join(state, prefix + '-acceptance.json'); await writeFile(path, JSON.stringify(report, null, 2) + '\n'); console.log('Acceptance report:', path, 'passed:', !!report.passed);
}
