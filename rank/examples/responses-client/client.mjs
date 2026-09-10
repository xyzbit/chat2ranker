import assert from "node:assert/strict";
import OpenAI from "openai";

function completed(response) {
  if (response.status !== "completed") {
    throw new Error(`Response ${response.id}: ${response.status} (${response.error?.code || "no error code"})`);
  }
  return response;
}

function textOf(response) {
  return response.output.flatMap(item => item.content || [])
    .filter(item => item.type === "output_text").map(item => item.text).join("");
}

/**
 * Calls only the public Responses API; the two-turn demo asserts recall and version pinning.
 * @param options Rank endpoint, serving credential, deployment reference, and optional single-turn prompt.
 * @returns Response identifiers and resolved model after successful completion.
 */
export async function runExample({ baseURL, apiKey, model, prompt }) {
  if (!apiKey || !model) throw new Error("Set RANK_API_KEY and RANK_MODEL; use a Rank serving key, not a provider key.");
  const client = new OpenAI({ baseURL, apiKey, maxRetries: 0, timeout: 180_000 });
  let first;
  if (!prompt) {
    first = completed(await client.responses.create({
      model,
      input: "Remember order number RANK-2048. Reply only with that order number.",
    }, { headers: { "Idempotency-Key": crypto.randomUUID() } }));
    console.log(`First reply: ${textOf(first)}`);
    assert.ok(textOf(first).includes("RANK-2048"), "First reply did not contain the order number");
  }

  const stream = await client.responses.create({
    model,
    input: prompt || "What was the order number? Reply only with the number, including its prefix.",
    ...(first ? { previous_response_id: first.id } : {}),
    stream: true,
  }, { headers: { "Idempotency-Key": crypto.randomUUID() } });
  process.stdout.write("Streaming reply: ");
  let result;
  for await (const event of stream) {
    if (event.type === "response.output_text.delta") process.stdout.write(event.delta);
    if (event.type === "response.completed") result = completed(event.response);
    if (["response.failed", "response.incomplete", "response.cancelled"].includes(event.type)) {
      throw new Error(`Stream ended with ${event.type}: ${event.response?.id || "unknown response"}`);
    }
    if (event.type === "error") throw new Error(`Stream error: ${event.code || "unknown"}`);
  }
  console.log();
  assert.ok(result, "Stream closed without response.completed");
  const stored = completed(await client.responses.retrieve(result.id));
  assert.equal(textOf(stored), textOf(result), "Stored output differs from streamed response");
  if (first) {
    assert.ok(textOf(stored).includes("RANK-2048"), "Continuation lost the order number");
    assert.equal(stored.model, first.model, "Continuation changed the resolved Agent version");
  }
  const report = { firstResponseId: first?.id, responseId: stored.id, model: stored.model, status: stored.status };
  console.log(JSON.stringify(report, null, 2));
  return report;
}
