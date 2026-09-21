/** Lab-only fixed DeepSeek forwarding with a durable total request budget. */
import { createServer } from 'node:http';
import { readFileSync, writeFileSync, renameSync } from 'node:fs';
import { Readable } from 'node:stream';
import { pipeline } from 'node:stream/promises';

const directory = process.env.RANK_PROVIDER_STATE;
const config = JSON.parse(readFileSync('/config.json', 'utf8'));
if (config.baseURL !== 'https://api.deepseek.com' || config.maxRequests !== 10 || config.maxOutputTokens !== 512 || !config.proxyToken) throw new Error('Invalid bounded acceptance configuration');
const ledgerPath = directory + '/deepseek-ledger.json';
let ledger;
try { ledger = JSON.parse(readFileSync(ledgerPath, 'utf8')); }
catch (error) { if (error.code !== 'ENOENT') throw error; ledger = { attempts: [] }; }
const save = () => { writeFileSync(ledgerPath + '.tmp', JSON.stringify(ledger, null, 2) + '\n', { mode: 0o600 }); renameSync(ledgerPath + '.tmp', ledgerPath); };
const reply = (res, status, value) => { res.writeHead(status, { 'content-type': 'application/json' }); res.end(JSON.stringify(value)); };
createServer(async (req, res) => {
  if (req.url === '/facts' && req.method === 'GET') { reply(res, 200, ledger); return; }
  if (req.headers.authorization !== 'Bearer ' + config.proxyToken) { reply(res, 401, { error: 'unauthorized' }); return; }
  if (req.url === '/v1/models' && req.method === 'GET') { reply(res, 200, { data: [{ id: 'deepseek-chat' }] }); return; }
  if (req.url !== '/v1/chat/completions' || req.method !== 'POST') { reply(res, 404, { error: 'not_found' }); return; }
  try {
    let bytes = 0; const chunks = [];
    for await (const chunk of req) { bytes += chunk.length; if (bytes > 1024 * 1024) throw new Error('body limit'); chunks.push(chunk); }
    const body = JSON.parse(Buffer.concat(chunks));
    if (body.model !== 'deepseek-chat') { reply(res, 400, { error: 'acceptance_model_only' }); return; }
    if (ledger.attempts.length >= config.maxRequests) { reply(res, 403, { error: 'acceptance_budget_exhausted' }); return; }
    body.max_tokens = Math.min(body.max_tokens ?? body.max_completion_tokens ?? config.maxOutputTokens, config.maxOutputTokens);
    delete body.max_completion_tokens;
    if (!Number.isSafeInteger(body.max_tokens) || body.max_tokens < 1) { reply(res, 400, { error: 'invalid_output_limit' }); return; }
    const entry = { number: ledger.attempts.length + 1, startedAt: new Date().toISOString(), maxOutputTokens: body.max_tokens, status: 'dispatching' };
    ledger.attempts.push(entry); save(); // Persist before dispatch; a restart cannot replenish the budget.
    const started = performance.now();
    try {
      const upstream = await fetch(config.baseURL + '/chat/completions', { method: 'POST', headers: { authorization: 'Bearer ' + config.apiKey, 'content-type': 'application/json' }, body: JSON.stringify(body), redirect: 'error', signal: AbortSignal.timeout(55000) });
      entry.status = upstream.status; save();
      res.writeHead(upstream.status, { 'content-type': upstream.headers.get('content-type') ?? 'application/json' });
      await pipeline(Readable.fromWeb(upstream.body), res);
    } catch { entry.transportFailure = true; if (!res.headersSent) reply(res, 502, { error: 'provider_transport_failure' }); else res.destroy(); }
    finally { entry.durationMs = Math.round(performance.now() - started); save(); }
  } catch { if (!res.headersSent) reply(res, 400, { error: 'invalid_request' }); else res.destroy(); }
}).listen(8800, '0.0.0.0');
