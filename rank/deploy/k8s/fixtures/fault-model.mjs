/** Local failure-injection provider: count external effects without logging prompts or credentials. */
import { createServer } from 'node:http';
const facts = new Map();
const pendingEffects = new Map();
const get = key => {
  if (!facts.has(key)) facts.set(key, { modelRequests: 0, effects: 0, closedEffects: 0, deniedWrites: 0 });
  return facts.get(key);
};
const server = createServer(async (request, response) => {
  try {
    const url = new URL(request.url, 'http://fixture');
    if (url.pathname === '/health') { response.end('{"ok":true}'); return; }
    if (url.pathname === '/facts') {
      response.setHeader('content-type', 'application/json');
      response.end(JSON.stringify(Object.fromEntries(facts))); return;
    }
    if (url.pathname === '/release' && request.method === 'POST') {
      const pending = pendingEffects.get(url.searchParams.get('key'));
      if (!pending) { response.writeHead(404); response.end(); return; }
      pending.end('effect acknowledged'); response.end('{"released":true}'); return;
    }
    if (url.pathname === '/effect') {
      const key = url.searchParams.get('key');
      if (!/^fault-[a-z0-9-]+$/.test(key || '')) throw new Error('Invalid fixture key');
      const value = get(key); value.effects++;
      pendingEffects.set(key, response);
      response.once('close', () => { if (pendingEffects.get(key) === response) pendingEffects.delete(key); });
      if (url.searchParams.get('deniedWrite') === 'true') value.deniedWrites++;
      response.once('close', () => { value.closedEffects++; });
      const timer = setTimeout(() => response.end('effect acknowledged'), 90_000);
      response.once('close', () => clearTimeout(timer));
      return;
    }
    if (url.pathname === '/v1/models') { response.end('{"object":"list","data":[{"id":"rank-lab","object":"model"}]}'); return; }
    if (url.pathname !== '/v1/chat/completions' || request.method !== 'POST') { response.writeHead(404); response.end(); return; }
    const chunks = [];
    for await (const chunk of request) chunks.push(chunk);
    const body = JSON.parse(Buffer.concat(chunks).toString());
    const key = body.messages?.filter(message => message.role === 'user').map(message => typeof message.content === 'string' ? message.content : JSON.stringify(message.content)).join('\n').match(/fault-[a-z0-9-]+/)?.[0];
    const value = key ? get(key) : null;
    if (value) {
      value.modelRequests++;
      value.roles = body.messages.map(message => message.role);
      value.toolResults = body.messages.filter(message => message.role === 'tool').map(message => String(typeof message.content === 'string' ? message.content : JSON.stringify(message.content)).slice(0, 1000));
    }
    let message = { role: 'assistant', content: 'Fault fixture completed.' };
    let finish = 'stop';
    if (key && key.includes('-tool-') && !body.messages.some(item => item.role === 'tool')) {
      if (!body.tools?.some(tool => tool.function?.name === 'bash')) throw new Error('DSH must advertise its real bash tool');
      const endpoint = new URL(`/effect?key=${key}&deniedWrite=true`, `http://${request.headers.host}`);
      const program = `try{require("node:fs").writeFileSync("/tmp/rank-fault-denied", "unexpected");throw new Error("read-only sandbox permitted a write")}catch(e){if(!["EACCES","EPERM","EROFS"].includes(e.code))throw e}fetch(${JSON.stringify(endpoint.href)}).then(r=>r.text()).then(console.log).catch(e=>{console.error(e.message);process.exitCode=1})`;
      const command = `node -e '${program.replaceAll("'", "'\\''")}'`;
      message = { role: 'assistant', content: null, tool_calls: [{ id: 'effect_' + key, type: 'function', function: { name: 'bash', arguments: JSON.stringify({ command, description: 'Call the local failure-injection tool', timeoutMs: 90_000 }) } }] };
      finish = 'tool_calls';
    }
    const frame = choices => ({ id: 'rank-fault-fixture', object: 'chat.completion.chunk', model: body.model, choices });
    const usage = { prompt_tokens: 12, completion_tokens: 3, total_tokens: 15 };
    if (body.stream) {
      const delta = message.tool_calls ? { role: 'assistant', tool_calls: message.tool_calls.map((call, index) => ({ index, ...call })) } : message;
      response.writeHead(200, { 'content-type': 'text/event-stream' });
      response.write(`data: ${JSON.stringify(frame([{ index: 0, delta, finish_reason: null }]))}\n\n`);
      response.write(`data: ${JSON.stringify({ ...frame([{ index: 0, delta: {}, finish_reason: finish }]), usage })}\n\n`);
      response.end('data: [DONE]\n\n');
    } else {
      response.setHeader('content-type', 'application/json');
      response.end(JSON.stringify({ id: 'rank-fault-fixture', model: body.model, choices: [{ index: 0, message, finish_reason: finish }], usage }));
    }
  } catch (error) {
    response.writeHead(400); response.end(JSON.stringify({ error: { message: error.message } }));
  }
});
server.listen(8800, '0.0.0.0');
