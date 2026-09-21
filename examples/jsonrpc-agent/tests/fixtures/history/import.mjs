/** Import an external conversation when the first SDK input enters an idle inbox. */
import { createMessage, createUserMessage } from '@deepseek-ai/dsh-llm';
export const name = 'sdk-history-fixture';
/** Import after the enqueue receipt and before native turn admission. */
export function apply(ctx) {
  const imported = new WeakSet();
  ctx.on('agent/inbox/inserted', ({ agent }) => {
    if (imported.has(agent)) return;
    imported.add(agent);
    for (const message of [
      createUserMessage({ source: { kind: 'user' }, content: [{ type: 'text', text: 'Remember Ada' }] }),
      createMessage({ role: 'assistant', source: { kind: 'plugin', plugin: name, form: 'recall' }, content: [{ type: 'text', text: 'Remembered Ada' }] }),
    ]) agent.session.append('history/message', { message }, { surfaceOp: 'append' });
  });
}
