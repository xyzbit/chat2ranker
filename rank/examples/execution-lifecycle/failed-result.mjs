/** Verify known worker failure, retained evidence and idempotent replay through the public API. */
import assert from 'node:assert/strict';
import { setTimeout as delay } from 'node:timers/promises';

export async function verifyFailedResult(request, metadata = {}, key = 'failed-result') {
  const input = { idempotencyKey: key, spec: { protocolVersion: 2, kind: 'agent', harness: 'mock', prompt: 'failure evidence', metadata,
    structuredOutput: { name: 'required-status', schema: { type: 'object', properties: { status: { const: 'required' } }, required: ['status'] } } } };
  const accepted = await request('/v1/executions', input);
  const deadline = Date.now() + 55_000;
  let failed;
  while (Date.now() < deadline) {
    const value = await request(`/v1/executions/${accepted.id}`);
    if (['failed', 'completed', 'cancelled'].includes(value.status)) { failed = value; break; }
    await delay(50);
  }
  assert.equal(failed?.status, 'failed', failed?.error);
  assert.match(failed.error, /^structured output does not match schema:/);
  assert.equal(failed.result.output, '{"status":"ok"}');
  assert.deepEqual(failed.result.usage, { inputTokens: 70, outputTokens: 30 });
  assert.equal(failed.result.costKnown, true);
  const ref = failed.result.artifacts.find(item => item.kind === 'result');
  assert.ok(ref);
  const artifact = await request(`/v1/executions/${failed.id}/artifacts?path=${encodeURIComponent(ref.path)}`);
  const response = JSON.parse(artifact.content);
  assert.equal(response.executionId, failed.id);
  assert.equal(response.status, 'failed');
  assert.equal(response.error, failed.error);
  assert.equal(response.result.output, failed.result.output);
  const replay = await request('/v1/executions', input);
  assert.equal(replay.id, failed.id);
  assert.equal(replay.status, 'failed');
  assert.equal(replay.attempt, 1);
  assert.equal(Boolean(replay.runtimeCleanupPending), false);
  return { status: failed.status, output: failed.result.output, usage: failed.result.usage, costKnown: failed.result.costKnown,
    schemaFailureIdentified: true, failureArtifactReadable: true, replayAttempt: replay.attempt, cleanupPending: false };
}
