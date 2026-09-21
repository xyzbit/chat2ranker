/** Verify browser-cookie admission through the installed gateway without printing credentials. */
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { homedir } from 'node:os';
import { join } from 'node:path';

const base = process.env.RANK_LOGIN_TEST_URL || 'http://127.0.0.1:18780';
const privateState = JSON.parse(await readFile(join(homedir(), '.local/state/agentkiln/rank-k8s/platform-lab.json'), 'utf8'));
const call = (method, path, body, cookie = '', origin = base) => fetch(base + path, {
  method, headers: { origin, cookie, 'content-type': 'application/json' },
  body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(30000),
});
const shell = await call('GET', '/');
assert.equal(shell.status, 200);
assert.equal(shell.headers.get('www-authenticate'), null);
assert.match(await shell.text(), /<div id="root">/);
assert.equal((await call('GET', '/api/bootstrap')).status, 401);
assert.equal((await call('POST', '/api/auth/login', { username: 'rank', password: 'incorrect' })).status, 401);
const login = await call('POST', '/api/auth/login', { username: 'rank', password: privateState.GATEWAY_PASSWORD });
assert.equal(login.status, 200);
assert.equal((await login.json()).subject, 'rank');
const cookie = login.headers.get('set-cookie').split(';')[0];
assert.match(login.headers.get('set-cookie'), /HttpOnly/);
assert.match(login.headers.get('set-cookie'), /SameSite=Strict/);
try {
  assert.equal((await call('GET', '/api/identity', undefined, cookie)).status, 200);
  assert.equal((await call('GET', '/api/bootstrap', undefined, cookie)).status, 200);
  assert.equal((await call('POST', '/api/experiments', {}, cookie, 'http://other.test')).status, 403);
  assert.equal((await call('POST', '/v1/responses', {}, cookie)).status, 401);
  const created = await call('POST', '/api/experiments', { title: 'Browser login acceptance' }, cookie);
  assert.equal(created.status, 201);
  const experiment = await created.json();
  const id = experiment.id || experiment.experiment?.id;
  assert.ok(id);
  // Empty input proves the authenticated control route without generating a model call.
  const control = `/control/v1/experiments/${id}/messages`;
  assert.equal((await call('POST', control, { content: '' }, cookie)).status, 400);
  assert.equal((await call('POST', control, { content: '' }, cookie, 'http://other.test')).status, 403);
  assert.equal((await call('POST', control, { content: '' })).status, 401);
} finally {
  assert.equal((await call('POST', '/api/auth/logout', {}, cookie)).status, 204);
}
assert.equal((await call('GET', '/api/identity', undefined, cookie)).status, 401);
console.log('Gateway browser-login checks passed: public shell, invalid password, cookie identity/read/write, CSRF, control-host forwarding, public-key isolation and server-side logout.');
