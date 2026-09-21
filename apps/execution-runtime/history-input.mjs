/**
 * Validate the private Execution history file before importing model-visible messages.
 * @param {unknown} input - Parsed JSON received from the Execution worker.
 * @returns {object} Validated input with ordered messages.
 */
export function parseHistory(input) {
  if (!input || typeof input !== 'object' || typeof input.executionId !== 'string' || !input.executionId.trim() || !Array.isArray(input.messages) || !input.messages.length) throw new Error('Execution history is required');
  const pending = new Set(), used = new Set();
  for (const message of input.messages) {
    if (!message || typeof message !== 'object' || (message.content !== undefined && typeof message.content !== 'string')) throw new Error('Invalid history message');
    const hasText = typeof message.content === 'string' && message.content.trim() !== '';
    const calls = message.toolCalls ?? [];
    if (!Array.isArray(calls)) throw new Error('Invalid historical tool calls');
    if (message.role !== 'tool' && pending.size) throw new Error('Pending tool calls require consecutive results');
    switch (message.role) {
      case 'user':
        if (!hasText || calls.length || message.toolCallId) throw new Error('Invalid historical user message');
        break;
      case 'assistant':
        if ((!hasText && !calls.length) || message.toolCallId) throw new Error('Invalid historical assistant message');
        for (const call of calls) {
          if (!call || typeof call.id !== 'string' || !call.id.trim() || used.has(call.id) || typeof call.name !== 'string' || !call.name.trim() || !call.arguments || Array.isArray(call.arguments) || typeof call.arguments !== 'object') throw new Error('Invalid or repeated historical tool call');
          used.add(call.id); pending.add(call.id);
        }
        break;
      case 'tool':
        if (!hasText || calls.length || !pending.delete(message.toolCallId)) throw new Error('Unmatched historical tool result');
        break;
      default:
        throw new Error('Unsupported history role');
    }
  }
  if (pending.size || input.messages.at(-1).role === 'assistant') throw new Error('History must end in user or tool input without pending calls');
  return input;
}
