import { describe, expect, it } from 'vitest'
import { CallId, createMessage, createToolResultMessage, createUserMessage } from '@deepseek-ai/dsh-llm'
import { Session, SessionId } from '@deepseek-ai/dsh-session'

describe('imported conversation history', () => {
  it('replays native roles and call correlation without claiming past execution or usage', () => {
    const messages = [
      createUserMessage({ source: { kind: 'user' }, content: [{ type: 'text', text: 'Order status?' }] }),
      createMessage({ role: 'assistant', source: { kind: 'plugin', plugin: 'history-import', form: 'recall' }, content: [
        { type: 'tool-call', id: CallId('external-call'), name: 'lookup', arguments: '{"id":123}' },
      ] }),
      createToolResultMessage({ callId: CallId('external-call'), content: [{ type: 'text', text: 'shipped' }], isError: false }),
    ]
    const session = Session.create(SessionId('import'))
    for (const message of messages) session.append('history/message', { message }, { surfaceOp: 'append' })
    const replay = Session.create(SessionId('replay'), structuredClone(session.events))
    expect(replay.deriveMessages()).toEqual(messages)
    expect(replay.events.filter(event => event.type === 'history/message')).toHaveLength(3)
    expect(replay.events.some(event => ['assistant/message', 'tool/call', 'step/start', 'turn/start'].includes(event.type))).toBe(false)
    expect(Object.isFrozen(replay.deriveMessages()[1]?.content)).toBe(true)
    expect(Object.isFrozen(messages[0]?.content)).toBe(true)
    expect(session.deriveMessages()[0]?.content).toHaveLength(1)
  })

  it('refuses malformed imported roles and mismatched durable tool correlation', () => {
    const valid = createToolResultMessage({ callId: CallId('external-call'), content: [{ type: 'text', text: 'shipped' }], isError: false })
    const session = Session.create(SessionId('malformed'))
    session.append('history/message', { message: valid }, { surfaceOp: 'append' })
    const seed = structuredClone(session.events)
    const imported = seed.find(event => event.type === 'history/message')!
    // Imported files are untyped; deliberate invalid wire values exercise the parser.
    expect(() => Session.create(SessionId('bad-role'), [{ ...imported, data: { message: { ...valid, role: 'system' } } }] as never)).toThrow('role')
    expect(() => Session.create(SessionId('coerced-role'), [{ ...imported, data: { message: { ...valid, role: ['user'] } } }] as never)).toThrow('role')
    expect(() => Session.create(SessionId('assistant-tool'), [{ ...imported, data: { message: { ...valid, role: 'assistant' } } }] as never)).toThrow('tool source')
    expect(() => Session.create(SessionId('bad-call'), [{ ...imported, data: { message: { ...valid, source: { kind: 'tool', callId: 'other' } } } }] as never)).toThrow('mismatched tool call ids')
  })
})
