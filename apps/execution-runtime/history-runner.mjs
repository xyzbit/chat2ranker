/** Drive one DSH execution from a validated portable conversation history. */
import { parseHistory } from './history-input.mjs';
import { readFile } from 'node:fs/promises';
import z from '@deepseek-ai/schemastery';
import { installModelSelection } from '@deepseek-ai/dsh-agent';
import { CallId, createMessage, createToolResultMessage, createUserMessage } from '@deepseek-ai/dsh-llm';
import { Session, SessionId } from '@deepseek-ai/dsh-session';

/** Stable plugin identifier. */
export const name = 'rank-history-runner';
/** Required services for the one-shot driver. */
export const inject = ['agentDefaultModel', 'agents', 'sessions'];
/** Location of the invocation-private history file. */
export const Config = z.object({ inputPath: z.string().required() });

/** Convert portable conversation messages without inventing native model calls. */
function messageOf(message) {
  const content = message.content ? [{ type: 'text', text: message.content }] : [];
  switch (message.role) {
    case 'user':
      return createUserMessage({ source: { kind: 'user' }, content });
    case 'assistant':
      for (const call of message.toolCalls || []) {
        content.push({ type: 'tool-call', id: CallId(call.id), name: call.name, arguments: JSON.stringify(call.arguments) });
      }
      return createMessage({ role: 'assistant', source: { kind: 'plugin', plugin: name, form: 'recall' }, content });
    case 'tool':
      return createToolResultMessage({ callId: CallId(message.toolCallId), content, isError: false });
    default:
      throw new Error('Unsupported history role');
  }
}

async function run(ctx, inputPath, exit) {
  const input = parseHistory(JSON.parse(await readFile(inputPath, 'utf8')));
  const messages = input.messages.map(messageOf);
  const last = messages.at(-1);
  if (last.role !== 'user') throw new Error('History must end in user or tool input');
  await ctx.get('loader')?.await();
  const agents = ctx.get('agents'), sessions = ctx.get('sessions'), defaults = ctx.get('agentDefaultModel');
  // Shutdown can dispose services while loader settlement is pending.
  if (!agents || !sessions || !defaults) return;
  const sessionId = SessionId(input.executionId);
  const imported = Session.create(sessionId);
  for (const message of messages.slice(0, -1)) imported.append('history/message', { message }, { surfaceOp: 'append' });
  const selection = defaults.currentSelection();
  const { agent } = await agents.create({
    sessionId, seed: imported.events, meta: { cwd: process.cwd(), seedLength: imported.events.length },
    agentOptions: { provider: selection.provider, model: selection.model },
    setup: agentCtx => {
      installModelSelection(agentCtx, { current: selection, assembled: undefined });
      if (last.source.kind === 'tool') {
        let entered = false;
        agentCtx.on('agent/pre-step', async (_payload, next) => {
          const decision = await next();
          if (entered || decision.kind !== 'enter') return decision;
          entered = true;
          const result = decision.messages.find(message => message.id === last.id);
          if (!result) throw new Error('The historical tool result was removed before model entry');
          return { ...decision, messages: [result, ...decision.messages.filter(message => message.id !== last.id)] };
        });
      }
    },
  });
  await agent.whenIdle();
  const firstSeq = agent.session.seq;
  agent.followup(last);
  await agent.whenIdle();
  await sessions.flush(agent.session);
  const events = agent.session.events.filter(event => event.seq >= firstSeq);
  const output = events.findLast(event => event.type === 'assistant/message');
  const outcome = events.findLast(event => event.type === 'turn/end')?.data.reason;
  process.stdout.write((output?.data.message.content.filter(block => block.type === 'text').map(block => block.text).join('') || '') + '\n');
  if (outcome?.kind === 'error') process.stderr.write(outcome.error.message + '\n');
  exit(outcome?.kind === 'completed' ? 0 : 1);
}

/** Mount a one-shot driver; process shutdown owns agent and log disposal. */
export function apply(ctx, config) {
  const exit = ctx.get('appExit');
  if (!exit) throw new Error('Rank history runner requires the DSH launcher');
  void run(ctx, config.inputPath, exit).catch(error => { process.stderr.write(String(error) + '\n'); exit(1); });
}
