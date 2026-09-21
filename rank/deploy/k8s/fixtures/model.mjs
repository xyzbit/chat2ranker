/** Deterministic protocol provider for isolated platform lab acceptance; no external model calls. */
import { createServer } from 'node:http';
const delayMs = Number(process.env.RANK_LAB_DELAY_MS || 0);
if (!Number.isFinite(delayMs) || delayMs < 0 || delayMs > 30_000) throw new Error('RANK_LAB_DELAY_MS must be between 0 and 30000');
const answer = messages => {
  const text = messages.filter(message => message.role !== 'system').map(message => typeof message.content === 'string' ? message.content : JSON.stringify(message.content)).join('\n');
  if (messages.some(message => message.role === 'system' && String(message.content).includes('Return every final user-visible reply'))) return '{"type":"summary","text":"Rank 本地集群中控服务已连接。"}';
  if (text.includes('CANDIDATE_OUTPUT')) return '{"status":"pass","passed":true,"score":1,"reason":"Deterministic lab evidence"}';
  if (messages.some(message => message.role === 'system' && String(message.content).includes('Hacker News'))) return '今日热点（本地测试源）：\n1. 本地集群完成端点调用验证。[1]\n2. 多轮消息保留角色与工具关联。[2]';
  if (text.includes('小林')) return '小林，你喜欢科技新闻，每天有10分钟：先读要闻，再选一条深入阅读。';
  if (text.includes('Ada')) return 'Ada';
  if (text.includes('shipped')) return 'The order has shipped.';
  return 'Rank local platform request completed.';
};
const server = createServer(async (request, response) => {
  try {
    if (request.url === '/health') { response.end('{"ok":true}'); return; }
    if (request.url === '/v1/models') {
      response.setHeader('content-type', 'application/json');
      response.end('{"object":"list","data":[{"id":"rank-lab","object":"model"}]}'); return;
    }
    if (request.url !== '/v1/chat/completions' || request.method !== 'POST') { response.writeHead(404); response.end(); return; }
    const chunks = [];
    for await (const chunk of request) chunks.push(chunk);
    const body = JSON.parse(Buffer.concat(chunks).toString());
    if (!Array.isArray(body.messages)) throw new Error('messages required');
    let pending = new Set();
    for (const message of body.messages) {
      if (message.role === 'tool') {
        if (!pending.delete(message.tool_call_id)) throw new Error('unmatched tool result');
      } else {
        if (pending.size) throw new Error('interrupted tool pair');
        for (const call of message.tool_calls || []) pending.add(call.id);
      }
    }
    if (pending.size) throw new Error('missing tool result');
    process.stdout.write(JSON.stringify({ type: 'model.request', roles: body.messages.map(message => message.role), model: body.model }) + '\n');
    if (String(body.model).startsWith('rank-budget-429-')) {
      response.writeHead(429, { 'content-type': 'application/json' });
      response.end('{"error":{"message":"request rate limited","type":"rate_limit_error"}}');
      return;
    }
    if (delayMs) await new Promise(resolve => setTimeout(resolve, delayMs));
    const output = answer(body.messages);
    // Log protocol facts only; never log request text, credentials or tool arguments.
    const frame = choices => ({ id: 'rank-fixture', object: 'chat.completion.chunk', model: body.model, choices });
    if (body.stream) {
      response.writeHead(200, { 'content-type': 'text/event-stream' });
      response.write(`data: ${JSON.stringify(frame([{ index: 0, delta: { role: 'assistant', content: output }, finish_reason: null }]))}\n\n`);
      response.write(`data: ${JSON.stringify({ ...frame([{ index: 0, delta: {}, finish_reason: 'stop' }]), usage: { prompt_tokens: 12, completion_tokens: 3, total_tokens: 15 } })}\n\n`);
      response.end('data: [DONE]\n\n');
    } else {
      response.setHeader('content-type', 'application/json');
      response.end(JSON.stringify({ id: 'rank-fixture', model: body.model, choices: [{ index: 0, message: { role: 'assistant', content: output }, finish_reason: 'stop' }], usage: { prompt_tokens: 12, completion_tokens: 3, total_tokens: 15 } }));
    }
  } catch (error) { response.writeHead(400); response.end(JSON.stringify({ error: { message: error.message } })); }
});
server.listen(Number(process.env.PORT || 8800), '0.0.0.0');
