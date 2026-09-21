/** Run actual DSH/SSE requests across rolling Rank API Pods; assert the keyless transcript. */
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { readFile, writeFile } from 'node:fs/promises';
import { createServer } from 'node:net';
import { homedir } from 'node:os';
import { join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { createClient, textOf } from '../responses-client/demo-client.mjs';

const state = join(homedir(), '.local/state/agentkiln/rank-k8s');
const secrets = JSON.parse(await readFile(join(state, 'platform-lab.json')));
const executionSecrets = JSON.parse(await readFile(join(state, 'execution-lab.json')));
const images = JSON.parse(await readFile(join(state, 'platform-images.json')));
const prefix = 'rolling-' + Date.now();
const kube = ['--kubeconfig=' + join(state, 'config'), '--context=rank-dev', '-n', 'rank-dev'];
const children = [];
const command = (name, args) => new Promise((resolve, reject) => {
  const child = spawn(name, args, { stdio: ['ignore', 'pipe', 'pipe'] }); let output = '';
  child.stdout.on('data', chunk => { output += chunk; }); child.stderr.on('data', chunk => { output += chunk; });
  child.on('error', reject); child.on('exit', code => code === 0 ? resolve(output.trim()) : reject(new Error(name + ' failed: ' + output)));
});
const kubectl = args => command('kubectl', [...kube, ...args]);
const wait = async (check, timeout = 120_000) => {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) { const value = await check(); if (value) return value; await delay(100); }
  throw new Error('Rolling acceptance condition timed out');
};
async function port() {
  const server = createServer().listen(0, '127.0.0.1'); await once(server, 'listening');
  const value = server.address().port; await new Promise(resolve => server.close(resolve)); return value;
}
async function forward(service, target) {
  const local = await port();
  const child = spawn('kubectl', [...kube, 'port-forward', '--address=127.0.0.1', 'service/' + service, local + ':' + target], { stdio: 'ignore' });
  children.push({ child, exited: once(child, 'exit') });
  const base = 'http://127.0.0.1:' + local;
  await wait(async () => { try { return (await fetch(base + (service === 'rank-gateway' ? '/health' : '/v1/health'))).ok; } catch { return false; } }, 20_000);
  return base;
}
let base, executionBase, fixtureCreated = false, credential, connection, cookie, client;
const report = { startedAt: new Date().toISOString(), scenarios: [], checks: {}, images };
const basic = 'Basic ' + Buffer.from('rank:' + secrets.GATEWAY_PASSWORD).toString('base64');
async function request(path, body, method = body === undefined ? 'GET' : 'POST', headers = {}) {
  const response = await fetch(base + path, { method, headers: { authorization: basic, 'content-type': 'application/json', ...headers }, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(60_000) });
  const value = response.status === 204 ? null : await response.json();
  assert.ok(response.ok, path + ': ' + response.status + ' ' + JSON.stringify(value)); return value;
}
async function identity() {
  const response = await fetch(base + '/api/identity', { headers: { cookie }, signal: AbortSignal.timeout(3000) });
  await response.body.cancel(); return response.status;
}
async function execution(id) {
  const response = await fetch(executionBase + '/v1/executions/' + id, { headers: { authorization: 'Bearer ' + executionSecrets.EXECUTION_SERVICE_TOKEN } });
  assert.ok(response.ok); return response.json();
}
try {
  base = await forward('rank-gateway', 8080); executionBase = await forward('execution-api', 8790);
  const configuration = JSON.parse(await kubectl(['get', 'deployment/rank-api', '-o', 'json']));
  assert.equal(configuration.spec.strategy.type, 'RollingUpdate');
  assert.equal(configuration.spec.strategy.rollingUpdate.maxUnavailable, 0);
  assert.equal(configuration.spec.replicas, 1);
  const dispatcherBefore = JSON.parse(await kubectl(['get', 'pods', '-l', 'app=rank-dispatcher', '-o', 'json']));
  assert.equal(dispatcherBefore.items.length, 1);
  report.dispatcherPod = dispatcherBefore.items[0].metadata.uid;
  await command('docker', ['run', '-d', '--name', prefix, '--network', 'rank-dev', '-p', '127.0.0.1::8800', '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges', '--mount', 'type=bind,src=' + join(import.meta.dirname, 'model.mjs') + ',dst=/tmp/model.mjs,readonly', '--entrypoint', 'node', images.images.platform, '/tmp/model.mjs']); fixtureCreated = true;
  const inspected = JSON.parse(await command('docker', ['inspect', prefix]));
  const ip = inspected[0].NetworkSettings.Networks['rank-dev'].IPAddress;
  const factsURL = 'http://127.0.0.1:' + inspected[0].NetworkSettings.Ports['8800/tcp'][0].HostPort + '/facts';
  const facts = async () => { const response = await fetch(factsURL); assert.ok(response.ok); return response.json(); };
  await wait(async () => { try { await facts(); return true; } catch { return false; } }, 10_000);
  connection = await request('/api/model-connections', { name: prefix, provider: 'custom', protocol: 'openai-chat-completions', baseUrl: 'http://' + ip + ':8800/v1', apiKey: 'local-rolling-fixture', defaultModel: 'rank-rolling-model' });
  await request('/api/model-connections/' + connection.id + '/verify', {});
  const agent = await request('/api/agents', { name: prefix, runnerType: 'dsh', modelConnectionId: connection.id, model: 'rank-rolling-model', systemPrompt: 'Return the model answer without tool calls.' });
  const deployment = await request('/api/deployments', { tenantId: 'local', name: prefix, agentFamilyId: agent.familyId, policy: { requestsPerMinute: 500, maxConcurrentResponses: 4, executionTimeoutMs: 120_000 } });
  const dataset = await request('/api/datasets', { name: prefix, cases: [{ id: 'ready', title: 'Fixture responds', input: 'Reply READY', expected: { outputContains: 'READY' } }] });
  await request('/api/agent-families/' + agent.familyId + '/evaluation-defaults', { regressionDatasetId: dataset.familyId, expectedRevision: 0 }, 'PUT');
  const draft = await request('/api/deployments/' + deployment.id + '/release-draft', { alias: 'production', agentVersionId: agent.id });
  const task = await request('/api/deployments/' + deployment.id + '/release-tasks', { alias: 'production', agentVersionId: agent.id, developmentDatasetVersionId: draft.developmentDatasetVersionId, regressionDatasetVersionId: draft.regressionDatasetVersionId, expectedReleaseRevisionId: draft.expectedReleaseRevisionId, expectedPolicyRevision: draft.policy.revision, expectedCandidateRunId: draft.candidateEvidence?.runId ?? '', expectedCurrentRunId: draft.currentEvidence?.runId ?? '', trialCount: 1, actor: 'rolling-acceptance' }, 'POST', { 'idempotency-key': crypto.randomUUID() });
  const release = await wait(async () => { const value = await request('/api/release-tasks/' + task.id); return !['ready', 'regression'].includes(value.status) && value; });
  assert.equal(release.status, 'published'); report.releaseTask = release.id;

  credential = await request('/api/serving-credentials', { tenantId: 'local', name: prefix, deploymentIds: [deployment.id], scopes: ['responses:create', 'responses:read', 'responses:cancel'] });
  client = createClient({ RANK_BASE_URL: base + '/v1', RANK_API_KEY: credential.apiKey, RANK_MODEL: prefix }); client.maxRetries = 0;
  const login = await fetch(base + '/api/auth/login', { method: 'POST', headers: { origin: base, 'content-type': 'application/json' }, body: JSON.stringify({ username: 'rank', password: secrets.GATEWAY_PASSWORD }) });
  assert.equal(login.status, 200); cookie = login.headers.get('set-cookie').split(';')[0]; await login.body.cancel();
  const first = await client.responses.create({ model: prefix, input: 'READY' }); assert.equal(first.status, 'completed');
  for (const label of ['slow', 'long']) {
    await wait(async () => { const pods = JSON.parse(await kubectl(['get', 'pods', '-l', 'app=rank-api', '-o', 'json'])); return pods.items.length === 1 && !pods.items[0].metadata.deletionTimestamp; });
    const before = JSON.parse(await kubectl(['get', 'pods', '-l', 'app=rank-api', '-o', 'json'])).items[0];
    const scenario = { label, oldPod: before.metadata.uid, startedAt: Date.now(), reads: 0, httpStatuses: {}, responseId: null, streamCompleted: false, disconnected: false }; report.scenarios.push(scenario);
    const stream = await client.responses.create({ model: prefix, input: 'ROLLING-' + label.toUpperCase(), stream: true });
    const reading = (async () => {
      try {
        for await (const event of stream) {
          if (event.response?.id) scenario.responseId = event.response.id;
          if (event.type === 'response.completed') { scenario.streamCompleted = true; scenario.text = textOf(event.response); }
          if (['response.failed', 'response.incomplete', 'response.cancelled', 'error'].includes(event.type)) throw new Error('Unexpected terminal event: ' + event.type);
        }
      } catch (error) { scenario.disconnected = true; scenario.streamError = error.name; }
      scenario.streamEndedAt = Date.now();
      if (!scenario.streamCompleted) scenario.disconnected = true;
    })();
    await wait(async () => scenario.responseId && (await facts()).some(call => call.label === label));
    scenario.modelStartedAt = (await facts()).find(call => call.label === label).startedAt;
    let monitoring = true;
    const monitor = (async () => {
      while (monitoring) {
        for (const [path, headers] of [['/api/identity', { cookie }], ['/v1/responses/' + first.id, { authorization: 'Bearer ' + credential.apiKey }]]) {
          const response = await fetch(base + path, { headers, signal: AbortSignal.timeout(3000) });
          scenario.httpStatuses[response.status] = (scenario.httpStatuses[response.status] || 0) + 1; scenario.reads++; await response.body.cancel();
        }
        await delay(50);
      }
    })().catch(error => { scenario.monitorError = error.message; monitoring = false; });
    try {
      await kubectl(['rollout', 'restart', 'deployment/rank-api']);
      await kubectl(['rollout', 'status', 'deployment/rank-api', '--timeout=90s']);
      await reading;
      await delay(1000);
    } finally { monitoring = false; await monitor; }
    assert.equal(scenario.monitorError, undefined); assert.deepEqual(Object.keys(scenario.httpStatuses), ['200']); assert.ok(scenario.reads > 20);
    if (label === 'slow') { assert.equal(scenario.streamCompleted, true); assert.equal(scenario.disconnected, false); }
    else { assert.equal(scenario.streamCompleted, false); assert.equal(scenario.disconnected, true); assert.ok(scenario.streamEndedAt - scenario.modelStartedAt < 45_000, 'API failed to bound the unfinished stream'); }
    const stored = await wait(async () => { const value = await client.responses.retrieve(scenario.responseId); assert.ok(!['failed', 'cancelled', 'incomplete'].includes(value.status), JSON.stringify(value.error)); return value.status === 'completed' && value; });
    assert.match(textOf(stored), new RegExp('ROLLING-' + label.toUpperCase() + '-DONE'));
    assert.equal(stored.deployment.agent_version_id, agent.id);
    assert.equal(await identity(), 200);
    const detail = await request('/api/responses/' + stored.id + '?tenantId=local');
    const run = await wait(async () => { const value = await execution(detail.response.executionId); return !value.runtimeCleanupPending && value; });
    assert.equal(run.attempt, 1); assert.equal(run.status, 'completed');
    assert.equal((await facts()).filter(call => call.label === label).length, 1);
    const replay = await fetch(base + '/v1/responses/' + stored.id + '?stream=true', { headers: { authorization: 'Bearer ' + credential.apiKey } });
    assert.equal(replay.status, 200); assert.match(await replay.text(), /response.completed/);
    scenario.executionId = run.id; scenario.attempt = run.attempt; scenario.storedStatus = stored.status;
    console.log(label + ': ' + scenario.reads + ' reads, zero HTTP errors; ' + (scenario.streamCompleted ? 'stream completed during drain' : 'bounded stream disconnect; durable execution completed once'));
  }
  const dispatcherAfter = JSON.parse(await kubectl(['get', 'pods', '-l', 'app=rank-dispatcher', '-o', 'json']));
  assert.equal(dispatcherAfter.items[0].metadata.uid, report.dispatcherPod);
  const logout = await fetch(base + '/api/auth/logout', { method: 'POST', headers: { cookie, origin: base } }); assert.equal(logout.status, 204); assert.equal(await identity(), 401);
  report.checks = { evaluatedReleasePublished: true, zeroGatewayErrors: true, slowStreamDrained: true, longStreamBounded: true, durableCompletionOnce: true, storedSSEReplay: true, frozenAgentVersion: true, sharedLoginSurvivesRollouts: true, logoutRevokesSharedSession: true, dispatcherUnchanged: true };
  assert.deepEqual(report.checks, JSON.parse(await readFile(new URL('./snapshot.json', import.meta.url))));
  report.passed = true;
} catch (error) { report.failure = error.message; throw error; }
finally {
  const cleanupErrors = [];
  if (credential) {
    for (const scenario of report.scenarios) {
      if (scenario.responseId && !scenario.storedStatus) { try { await client.responses.cancel(scenario.responseId); await wait(async () => { const response = await client.responses.retrieve(scenario.responseId); return ['completed', 'failed', 'cancelled'].includes(response.status); }, 90_000); } catch (error) { cleanupErrors.push('Cancel: ' + error.message); } }
    }
    try { await request('/api/serving-credentials/' + credential.id + '/revoke?tenantId=local', {}); } catch (error) { cleanupErrors.push('Revoke: ' + error.message); }
  }
  if (fixtureCreated) { try { await command('docker', ['rm', '-f', prefix]); } catch (error) { cleanupErrors.push(error.message); } }
  for (const { child, exited } of children) { child.kill('SIGTERM'); await exited; }
  report.cleanupErrors = cleanupErrors; report.completedAt = new Date().toISOString();
  const path = join(state, prefix + '.json'); await writeFile(path, JSON.stringify(report, null, 2) + '\n'); console.log('Rolling evidence: ' + path);
  assert.deepEqual(cleanupErrors, []);
}
