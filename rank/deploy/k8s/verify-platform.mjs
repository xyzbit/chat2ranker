/** Exercise actual public clients through the local cluster gateway and real DSH worker processes. */
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { readFile, writeFile } from 'node:fs/promises';
import { randomBytes, createHash } from 'node:crypto';
import { verifyAccess } from '../../examples/access-control/scenario.mjs';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { createClient, dailyNews, requireCompleted, textOf } from '../../examples/responses-client/demo-client.mjs';
import { verifyFailedResult } from '../../examples/execution-lifecycle/failed-result.mjs';

const state = join(homedir(), '.local/state/agentkiln/rank-k8s');
let mode = { executor: 'local-process', modelURL: 'http://rank-model-fixture:8800/v1' };
try { mode = JSON.parse(await readFile(join(state, 'platform-mode.json'), 'utf8')); } catch (error) {
  if (error.code !== 'ENOENT') throw error;
}
const executionSecrets = JSON.parse(await readFile(join(state, 'execution-lab.json'), 'utf8'));
const secrets = JSON.parse(await readFile(join(state, 'platform-lab.json'), 'utf8'));
const basic = 'Basic ' + Buffer.from('rank:' + secrets.GATEWAY_PASSWORD).toString('base64');
const base = process.env.RANK_LAB_GATEWAY || 'http://127.0.0.1:18793';
const kube = ['--kubeconfig=' + join(state, 'config'), '--context=rank-dev', '-n', 'rank-dev'];
const command = (args) => new Promise((resolve, reject) => {
  const child = spawn('kubectl', [...kube, ...args], { stdio: ['ignore', 'pipe', 'pipe'] });
  let output = ''; child.stdout.on('data', chunk => { output += chunk; }); child.stderr.on('data', chunk => { output += chunk; });
  child.on('error', reject); child.on('exit', code => code === 0 ? resolve(output) : reject(new Error(output)));
});
const api = async (path, body, method = body === undefined ? 'GET' : 'POST', timeout = 90_000) => {
  const response = await fetch(base + path, { method, headers: { authorization: basic, 'content-type': 'application/json' }, ...(body === undefined ? {} : { body: JSON.stringify(body) }), signal: AbortSignal.timeout(timeout) });
  const payload = await response.json();
  if (!response.ok) throw new Error(path + ': ' + JSON.stringify(payload));
  return payload;
};
const wait = async (check, { retryErrors = false } = {}) => {
  const deadline = Date.now() + 60_000;
  while (Date.now() < deadline) {
    try { const value = await check(); if (value) return value; } catch (error) {
      if (!retryErrors) throw error; // Only readiness checks retry transient connection failures.
    }
    await new Promise(resolve => setTimeout(resolve, 200));
  }
  throw new Error('Platform condition did not converge');
};
const forward = spawn('kubectl', [...kube, 'port-forward', '--address=127.0.0.1', 'service/rank-gateway', '18793:8080'], { stdio: 'ignore' });
const executionForward = spawn('kubectl', [...kube, 'port-forward', '--address=127.0.0.1', 'service/execution-api', '18794:8790'], { stdio: 'ignore' });
const executionRequest = async (path, body) => {
  const response = await fetch('http://127.0.0.1:18794' + path, { method: body === undefined ? 'GET' : 'POST', headers: { authorization: 'Bearer ' + executionSecrets.EXECUTION_SERVICE_TOKEN, 'content-type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(30_000) });
  if (!response.ok) throw new Error('Execution request failed: ' + response.status);
  return response;
};
const report = { startedAt: new Date().toISOString(), checks: [] };
try {
  await wait(() => fetch(base + '/health', { signal: AbortSignal.timeout(2000) }).then(response => response.ok), { retryErrors: true });
  await wait(() => executionRequest('/v1/health').then(response => response.ok), { retryErrors: true });
  assert.equal((await fetch(base + '/api/bootstrap')).status, 401);
  assert.equal((await fetch(base + '/')).status, 200);
  assert.equal((await fetch(base + '/control/v1/health')).status, 200);
  assert.equal((await fetch(base + '/api/experiments', { method: 'POST', headers: { authorization: basic, 'content-type': 'application/json', origin: 'https://untrusted.example' }, body: JSON.stringify({ title: 'Rejected cross-origin fixture' }) })).status, 403);
  assert.equal((await fetch(base + '/v1/responses', { method: 'POST', headers: { 'content-type': 'application/json' }, body: '{}' })).status, 401);
  assert.equal((await fetch(base + '/api/internal/control/commands', { headers: { authorization: basic } })).status, 404);
  assert.equal((await fetch(base + '/', { headers: { authorization: basic } })).status, 200);
  report.checks.push('Gateway protects management, leaves public API bearer authentication to Rank, and hides internal commands');
  const prefix = 'cluster-' + Date.now();
  report.access = await verifyAccess(async (method, path, body, authorization) => {
    const response = await fetch(base + path, { method, headers: { authorization, 'content-type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(40000) });
    return { status: response.status, body: await response.json() };
  }, { operator: basic, subject: 'rank', prefix: prefix + '-access' });
  const secret = JSON.parse(await command(['get', 'secret/rank-management', '-o', 'json']));
  const originalAccess = secret.data['access.json'];
  const access = JSON.parse(Buffer.from(originalAccess, 'base64').toString('utf8'));
  const temporaryToken = randomBytes(32).toString('hex');
  access.operators.push({ subject: 'verification', tokenSha256: createHash('sha256').update(temporaryToken).digest('hex'), expiresAt: new Date(Date.now() + 600000).toISOString() });
  const patchAccess = value => command(['patch', 'secret/rank-management', '--type=merge', '-p', JSON.stringify({ data: { 'access.json': value } })]);
  const waitCredential = async expected => {
    const deadline = Date.now() + 180000;
    while (Date.now() < deadline) {
      const response = await fetch(base + '/api/identity', { headers: { authorization: 'Bearer ' + temporaryToken }, signal: AbortSignal.timeout(3000) });
      await response.body?.cancel();
      if (response.status === expected) return;
      await new Promise(resolve => setTimeout(resolve, 250));
    }
    throw new Error('Projected operator credential did not converge');
  };
  try {
    await patchAccess(Buffer.from(JSON.stringify(access)).toString('base64'));
    await waitCredential(200);
  } finally { await patchAccess(originalAccess); }
  await waitCredential(401);
  report.access.operatorRotation = true;
  const serviceChecks = JSON.parse(await command(['exec', 'deployment/rank-control', '--', 'node', '-e', `
    const root = 'http://rank-api:8787';
    const headers = { 'x-rank-control-token': process.env.RANK_CONTROL_TOKEN };
    const bootstrap = await fetch(root + '/api/bootstrap', { headers }); await bootstrap.body.cancel();
    const credentials = await fetch(root + '/api/serving-credentials', { headers }); await credentials.body.cancel();
    const control = await fetch('http://127.0.0.1:8788/control/v1/experiments/missing/session'); await control.body.cancel();
    console.log(JSON.stringify({ bootstrap: bootstrap.status, credentials: credentials.status, control: control.status }));
  `]));
  assert.deepEqual(serviceChecks, { bootstrap: 200, credentials: 403, control: 401 });
  report.access.serviceScope = serviceChecks;
  report.checks.push('Independent operator identities, live credential revocation, scoped control service and tenant/endpoint public permissions hold through the gateway');

  report.failedResult = await verifyFailedResult(async (path, body) => (await executionRequest(path, body)).json(), { responseId: prefix + '-failure' }, prefix + '-failure');
  report.checks.push('Known worker failure retains output, usage and S3 result evidence after cleanup; idempotent replay stays on attempt one');
  const connection = await api('/api/model-connections', { name: prefix, provider: 'custom', protocol: 'openai-chat-completions', baseUrl: mode.modelURL, apiKey: 'rank-lab-fixture', defaultModel: 'rank-lab' });
  await api(`/api/model-connections/${connection.id}/verify`, {});
  const budgetModel = 'rank-budget-429-' + prefix;
  const budgetRequest = { idempotencyKey: prefix + '-budget', spec: { protocolVersion: 2, kind: 'agent', harness: 'dsh', modelConnectionId: connection.id, model: budgetModel, maxModelRequests: 1, messages: [{ role: 'user', content: 'Reply briefly.' }], metadata: { responseId: prefix + '-budget' } } };
  const budgetAccepted = await (await executionRequest('/v1/executions', budgetRequest)).json();
  assert.equal(budgetAccepted.maxModelRequests, 1);
  const budgetFailed = await wait(async () => { const value = await (await executionRequest('/v1/executions/' + budgetAccepted.id)).json(); return ['failed', 'completed', 'cancelled'].includes(value.status) && value; });
  assert.equal(budgetFailed.status, 'failed');
  assert.match(budgetFailed.error, /execution_model_budget_exhausted/);
  assert.equal(Boolean(budgetFailed.runtimeCleanupPending), false);
  const budgetReplay = await (await executionRequest('/v1/executions', budgetRequest)).json();
  assert.equal(budgetReplay.id, budgetAccepted.id);
  assert.equal(budgetReplay.attempt, 1);
  let modelLog;
  if (mode.executor === 'opensandbox') {
    modelLog = await new Promise((resolve, reject) => {
      const child = spawn('docker', ['logs', 'rank-opensandbox-model'], { stdio: ['ignore', 'pipe', 'pipe'] });
      let output = ''; child.stdout.on('data', chunk => { output += chunk; }); child.stderr.on('data', chunk => { output += chunk; });
      child.on('error', reject); child.on('exit', code => code === 0 ? resolve(output) : reject(new Error('Model fixture logs unavailable')));
    });
  } else modelLog = await command(['logs', 'deployment/rank-model-fixture']);
  const budgetCalls = modelLog.split('\n').filter(line => { try { return JSON.parse(line).model === budgetModel; } catch { return false; } }).length;
  assert.equal(budgetCalls, 1);
  report.modelBudget = { limit: budgetFailed.maxModelRequests, providerRequests: budgetCalls, status: budgetFailed.status, replayAttempt: budgetReplay.attempt, cleanupPending: false };
  assert.deepEqual(report.modelBudget, JSON.parse(await readFile(new URL('./fixtures/model-budget.snapshot.json', import.meta.url), 'utf8')));
  report.checks.push('Frozen model budget stops repeated 429 calls after one provider request and survives idempotent replay');
  for (const role of ['control', 'judge']) await api('/api/system-models/' + role, { connectionId: connection.id, model: 'rank-lab' }, 'PUT');
  await wait(async () => (await api('/control/v1/health')).mode === 'configured');
  const agent = await api('/api/agents', { name: prefix, runnerType: 'dsh', modelConnectionId: connection.id, model: 'rank-lab', systemPrompt: 'Answer briefly and preserve conversation context.' });
  const deployment = await api('/api/deployments', { tenantId: 'local', name: prefix, agentFamilyId: agent.familyId, policy: { requestsPerMinute: 100, maxConcurrentResponses: 4, executionTimeoutMs: 60_000 } });
  await api(`/api/deployments/${deployment.id}/aliases/production/releases`, { agentVersionId: agent.id, policyResult: 'manual_override', actor: 'local-lab', reason: 'Deterministic fixture endpoint; production quality is not asserted' });
  const credential = await api('/api/serving-credentials', { tenantId: 'local', name: prefix, deploymentIds: [deployment.id], scopes: ['responses:create', 'responses:read', 'responses:cancel', 'responses:approve'] });
  const client = createClient({ RANK_BASE_URL: base + '/v1', RANK_API_KEY: credential.apiKey, RANK_MODEL: prefix });
  const now = Date.now();
  report.news = await dailyNews(client, prefix, { now, request: async () => ({ ok: true, json: async () => ({ hits: [1, 2].map(number => ({ objectID: String(number), title: 'Rank local acceptance ' + number, points: 10 - number, num_comments: 1, created_at_i: Math.floor(now / 1000) - 60, created_at: new Date(now - 60_000).toISOString(), url: 'https://example.test/news/' + number })) }) }) });
  assert.match(report.news.text, /\[1\]/);
  const first = requireCompleted(await client.responses.create({ model: prefix, input: '我叫小林，喜欢科技新闻，每天有10分钟。' }));
  const stream = await client.responses.create({ model: prefix, previous_response_id: first.id, input: '我叫什么，喜欢什么，每天多久？', stream: true });
  let streamed = '', second;
  for await (const event of stream) {
    if (event.type === 'response.output_text.delta') streamed += event.delta;
    if (event.type === 'response.completed') second = event.response;
  }
  requireCompleted(second);
  assert.match(streamed, /小林/); assert.match(streamed, /科技/); assert.match(streamed, /10/);
  assert.equal(streamed, textOf(second));
  assert.equal(second.deployment.agent_version_id, agent.id);
  report.chat = { first: first.id, second: second.id, text: streamed, agentVersionId: agent.id };
  report.checks.push('Existing daily-news client and two-turn OpenAI SDK chat cross gateway, Rank, PostgreSQL/River, DSH workers and local provider');
  const detail = await api(`/api/responses/${second.id}?tenantId=local`);
  report.executionId = detail.response.executionId;
  await wait(() => executionRequest('/v1/health'), { retryErrors: true });
  const execution = await (await executionRequest(`/v1/executions/${report.executionId}`)).json();
  assert.equal(execution.executor, mode.executor);
  if (mode.executor === 'opensandbox') {
    assert.ok(execution.runtime?.handle);
    assert.deepEqual(execution.runtimePlan.egress, JSON.parse(await readFile(join(state, 'sandbox-egress-policy.json'), 'utf8')));
  }
  report.runtime = { executor: execution.executor, image: execution.runtime?.image, egress: execution.runtimePlan?.egress, cleanupPending: Boolean(execution.runtimeCleanupPending) };
  assert.equal(Boolean(execution.runtimeCleanupPending), false);
  assert.ok(!execution.spec.prompt);
  assert.deepEqual(execution.spec.messages.map(message => message.role), ['user', 'assistant', 'user']);
  assert.ok(execution.result.artifacts.length > 0);
  const stdout = execution.result.artifacts.find(artifact => artifact.path.endsWith('stdout.txt'));
  assert.ok(stdout);
  const artifact = await (await executionRequest(`/v1/executions/${report.executionId}/artifacts?path=${encodeURIComponent(stdout.path)}`)).json();
  assert.match(artifact.content, /小林/);
  report.artifact = { path: stdout.path, sha256: stdout.sha256 };
  const eventText = await (await executionRequest(`/v1/executions/${report.executionId}/events`)).text();
  const events = eventText.split('\n').filter(line => line.startsWith('data: ')).map(line => JSON.parse(line.slice(6)));
  assert.equal(events.at(-1).type, 'execution.completed');
  const cursor = events.at(-2).sequence;
  const replayText = await (await executionRequest(`/v1/executions/${report.executionId}/events?after=${cursor}`)).text();
  const replay = replayText.split('\n').filter(line => line.startsWith('data: ')).map(line => JSON.parse(line.slice(6)));
  assert.deepEqual(replay, [events.at(-1)]);
  report.checks.push('Structured roles survive durable admission; S3-backed stdout and cursor-based terminal SSE replay remain readable');

  const trial = await api('/api/trial-responses', { agentVersionId: agent.id, input: 'Remember Ada' });
  const trialDone = await wait(async () => {
    const history = await api(`/api/trials/${trial.conversationId}`);
    const response = history.messages.find(item => item.id === trial.id);
    if (['failed', 'cancelled'].includes(response.status)) throw new Error('Cluster trial failed');
    return response.status === 'completed' && response;
  });
  assert.equal(trialDone.outputText, 'Ada');
  const byVersion = await api(`/api/trials?agentVersionId=${agent.id}&directOnly=true`);
  const byFamily = await api(`/api/trials?agentFamilyId=${agent.familyId}`);
  assert.deepEqual(byVersion.map(item => item.id), [trial.conversationId]);
  assert.deepEqual(byFamily.map(item => item.id), [trial.conversationId]);
  report.trial = { conversationId: trial.conversationId, responseId: trial.id, title: byVersion[0].title };
  report.checks.push('Direct Agent trial completes through DSH and PostgreSQL history filters return the same conversation');

  const experiment = await api('/api/experiments', { title: prefix + ' control' });
  const turn = await api(`/control/v1/experiments/${experiment.id}/messages`, { content: '确认本地中控服务连接。' });
  const control = await fetch(base + `/control/v1/experiments/${experiment.id}/turns/${turn.turnId}`, { headers: { authorization: basic }, signal: AbortSignal.timeout(60_000) }).then(response => response.text());
  assert.match(control, /Rank 本地集群中控服务已连接/);
  report.control = { experimentId: experiment.id, turnId: turn.turnId };
  const beforeRestartSession = await api(`/control/v1/experiments/${experiment.id}/session`);
  report.control.originalEventCount = beforeRestartSession.eventCount;
  report.checks.push('Configured Rank control host runs a separate persistent native conversation');
  for (const name of ['rank-api', 'rank-control']) {
    await command(['rollout', 'restart', 'deployment/' + name]);
    await command(['rollout', 'status', 'deployment/' + name, '--timeout=60s']);
  }
  await wait(() => api('/api/health', undefined, 'GET', 2000), { retryErrors: true });
  assert.equal(textOf(requireCompleted(await client.responses.retrieve(second.id))), streamed);
  const third = requireCompleted(await client.responses.create({ model: prefix, previous_response_id: second.id, input: '再确认我的名字。' }));
  assert.match(textOf(third), /小林/);
  report.chat.afterRestart = third.id;
  const session = await api(`/control/v1/experiments/${experiment.id}/session`);
  assert.equal(session.resumed, true);
  assert.ok(session.eventCount >= report.control.originalEventCount);
  report.control.resumedEventCount = session.eventCount;
  report.checks.push('Rank and control Pod replacement retains serving lineage and control history; a fresh invocation continues the conversation');
  try {
    await command(['scale', 'deployment/execution-worker', '--replicas=0']);
    await wait(async () => JSON.parse(await command(['get', 'pods', '-l', 'app=execution-worker', '-o', 'json'])).items.length === 0);
    const queued = await client.responses.create({ model: prefix, input: 'Remember Ada', background: true });
    assert.ok(['queued', 'in_progress'].includes(queued.status));
    await command(['rollout', 'restart', 'deployment/rank-api']);
    await command(['rollout', 'status', 'deployment/rank-api', '--timeout=60s']);
    await wait(() => api('/api/health', undefined, 'GET', 2000), { retryErrors: true });
    await command(['scale', 'deployment/execution-worker', '--replicas=2']);
    await command(['rollout', 'status', 'deployment/execution-worker', '--timeout=60s']);
    const completed = await wait(async () => {
      const value = await client.responses.retrieve(queued.id);
      if (['failed', 'cancelled'].includes(value.status)) throw new Error(JSON.stringify(value.error));
      return value.status === 'completed' && value;
    });
    assert.equal(textOf(completed), 'Ada');
    const resumed = await api(`/api/responses/${queued.id}?tenantId=local`);
    const owned = await (await executionRequest(`/v1/executions/${resumed.response.executionId}`)).json();
    assert.equal(owned.attempt, 1);
    report.recoveredQueuedResponse = { responseId: queued.id, executionId: owned.id, attempt: owned.attempt };
    const retained = await (await executionRequest(`/v1/executions/${report.executionId}/artifacts?path=${encodeURIComponent(stdout.path)}`)).json();
    assert.equal(retained.content, artifact.content);
    report.checks.push('Worker scale-to-zero queues accepted work; Rank restart and scale-up complete the same request once, retaining old S3 evidence');
  } finally {
    await command(['scale', 'deployment/execution-worker', '--replicas=2']);
    await command(['rollout', 'status', 'deployment/execution-worker', '--timeout=60s']);
  }
  report.images = JSON.parse(await readFile(join(state, 'platform-images.json'), 'utf8'));
  report.completedAt = new Date().toISOString();
  const path = join(state, 'platform-verification-' + Date.now() + '.json');
  await writeFile(path, JSON.stringify(report, null, 2) + '\n');
  console.log('PASS:', report.checks.join('\nPASS: '));
  console.log('Report:', path);
} finally {
  for (const process of [forward, executionForward]) {
    process.kill('SIGTERM');
    await new Promise(resolve => { if (process.exitCode !== null) resolve(); else process.once('exit', resolve); });
  }
}
