import assert from 'node:assert/strict';

/** Exercise model freezing through public configuration and conversation APIs. */
export async function verifyModelResolution({ baseURL, executionURL, request, connection, providerURL }) {
  const post = (path, value, headers = {}) => request(baseURL, path, { method: 'POST', headers, body: JSON.stringify(value) });
  const agent = await post('/api/agents', { name: 'Frozen default', runnerType: 'mock', modelConnectionId: connection.id });
  assert.equal(agent.model, 'catalog-a');
  const experiment = await post('/api/experiments', { title: 'Model resolution' });
  const headers = { 'x-rank-control-token': process.env.RANK_CONTROL_TOKEN || 'rank-local-control-token' };
  const command = (type, payload, key) => post('/api/internal/control/commands', {
    experimentId: experiment.id, controlSessionId: experiment.controlSessionId, idempotencyKey: key, type, payload,
  }, headers);
  const payload = { name: 'Conversation default', runnerType: 'mock', modelConnectionId: connection.id };
  const created = await command('create_agent', payload, 'create-frozen-default');
  const view = await request(baseURL, `/api/experiments/${experiment.id}`);
  assert.equal(view.agent.model, 'catalog-a');
  await request(baseURL, `/api/model-connections/${connection.id}`, {
    method: 'PATCH', body: JSON.stringify({ name: connection.name, provider: 'custom', protocol: connection.protocol, baseUrl: providerURL, defaultModel: 'account-latest' }),
  });
  const replay = await command('create_agent', payload, 'create-frozen-default');
  assert.equal(replay.command.id, created.command.id);
  const verified = await post(`/api/model-connections/${connection.id}/verify`, {});
  assert.equal(verified.defaultModel, 'account-latest');
  const next = await post(`/api/agent-families/${agent.familyId}/versions`, { runnerType: 'mock', modelConnectionId: connection.id });
  assert.equal(next.model, 'account-latest');
  await command('create_agent_version', { baseAgentVersionId: view.agent.id, model: '' }, 'refresh-default');
  const revised = await request(baseURL, `/api/experiments/${experiment.id}`);
  assert.equal(revised.agent.model, 'account-latest');
  const original = await request(baseURL, `/api/experiments/${experiment.id}`, { method: 'PATCH', body: JSON.stringify({ agentVersionId: agent.id }) });
  assert.equal(original.agent.model, 'catalog-a');
  const explicit = await post('/api/agents', { name: 'Custom model', runnerType: 'mock', modelConnectionId: connection.id, model: '  custom-model  ' });
  assert.equal(explicit.model, 'custom-model');
  await assert.rejects(command('create_agent', { ...payload, modelConnectionId: 'missing' }, 'missing-model-connection'), /模型连接不存在/);
  await assert.rejects(command('create_agent_version', { baseAgentVersionId: view.agent.id, runnerType: 'codex' }, 'wrong-protocol'), /Responses/);
  const input = { idempotencyKey: 'unresolved-model', spec: { protocolVersion: 2, kind: 'agent', harness: 'mock', prompt: 'test', modelConnectionId: connection.id } };
  const rejected = await fetch(`${executionURL}/v1/executions`, { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify(input) });
  assert.equal(rejected.status, 400);
  assert.equal((await rejected.json()).error.code, 'invalid_model_selection');
  await request(baseURL, `/api/model-connections/${connection.id}`, {
    method: 'PATCH', body: JSON.stringify({ name: connection.name, provider: 'custom', protocol: connection.protocol, baseUrl: providerURL, defaultModel: 'catalog-a' }),
  });
  await post(`/api/model-connections/${connection.id}/verify`, {});
  return { initialModel: agent.model, afterDefaultChange: next.model, conversationInitialModel: view.agent.model, conversationNewVersionModel: revised.agent.model, explicitModel: explicit.model, replayKeepsOriginal: replay.command.id === created.command.id, unresolvedExecutionStatus: rejected.status };
}
