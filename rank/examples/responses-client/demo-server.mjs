import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { createClient, dailyNews, requireCompleted, resultOf } from "./demo-client.mjs";

const files = new Map([["/", ["web/index.html", "text/html"]], ["/demo.js", ["web/demo.js", "text/javascript"]], ["/demo.css", ["web/demo.css", "text/css"]], ["/marked.js", ["node_modules/marked/lib/marked.esm.js", "text/javascript"]], ["/purify.js", ["node_modules/dompurify/dist/purify.es.mjs", "text/javascript"]]]);

/** Starts a loopback-only example app. Sessions and transcripts last until restart or 24h expiry. */
export function createDemoServer({ client, model, newsRequest = fetch, kind = "both", platformURL = "http://127.0.0.1:4173" }) {
  const sessions = new Map();
  function json(res, status, value) { res.writeHead(status, { "Content-Type": "application/json; charset=utf-8" }); res.end(JSON.stringify(value)); }
  return createServer(async (req, res) => {
    res.setHeader("Cache-Control", "no-store");
    res.setHeader("X-Content-Type-Options", "nosniff");
    const host = req.headers.host;
    if (!/^127\.0\.0\.1:\d+$/.test(host || "")) return json(res, 403, { error: "Open this demo using its 127.0.0.1 URL." });
    if (req.headers.origin && req.headers.origin !== `http://${host}`) return json(res, 403, { error: "Cross-origin requests are not allowed." });
    try {
      const path = new URL(req.url, `http://${host}`).pathname;
      if (req.method === "GET" && files.has(path)) {
        const [name, type] = files.get(path);
        res.writeHead(200, { "Content-Type": `${type}; charset=utf-8` });
        return res.end(await readFile(new URL(name, import.meta.url)));
      }
      if (!path.startsWith("/api/")) return json(res, 404, { error: "Not found" });
      for (const [id, item] of sessions) if (!item.busy && Date.now() - item.touched > 86_400_000) sessions.delete(id);
      const token = req.headers.cookie?.match(/(?:^|;\s*)rank_demo=([a-f0-9-]+)/)?.[1];
      let session = sessions.get(token);
      if (!session && req.method === "GET" && path === "/api/state") {
        if (sessions.size >= 100) return json(res, 503, { error: "Demo session limit reached; restart the demo server." });
        const id = crypto.randomUUID();
        session = { messages: [], previousId: undefined, busy: false, activeId: undefined, cancelRequested: false, broken: false, news: null, touched: Date.now() };
        sessions.set(id, session);
        res.setHeader("Set-Cookie", `rank_demo=${id}; HttpOnly; SameSite=Strict; Path=/; Max-Age=86400`);
      }
      if (!session) return json(res, 401, { error: "Session expired. Reload this page." });
      session.touched = Date.now();
      if (req.method === "GET" && path === "/api/state") return json(res, 200, { model, kind, platformURL, messages: session.messages, busy: session.busy, broken: session.broken, news: session.news });
      if (req.method !== "POST") return json(res, 405, { error: "Method not allowed" });
      if (!req.headers["content-type"]?.startsWith("application/json")) return json(res, 415, { error: "Use application/json" });
      let raw = "";
      for await (const chunk of req) { raw += chunk; if (Buffer.byteLength(raw) > 32_768) return json(res, 413, { error: "Message is too long." }); }
      let body;
      try { body = JSON.parse(raw || "{}"); } catch { return json(res, 400, { error: "Invalid JSON" }); }
      if (path === "/api/cancel") {
        session.cancelRequested = !session.activeId;
        if (session.activeId) await client.responses.cancel(session.activeId);
        return json(res, 200, { requested: true });
      }
      if (session.busy) return json(res, 409, { error: "A request is still running. Wait or stop it first." });
      if (path === "/api/reset") { session.messages = []; session.previousId = undefined; session.broken = false; return json(res, 200, { ok: true }); }
      if (path === "/api/news") {
        session.busy = true;
        try { session.news = await dailyNews(client, model, { request: newsRequest }); return json(res, 200, session.news); }
        finally { session.busy = false; }
      }
      if (path !== "/api/chat") return json(res, 404, { error: "Not found" });
      if (typeof body?.input !== "string" || !body.input.trim() || body.input.length > 8000) return json(res, 400, { error: "Enter a message of 1 to 8000 characters." });
      if (session.broken) return json(res, 409, { error: "The previous response failed or was cancelled. Start a new conversation." });
      session.busy = true;
      session.cancelRequested = false;
      const started = Date.now();
      const assistant = { role: "assistant", text: "", status: "in_progress" };
      session.messages.push({ role: "user", text: body.input.trim() }, assistant);
      res.writeHead(200, { "Content-Type": "application/x-ndjson; charset=utf-8", "X-Accel-Buffering": "no" });
      res.flushHeaders();
      const emit = value => { if (!res.destroyed) res.write(JSON.stringify(value) + "\n"); };
      let terminal = false;
      try {
        const stream = await client.responses.create({ model, input: body.input.trim(), stream: true,
          ...(session.previousId ? { previous_response_id: session.previousId } : {}),
          instructions: "用中文简洁回答，保留前文用户提供的信息。", metadata: { demo: "chat-assistant" },
        }, { headers: { "Idempotency-Key": crypto.randomUUID() } });
        for await (const event of stream) {
          if (event.response?.id) {
            session.activeId = event.response.id;
            assistant.id = event.response.id;
          }
          if (session.cancelRequested && session.activeId && !terminal) {
            session.cancelRequested = false;
            await client.responses.cancel(session.activeId);
          }
          if (event.type === "response.output_text.delta") { assistant.text += event.delta; emit({ type: "delta", text: event.delta }); }
          if (event.type === "response.completed") {
            const response = requireCompleted(event.response);
            terminal = true;
            session.previousId = response.id;
            Object.assign(assistant, resultOf(response, started));
            emit({ type: "completed", result: assistant });
          }
          if (["response.failed", "response.cancelled", "response.incomplete", "error"].includes(event.type)) {
            assistant.status = event.type === "response.cancelled" ? "cancelled" : "failed";
            throw new Error(`${event.response?.error?.message || event.message || event.type} (${session.activeId || "not accepted"})`);
          }
        }
        if (!terminal) throw new Error(`Stream disconnected before completion (${session.activeId || "unknown response"}).`);
      } catch (error) {
        if (assistant.status !== "cancelled") assistant.status = "failed";
        assistant.error = error.message;
        session.broken = true;
        emit({ type: "error", status: assistant.status, responseId: assistant.id, error: error.message });
      } finally {
        session.busy = false;
        session.activeId = undefined;
        session.cancelRequested = false;
        res.end();
      }
    } catch (error) {
      if (!res.headersSent) json(res, Number.isInteger(error.status) ? error.status : 502, { error: error.message });
      else res.end();
    }
  });
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const client = createClient();
  const port = Number(process.env.DEMO_PORT || 4180);
  createDemoServer({ client, model: process.env.RANK_MODEL, kind: process.env.DEMO_KIND, platformURL: process.env.RANK_PLATFORM_URL }).listen(port, "127.0.0.1", () => console.log(`Rank demos: http://127.0.0.1:${port}/ (endpoint: ${process.env.RANK_MODEL})`));
}
