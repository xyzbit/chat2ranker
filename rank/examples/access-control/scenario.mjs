/** Exercise operator and public-credential authorization through actual Rank HTTP routes. */
import assert from 'node:assert/strict';

/**
 * Run tenant, endpoint, scope and revocation checks without emitting credentials.
 * @param request HTTP caller returning status and parsed response body.
 * @param options Operator authorization, expected subject and unique fixture prefix.
 * @returns Authorization facts suitable for a credential-free snapshot.
 */
export async function verifyAccess(request, { operator, subject, prefix }) {
  const call = async (method, path, body, auth = operator, expected = 200) => {
    const result = await request(method, path, body, auth);
    assert.equal(result.status, expected, `${method} ${path}: unexpected authorization or execution status`);
    return result.body;
  };
  await call('GET', '/api/bootstrap', undefined, '', 401);
  assert.equal((await call('GET', '/api/identity')).subject, subject);
  await call('POST', '/v1/responses', {}, operator, 401);
  const scopes = ['responses:create', 'responses:read', 'responses:cancel', 'responses:approve'];
  const endpoint = async (tenantId, suffix) => {
    const deployment = await call('POST', '/api/deployments', { tenantId, name: `${prefix}-${suffix}`, agentFamilyId: 'agent-research-demo', policy: { requestsPerMinute: 100, maxConcurrentResponses: 4, executionTimeoutMs: 30000 } }, operator, 201);
    const release = await call('POST', `/api/deployments/${deployment.id}/aliases/production/releases`, { agentVersionId: 'agent-research-demo-v1', actor: 'forged-actor', policyResult: 'manual_override', reason: 'Deterministic authorization acceptance' }, operator, 201);
    assert.equal(release.actor, subject);
    const credential = await call('POST', '/api/serving-credentials', { tenantId, name: suffix, deploymentIds: [deployment.id], scopes }, operator, 201);
    return { deployment, credential, auth: 'Bearer ' + credential.apiKey };
  };
  const a = await endpoint(prefix + '-tenant-a', 'a');
  const b = await endpoint(prefix + '-tenant-b', 'b');
  const c = await endpoint(prefix + '-tenant-a', 'c');
  await call('POST', '/api/serving-credentials', { tenantId: b.deployment.tenantId, name: 'cross-tenant', deploymentIds: [a.deployment.id], scopes }, operator, 400);
  await call('GET', '/api/bootstrap', undefined, a.auth, 401);
  const response = await call('POST', '/v1/responses', { model: a.deployment.name, input: 'Authorization acceptance' }, a.auth);
  assert.equal(response.status, 'completed');
  for (const other of [b, c]) {
    await call('GET', '/v1/responses/' + response.id, undefined, other.auth, 404);
    await call('GET', '/v1/responses/' + response.id + '?stream=true', undefined, other.auth, 404);
    await call('POST', '/v1/responses/' + response.id + '/cancel', {}, other.auth, 404);
    await call('POST', '/v1/responses/' + response.id + '/approval', { approved: true }, other.auth, 404);
    await call('POST', '/v1/responses', { model: other.deployment.name, previous_response_id: response.id, input: 'Cross-boundary continuation' }, other.auth, 404);
  }
  await call('POST', '/v1/responses', { model: a.deployment.name, input: 'Wrong tenant' }, b.auth, 404);
  const createOnly = await call('POST', '/api/serving-credentials', { tenantId: a.deployment.tenantId, name: 'create-only', deploymentIds: [a.deployment.id], scopes: ['responses:create'] }, operator, 201);
  await call('GET', '/v1/responses/' + response.id, undefined, 'Bearer ' + createOnly.apiKey, 403);
  await call('POST', `/api/serving-credentials/${a.credential.id}/revoke?tenantId=${encodeURIComponent(a.deployment.tenantId)}`, {}, operator, 200);
  await call('GET', '/v1/responses/' + response.id, undefined, a.auth, 401);
  const replacement = await call('POST', '/api/serving-credentials', { tenantId: a.deployment.tenantId, name: 'replacement', deploymentIds: [a.deployment.id], scopes }, operator, 201);
  assert.equal((await call('GET', '/v1/responses/' + response.id, undefined, 'Bearer ' + replacement.apiKey)).id, response.id);
  return { unauthenticatedManagement: 401, operatorIdentity: true, authoritativeReleaseActor: true, credentialsCannotCrossPlanes: true, crossTenantGrant: 400, crossTenantAndEndpointAccess: 404, missingScope: 403, revokedServingKey: 401, replacementRetainsHistory: true };
}
