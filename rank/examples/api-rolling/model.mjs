/** Deterministic slow model for real DSH requests during API replacement. */
import { createServer } from 'node:http';
const calls = [];
createServer(async (request, response) => {
  if (request.url === '/facts') { response.setHeader('content-type', 'application/json'); response.end(JSON.stringify(calls)); return; }
  if (request.url === '/v1/models') { response.setHeader('content-type', 'application/json'); response.end(JSON.stringify({ object: 'list', data: [{ id: 'rank-rolling-model', object: 'model', owned_by: 'local-fixture' }] })); return; }
  if (request.url !== '/v1/chat/completions') { response.writeHead(404); response.end(); return; }
  let body = ''; for await (const chunk of request) body += chunk;
  const input = JSON.parse(body);
  const text = JSON.stringify(input.messages);
  const label = text.includes('ROLLING-LONG') ? 'long' : text.includes('ROLLING-SLOW') ? 'slow' : 'probe';
  const delay = label === 'long' ? 45_000 : label === 'slow' ? 25_000 : 0;
  const call = { label, startedAt: Date.now(), completed: false }; calls.push(call);
  const output = label === 'probe' ? 'READY' : `ROLLING-${label.toUpperCase()}-DONE`;
  await new Promise(resolve => setTimeout(resolve, delay));
  call.completed = true;
  if (!input.stream) { response.setHeader('content-type', 'application/json'); response.end(JSON.stringify({ id: 'fixture', object: 'chat.completion', model: input.model, choices: [{ index: 0, message: { role: 'assistant', content: output }, finish_reason: 'stop' }], usage: { prompt_tokens: 10, completion_tokens: 5, total_tokens: 15 } })); return; }
  response.writeHead(200, { 'content-type': 'text/event-stream' });
  for (const [delta, finish] of [[{ role: 'assistant', content: output }, null], [{}, 'stop']]) {
    response.write('data: ' + JSON.stringify({ id: 'fixture', object: 'chat.completion.chunk', model: input.model, choices: [{ index: 0, delta, finish_reason: finish }] }) + '\n\n');
  }
  response.end('data: [DONE]\n\n');
}).listen(8800, '0.0.0.0');
