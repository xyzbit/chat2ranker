/** Launch packaged local services and verify independent operator/public credentials. */
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { randomBytes, createHash } from 'node:crypto';
import { once } from 'node:events';
import { mkdtemp, readFile, writeFile, rename, rm } from 'node:fs/promises';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { resolve, join } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';
import { verifyAccess } from './scenario.mjs';

const root = resolve(import.meta.dirname, '../../..');
const temp = await mkdtemp(join(tmpdir(), 'rank-access-'));
const children = [];
const token = randomBytes(32).toString('hex'), nextToken = randomBytes(32).toString('hex');
const env = { ...process.env, GOTOOLCHAIN: 'go1.26.0', RANK_ACTION_SECRET: randomBytes(32).toString('hex'), RANK_DATABASE_URL: '', RANK_ACCESS_FILE: '', RANK_MANAGEMENT_ORIGIN: '', EXECUTION_DATABASE_URL: '', EXECUTION_SERVICE_TOKEN: randomBytes(32).toString('hex'), RANK_CONTROL_TOKEN: randomBytes(32).toString('hex') };
const accessFile = join(temp, 'access.json');
const replace = async (tokens) => {
  await writeFile(accessFile + '.next', JSON.stringify({ version: 1, operators: tokens.map(value => ({ subject: 'alice', tokenSha256: createHash('sha256').update(value).digest('hex') })) }), { mode: 0o600 });
  await rename(accessFile + '.next', accessFile);
};
async function port() {
  const server = createServer(); server.listen(0, '127.0.0.1'); await once(server, 'listening');
  const value = server.address().port; await new Promise(done => server.close(done)); return value;
}
async function build(name, module) {
  const child = spawn('go', ['build', '-o', join(temp, name), './cmd/' + name], { cwd: join(root, module, 'backend'), env, stdio: ['ignore', 'ignore', 'pipe'] });
  let output = ''; child.stderr.on('data', chunk => { output += chunk; });
  const [code] = await once(child, 'exit'); assert.equal(code, 0, output);
}
function launch(name, args, extraEnv = {}, timeout) {
  const child = spawn(join(temp, name), args, { cwd: root, env: { ...env, ...extraEnv }, timeout, stdio: 'ignore' });
  children.push({ child, exited: once(child, 'exit') }); return child;
}
async function ready(url) {
  for (let attempt = 0; attempt < 100; attempt++) {
    try { if ((await fetch(url, { signal: AbortSignal.timeout(500) })).ok) return; } catch { /* Startup has not bound the listener yet. */ }
    await delay(100);
  }
  throw new Error('Packaged service did not become ready');
}
try {
  for (const [name, module] of [['rankd', 'rank'], ['executiond', 'execution'], ['execution-worker', 'execution']]) await build(name, module);
  await replace([token]);
  const rejected = async (args, extraEnv = {}) => {
    const child = launch('rankd', args, extraEnv, 5000);
    const [code, signal] = await once(child, 'exit');
    assert.ok(Number.isInteger(code) && code !== 0 && !signal, 'Invalid configuration must fail before binding a listener');
  };
  await rejected(['-addr=0.0.0.0:0']);
  for (const args of [['-shutdown-timeout=0'], ['-shutdown-timeout=-1s'], ['-event-poll-interval=0'], ['-mode=invalid'], ['-mode=api'], ['-mode=dispatcher']]) await rejected(args);
  for (const value of ['', 'rank-local-control-token']) await rejected(['-addr=127.0.0.1:0', '-access-file', accessFile], { RANK_CONTROL_TOKEN: value });
  for (const origin of ['ftp://example.test', 'https://example.test?', 'https://example.test#']) {
    await rejected(['-addr=127.0.0.1:0', '-access-file', accessFile, '-management-origin', origin]);
  }
  const executionPort = await port(), rankPort = await port();
  launch('executiond', ['-addr', `127.0.0.1:${executionPort}`, '-db', join(temp, 'execution.db'), '-worker', join(temp, 'execution-worker'), '-repo-root', root, '-artifacts', join(temp, 'artifacts'), '-sandboxes', join(temp, 'sandboxes'), '-credentials', join(temp, 'credentials')]);
  await ready(`http://127.0.0.1:${executionPort}/v1/health`);
  const base = `http://127.0.0.1:${rankPort}`;
  launch('rankd', ['-addr', `127.0.0.1:${rankPort}`, '-db', join(temp, 'rank.db'), '-access-file', accessFile, '-execution-url', `http://127.0.0.1:${executionPort}`, '-webhook-secret-dir', join(temp, 'webhooks')]);
  await ready(base + '/api/health');
  const request = async (method, path, body, authorization = '') => {
    const result = await fetch(base + path, { method, headers: { authorization, 'content-type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(40000) });
    return { status: result.status, body: result.status === 204 ? null : await result.json() };
  };
  const snapshot = await verifyAccess(request, { operator: 'Bearer ' + token, subject: 'alice', prefix: 'access' });
  const browser = async (method, path, body, cookie = '') => fetch(base + path, { method, headers: { origin: base, cookie, 'content-type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) });
  assert.equal((await browser('POST', '/api/auth/login', { username: 'alice', password: 'wrong' })).status, 401);
  const login = await browser('POST', '/api/auth/login', { username: 'alice', password: token });
  assert.equal(login.status, 200);
  assert.equal(login.headers.get('www-authenticate'), null);
  const cookie = login.headers.get('set-cookie').split(';')[0];
  assert.match(login.headers.get('set-cookie'), /HttpOnly/);
  assert.match(login.headers.get('set-cookie'), /SameSite=Strict/);
  assert.equal((await (await browser('GET', '/api/identity', undefined, cookie)).json()).subject, 'alice');
  assert.equal((await browser('GET', '/api/bootstrap', undefined, cookie)).status, 200);
  assert.equal((await browser('POST', '/api/experiments', { title: 'Browser session' }, cookie)).status, 201);
  assert.equal((await browser('POST', '/v1/responses', {}, cookie)).status, 401);
  assert.equal((await browser('POST', '/api/auth/logout', {}, cookie)).status, 204);
  assert.equal((await browser('GET', '/api/identity', undefined, cookie)).status, 401);
  const nextLogin = await browser('POST', '/api/auth/login', { username: 'alice', password: token });
  const revokedCookie = nextLogin.headers.get('set-cookie').split(';')[0];
  Object.assign(snapshot, { browserLogin: true, cookieManagementMutation: true, cookieCannotServe: true, logoutInvalidatesSession: true });
  await replace([token, nextToken]);
  for (const value of [token, nextToken]) assert.equal((await request('GET', '/api/identity', undefined, 'Bearer ' + value)).status, 200);
  await replace([nextToken]);
  assert.equal((await request('GET', '/api/identity', undefined, 'Bearer ' + token)).status, 401);
  assert.equal((await request('GET', '/api/identity', undefined, 'Bearer ' + nextToken)).body.subject, 'alice');
  assert.equal((await browser('GET', '/api/identity', undefined, revokedCookie)).status, 401);
  snapshot.credentialRotationRevokesBrowserSession = true;
  await writeFile(accessFile, '{broken', { mode: 0o600 });
  assert.equal((await request('GET', '/api/identity', undefined, 'Bearer ' + nextToken)).status, 503);
  Object.assign(snapshot, { invalidLifecycleConfigurationRefused: true, unprotectedNonLoopbackRefused: true, developmentSecretsRefused: true, operatorRotation: true, invalidCredentialFileFailsClosed: true });
  assert.deepEqual(snapshot, JSON.parse(await readFile(new URL('./snapshot.json', import.meta.url), 'utf8')));
  console.log('Access-control packaged snapshot passed.');
} finally {
  for (const { child, exited } of children.reverse()) { if (child.exitCode === null) child.kill('SIGTERM'); await exited; }
  await rm(temp, { recursive: true, force: true });
}
