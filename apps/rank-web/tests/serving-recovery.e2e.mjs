import assert from "node:assert/strict";
import { createServer } from "node:http";
import { Readable } from "node:stream";

/** A network fault proxy around the real Execution API; it never invents results. */
export async function startServingFaultProxy(executionURL) {
  let fault;
  const accepted = [];
  const server = createServer(async (incoming, outgoing) => {
    try {
      const chunks = [];
      for await (const chunk of incoming) chunks.push(chunk);
      const body = Buffer.concat(chunks);
      const submit = incoming.method === "POST" && incoming.url === "/v1/executions";
      const request = submit ? JSON.parse(body.toString()) : undefined;
      const current = submit && fault && request.spec.prompt.includes(fault.marker) && (!fault.kind || request.spec.kind === fault.kind) ? fault : undefined;
      if (current?.stage === "before_submit") {
        current.seen();
        outgoing.destroy();
        return;
      }
      const headers = { ...incoming.headers };
      delete headers.host;
      delete headers.connection;
      const response = await fetch(executionURL + incoming.url, {
        method: incoming.method, headers, body: body.length ? body : undefined,
      });
      if (submit) {
        assert.equal(response.status, 202);
        const value = await response.json();
        accepted.push({ key: request.idempotencyKey, id: value.id, spec: request.spec });
        if (current?.stage === "after_acceptance") {
          current.seen();
          outgoing.destroy();
        } else {
          outgoing.writeHead(response.status, {"content-type":"application/json"});
          outgoing.end(JSON.stringify(value));
        }
        return;
      }
      const returningResult = fault?.stage === "before_result_commit" && incoming.method === "GET"
        && accepted.some(item => incoming.url === `/v1/executions/${item.id}` && item.spec.prompt.includes(fault.marker));
      if (returningResult) {
        const value = await response.json();
        assert.equal(value.status, "completed");
        fault.seen();
        outgoing.destroy();
        return;
      }
      outgoing.writeHead(response.status, Object.fromEntries(response.headers));
      if (response.body) {
        const stream = Readable.fromWeb(response.body);
        outgoing.on("close", () => stream.destroy());
        stream.on("error", () => outgoing.destroy());
        stream.pipe(outgoing);
      } else outgoing.end();
    } catch (error) {
      outgoing.destroy(error);
    }
  });
  await new Promise((resolve, reject) => server.listen(0, "127.0.0.1", resolve).once("error", reject));
  return {
    url: `http://127.0.0.1:${server.address().port}`,
    accepted,
    arm(marker, stage, kind) {
      return new Promise(resolve => { fault = { marker, stage, kind, seen: resolve }; });
    },
    clear() { fault = undefined; },
    async close() {
      server.closeAllConnections();
      await new Promise(resolve => server.close(resolve));
    },
  };
}

/** Recover committed Responses across actual Rank process death and lost HTTP replies. */
export async function verifyServingRecovery({ baseURL, request, authorization, proxy, restartRank }) {
  const snapshot = [];
  for (const stage of ["before_submit", "after_acceptance", "before_result_commit"]) {
    const marker = `durable-serving-${stage}`;
    const seen = proxy.arm(marker, stage);
    const response = await request(baseURL, "/v1/responses", {
      method: "POST", headers: { ...authorization, "idempotency-key": marker },
      body: JSON.stringify({ model: "research-public@production", input: marker, background: true }),
    });
    let timer;
    try {
      await Promise.race([seen, new Promise((_, reject) => { timer = setTimeout(() => reject(new Error(`fault window not reached: ${stage}`)), 5000); })]);
    } finally { clearTimeout(timer); }
    // Kill before the business outcome is committed, leaving remote work intact.
    await restartRank(() => proxy.clear());
    let final;
    const deadline = Date.now() + 10000;
    do {
      final = await request(baseURL, `/v1/responses/${response.id}`, { headers: authorization });
      if (["completed", "failed", "cancelled"].includes(final.status)) break;
      assert.ok(Date.now() < deadline, `response not reconciled: ${stage}`);
      await new Promise(resolve => setTimeout(resolve, 20));
    } while (true);
    assert.equal(final.status, "completed");
    const detail = await request(baseURL, `/api/responses/${response.id}?tenantId=tenant-serving-e2e`);
    const observed = proxy.accepted.filter(item => item.key === `response:${response.id}`);
    if (stage !== "before_submit") {
      assert.ok(observed.length > 0);
      assert.ok(observed.every(item => item.id === detail.response.executionId));
    }
    const replay = await request(baseURL, "/v1/responses", {
      method: "POST", headers: { ...authorization, "idempotency-key": marker },
      body: JSON.stringify({ model: "research-public@production", input: marker, background: true }),
    });
    assert.equal(replay.id, response.id);
    const terminalEvents = detail.events.filter(event => event.type === "response.completed").length;
    assert.equal(terminalEvents, 1);
    snapshot.push({ window: stage, status: final.status, sameResponse: replay.id === response.id,
      originalExecutionRecovered: stage !== "before_submit" ? observed.every(item => item.id === detail.response.executionId) : null,
      terminalEvents });
  }
  return snapshot;
}
