/** Both SDKs retain imported history notifications without reporting old answers as new output. */
import { spawn } from 'node:child_process'
import { createServer } from 'node:http'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { delimiter, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, it } from 'vitest'
import { resolveExampleLaunch } from '@deepseek-ai/dsh-loader-smoke'
import { DeepSeekHarness } from '@deepseek-ai/dsh-sdk-client'

const repo = fileURLToPath(new URL('../../../', import.meta.url))
const fixture = fileURLToPath(new URL('./fixtures/history/', import.meta.url))
const expectedPath = join(fixture, 'projection.expected.json')

for (const sdk of ['typescript', 'python'] as const) {
  it.skipIf(sdk === 'python' && !process.env.PYTHON)(`projects imported history through the ${sdk} SDK transport`, async () => {
    const requests: { role: string; content?: string }[][] = []
    const server = createServer((request, response) => {
      void (async () => {
        const chunks = []
        for await (const chunk of request) chunks.push(chunk)
        const body = JSON.parse(Buffer.concat(chunks).toString()) as { messages: { role: string; content?: string }[] }
        requests.push(body.messages)
        response.writeHead(200, { 'content-type': 'text/event-stream' })
        response.write(`data: ${JSON.stringify({ id: 'fixture', model: 'deepseek-v4-flash', choices: [{ index: 0, delta: { role: 'assistant', content: 'Ada' }, finish_reason: null }] })}\n\n`)
        response.write(`data: ${JSON.stringify({ id: 'fixture', model: 'deepseek-v4-flash', choices: [{ index: 0, delta: {}, finish_reason: 'stop' }], usage: { prompt_tokens: 12, completion_tokens: 1, total_tokens: 13 } })}\n\n`)
        response.end('data: [DONE]\n\n')
      })().catch((error: unknown) => {
        response.writeHead(500)
        response.end(String(error))
      })
    })
    await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve))
    const address = server.address()
    if (address === null || typeof address === 'string') throw new Error('missing provider port')
    const cwd = await mkdtemp(join(tmpdir(), 'sdk-history-'))
    const launch = resolveExampleLaunch({
      srcBin: join(repo, 'packages/examples/jsonrpc-demo/src/bin.ts'),
      configArgs: [], tsconfigPath: join(repo, 'tsconfig.json'),
    })
    const env = {
      ...process.env, ...launch.env,
      DSH_CORDIS_CONFIG: join(fixture, 'cordis.yml'),
      DSH_CWD: cwd, DSH_SESSION_ROOT: join(cwd, 'sessions'),
      DEEPSEEK_API_KEY: 'fixture-key', DEEPSEEK_BASE_URL: `http://127.0.0.1:${address.port}`,
      DSH_TELEMETRY_DISABLED: '1',
    }
    const client = new DeepSeekHarness({ launch: { ...launch, cwd, env, requestTimeoutMs: 45_000 }, cwd, provider: 'deepseek-official', model: 'deepseek-v4-flash' })
    try {
      let sdkResult: unknown
      if (sdk === 'typescript') {
        const result = await client.run('Who did I mention?', { sessionId: 'history-typescript' })
        const imported = result.events.filter(event => event.type === 'history/message')
        sdkResult = {
          answer: result.finalResponse,
          imported: imported.map(event => ({ role: event.data.message.role, text: event.data.message.content.filter(block => block.type === 'text').map(block => block.text).join('') })),
          nativeAnswers: result.events.filter(event => event.type === 'assistant/message').length,
        }
        await client.close()
      } else {
        sdkResult = await new Promise<unknown>((resolve, reject) => {
          const child = spawn(process.env.PYTHON!, [join(fixture, 'python_client.py')], {
            cwd,
            env: { ...env, DSH_SESSION_ROOT: join(cwd, 'python-sessions'),
              PYTHONPATH: [join(repo, 'python/sdk/src'), process.env.PYTHONPATH].filter(Boolean).join(delimiter),
              HISTORY_LAUNCH: JSON.stringify([launch.command, ...launch.args]),
            }, stdio: ['ignore', 'pipe', 'pipe'],
          })
          let output = '', errors = ''
          child.stdout.on('data', (chunk: Buffer) => { output += chunk.toString() })
          child.stderr.on('data', (chunk: Buffer) => { errors += chunk.toString() })
          child.on('error', reject)
          child.on('exit', (code) => {
            if (code !== 0) reject(new Error(`Python SDK: ${errors}`))
            else { try { resolve(JSON.parse(output)) } catch (error) { reject(error instanceof Error ? error : new Error(String(error))) } }
          })
        })
      }
      expect(requests).toHaveLength(1)
      const modelMessages = requests[0]!.filter(message => message.role !== 'system')
      const projection = { sdkResult, modelMessages }
      const actual = JSON.stringify(projection, null, 2) + '\n'
      expect(sdkResult).toEqual({ answer: 'Ada', imported: [
        { role: 'user', text: 'Remember Ada' }, { role: 'assistant', text: 'Remembered Ada' },
      ], nativeAnswers: 1 })
      if (process.env.DSH_SNAPSHOT === 'refresh') await writeFile(expectedPath, actual)
      expect(actual).toBe(await readFile(expectedPath, 'utf8'))
    } finally {
      await client.close()
      server.closeAllConnections()
      await new Promise<void>((resolve, reject) => server.close((error) => { if (error) reject(error); else resolve() }))
      await rm(cwd, { recursive: true, force: true })
    }
  })
}
