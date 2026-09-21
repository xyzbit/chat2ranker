/** Bound all DSH model-stream attempts in one invocation, including retries and auxiliary calls. */
import z from '@deepseek-ai/schemastery';
import { setTimeout as delay } from 'node:timers/promises';

export const name = 'rank-model-budget';
export const inject = ['llm'];
export const Config = z.object({ maxRequests: z.number().step(1).min(1).max(Number.MAX_SAFE_INTEGER).required() });

/** Reserve synchronously before dispatch so parallel calls share the invocation's frozen cap. */
export function apply(ctx, config) {
  const maxRequests = config.maxRequests;
  if (!Number.isSafeInteger(maxRequests) || maxRequests < 1) throw new Error('A positive model request budget is required');
  let requests = 0;
  const encodedQuota = process.env.RANK_MODEL_QUOTA_TOKEN;
  delete process.env.RANK_MODEL_QUOTA_TOKEN;
  const quota = encodedQuota ? JSON.parse(encodedQuota) : null;
  if (quota && (!Number.isSafeInteger(quota.requestTimeoutMs) || quota.requestTimeoutMs < 1 || quota.requestTimeoutMs > 60000 || !Number.isSafeInteger(quota.maxOutputTokens) || quota.maxOutputTokens < 1 || typeof quota.token !== 'string' || !quota.token || typeof quota.endpoint !== 'string' || !/^https?:\/\//.test(quota.endpoint))) throw new Error('Invalid model quota runtime configuration');
  ctx.on('llm/stream', (options, next) => {
    if (requests >= maxRequests) throw new Error(`execution_model_budget_exhausted: maximum ${maxRequests} model requests per execution`);
    requests++;
    return quota ? limitedStream(quota, requests, options, next) : next();
  }, { global: true, prepend: true });
}

async function quotaRequest(quota, body, signal) {
  const response = await fetch(quota.endpoint, {
    method: 'POST', headers: { Authorization: `Bearer ${quota.token}`, 'Content-Type': 'application/json' },
    body: JSON.stringify(body), signal: AbortSignal.any([signal ?? new AbortController().signal, AbortSignal.timeout(quota.requestTimeoutMs)]), redirect: 'error',
  });
  if (!response.ok) throw new Error(`execution_model_quota_unavailable: HTTP ${response.status}`);
  return response.json();
}

async function* limitedStream(quota, sequence, options, next) {
  if (options.provider !== 'rank-connection') throw new Error('execution_model_quota_unsupported_provider');
  if (options.maxTokens !== undefined && (!Number.isSafeInteger(options.maxTokens) || options.maxTokens < 1 || options.maxTokens > quota.maxOutputTokens)) throw new Error('execution_model_quota_output_limit');
  // UTF-8 bytes plus message overhead is an explicit conservative estimate,
  // not provider tokenization. The adapter's configured output cap is reserved.
  const inputTokens = Buffer.byteLength(JSON.stringify({ system: options.system, messages: options.messages, tools: options.tools })) + 32 * (options.messages.length + 1);
  for (;;) {
    options.signal?.throwIfAborted();
    const started = performance.now();
    const permit = await quotaRequest(quota, { sequence, inputTokens }, options.signal);
    if (permit.granted) {
      if (!Number.isSafeInteger(permit.validForMs) || permit.validForMs <= performance.now() - started) throw new Error('execution_model_permit_expired');
      break;
    }
    if (!Number.isSafeInteger(permit.retryAfterMs) || permit.retryAfterMs < 1 || permit.retryAfterMs > 61000) throw new Error('execution_model_quota_invalid_wait');
    await delay(permit.retryAfterMs, undefined, { signal: options.signal });
  }
  let actualTokens;
  try {
    for await (const chunk of next()) {
      if (chunk.type === 'usage') {
        const { inputTokens: input, outputTokens: output, cacheReadTokens = 0, cacheWriteTokens = 0 } = chunk.usage;
        const counts = [input, output, cacheReadTokens, cacheWriteTokens];
        if (counts.every(value => Number.isSafeInteger(value) && value >= 0)) actualTokens = counts.reduce((sum, value) => sum + value, 0);
      }
      yield chunk;
    }
  } finally {
    // Some adapters emit synthetic zero usage on transport/provider failure.
    if (actualTokens > 0) {
      try { await quotaRequest(quota, { sequence, actualTokens }); }
      catch (_quotaReportFailure) { /* Failed reporting retains the original reservation until its expiry. */ }
    }
  }
}
