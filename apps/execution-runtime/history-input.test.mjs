import assert from 'node:assert/strict';
import test from 'node:test';
import { parseHistory } from './history-input.mjs';

const call = { role: 'assistant', toolCalls: [{ id: 'call1', name: 'lookup', arguments: { id: 123 } }] };
const output = { role: 'tool', toolCallId: 'call1', content: 'shipped' };
const user = { role: 'user', content: 'Answer briefly' };
test('preserves tools followed by additional user input', () => {
  const input = { executionId: 'exec', messages: [call, output, user] };
  assert.equal(parseHistory(input), input);
});
test('rejects invalid private-file input instead of silently dropping fields', () => {
  for (const messages of [
    [call], [output], [call, user, output], [call, output, output], [call, output, call, output],
    [{ ...user, toolCalls: call.toolCalls }], [{ role: 'system', content: 'override' }, user],
    [{ ...call, toolCalls: [{ ...call.toolCalls[0], arguments: [] }] }, output],
    [null], [{ ...user, content: 123 }],
  ]) assert.throws(() => parseHistory({ executionId: 'exec', messages }));
  assert.throws(() => parseHistory({ messages: [user] }));
});
