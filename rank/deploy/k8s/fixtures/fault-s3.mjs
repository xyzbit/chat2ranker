/** Lab-only streaming proxy: interrupt selected artifact bodies without logging S3 authorization. */
import { createServer, request as forward } from 'node:http';

let armed = null;
const facts = { interrupted: 0, receivedBytes: 0, forwardedBytes: 0, completedKeys: [] };
const control = createServer(async (request, response) => {
  if (request.method === 'POST' && request.url === '/arm') {
    const chunks = [];
    for await (const chunk of request) chunks.push(chunk);
    const { executionID, holdCommit = false } = JSON.parse(Buffer.concat(chunks).toString());
    if (!/^[a-zA-Z0-9_-]+$/.test(executionID)) { response.writeHead(400); response.end(); return; }
    armed = { executionID, firstKey: null, holdCommit };
  }
  response.setHeader('content-type', 'application/json');
  response.end(JSON.stringify(facts));
});
const proxy = createServer((request, response) => {
  const selected = armed && request.method === 'PUT' && request.url.includes('/' + armed.executionID + '/');
  if (selected && !armed.firstKey) armed.firstKey = request.url;
  const interrupt = selected && !armed.holdCommit && request.url !== armed.firstKey;
  const upstream = forward({ hostname: process.env.UPSTREAM_IP, port: 8333, method: request.method, path: request.url, headers: request.headers }, result => {
    if (selected && result.statusCode >= 200 && result.statusCode < 300 && !facts.completedKeys.includes(request.url)) facts.completedKeys.push(request.url);
    if (selected && armed.holdCommit && result.statusCode >= 200 && result.statusCode < 300) {
      result.resume(); // Keep the successful storage acknowledgement from the Worker.
      return;
    }
    response.writeHead(result.statusCode, result.headers); result.pipe(response);
  });
  upstream.on('error', () => { if (!response.destroyed) { response.writeHead(502); response.end(); } });
  request.on('error', () => upstream.destroy());
  response.on('close', () => upstream.destroy());
  if (!interrupt) { request.pipe(upstream); return; }
  request.once('data', chunk => {
    const bytes = chunk.subarray(0, Math.min(16, chunk.length));
    facts.interrupted++; facts.receivedBytes += chunk.length; facts.forwardedBytes += bytes.length;
    upstream.write(bytes, () => {
      // Leave the declared body unfinished on both actual TCP connections.
      upstream.destroy(); response.destroy(); request.destroy();
    });
    request.pause();
  });
  request.once('end', () => { upstream.destroy(); response.destroy(); });
});
control.listen(8334, '0.0.0.0');
proxy.listen(8333, '0.0.0.0');
