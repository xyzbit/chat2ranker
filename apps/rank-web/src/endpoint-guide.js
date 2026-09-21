/** Generates executable Responses examples without storing credentials. */
export function endpointExamples(name, origin, apiKey = "") {
  const baseURL = new URL("/v1", origin).href;
  const model = JSON.stringify(name);
  const setup = `import OpenAI from "openai";

const client = new OpenAI({
  baseURL: ${JSON.stringify(baseURL)},
  apiKey: process.env.RANK_API_KEY,
  maxRetries: 0,
  timeout: 180_000,
});`;
  const quoteShell = value => "'" + value.replaceAll("'", "'\\''") + "'";
  const environment = apiKey ? `export RANK_API_KEY=${quoteShell(apiKey)}\n` : "";
  const curl = `curl ${quoteShell(baseURL + "/responses")} \\
  -H "Authorization: Bearer $RANK_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '${JSON.stringify({ model: name, input: "Hello" })}'`;
  const javascript = `${setup}

const response = await client.responses.create({
  model: ${model},
  input: "Hello",
});
if (response.status !== "completed") throw new Error(response.error?.message || response.status);
console.log(response.output_text);

const next = await client.responses.create({
  model: ${model},
  previous_response_id: response.id,
  input: "Tell me more",
});
if (next.status !== "completed") throw new Error(next.error?.message || next.status);
console.log(next.output_text);`;
  const instructions = `${setup}

const response = await client.responses.create({
  model: ${model},
  instructions: "Reply in Chinese, in no more than three sentences.",
  input: "Explain how a rainbow forms.",
});
if (response.status !== "completed") throw new Error(response.error?.message || response.status);
console.log(response.output_text);`;
  const stream = `${setup}

const events = await client.responses.create({
  model: ${model},
  input: "Introduce yourself.",
  stream: true,
});
let completed;
for await (const event of events) {
  if (event.type === "response.output_text.delta") {
    process.stdout.write(event.delta);
  } else if (event.type === "response.completed") {
    completed = event.response;
  } else if (["response.failed", "response.cancelled", "response.incomplete", "error"].includes(event.type)) {
    throw new Error(event.response?.error?.message || event.message || event.type);
  }
}
if (!completed) throw new Error("Stream disconnected before completion");
console.log("\\nResponse ID:", completed.id);`;
  const tools = `${setup}

const response = await client.responses.create({
  model: ${model},
  input: "What time is it on the server?",
  tools: [{
    type: "function",
    name: "get_server_time",
    description: "Read the server time",
    parameters: {
      type: "object",
      properties: {},
      additionalProperties: false,
    },
    strict: true,
  }],
});
if (response.status !== "completed") throw new Error(response.error?.message || response.status);
const call = response.output.find(item => item.type === "function_call");
if (call) {
  if (call.name !== "get_server_time") throw new Error("Unknown function");
  const args = JSON.parse(call.arguments);
  if (Object.keys(args).length) throw new Error("Unexpected arguments");
  const result = { time: new Date().toISOString() };
  const next = await client.responses.create({
    model: ${model},
    previous_response_id: response.id,
    input: [{
      type: "function_call_output",
      call_id: call.call_id,
      output: JSON.stringify(result),
    }],
  });
  if (next.status !== "completed") throw new Error(next.error?.message || next.status);
  console.log(next.output_text);
} else {
  console.log(response.output_text);
}`;
  return {
    baseURL, curl, javascript, instructions, stream, tools,
    curlCommand: environment + curl,
    javascriptCommand: `${environment}npm install openai\nnode --input-type=module <<'RANK_EXAMPLE'\n${javascript}\nRANK_EXAMPLE`,
  };
}

/** Credential-free handoff for a coding Agent; secrets never enter its instructions. */
export function endpointAgentInstructions(name, origin, locale, docsURL) {
  const examples = endpointExamples(name, origin);
  return locale === "en"
    ? `Integrate the Rank endpoint "${name}" into this project using the OpenAI Responses API. Read RANK_API_KEY from the server environment. Never embed the key in chat or frontend code. Reuse the project framework and implement multi-turn chat with previous_response_id.\nAPI base URL: ${examples.baseURL}\nDocumentation: ${docsURL}\n\n${examples.javascript}`
    : `请将 Rank 端点「${name}」接入当前项目，使用 OpenAI Responses API。密钥从服务端环境变量 RANK_API_KEY 读取，不写入对话或前端代码。复用项目框架，实现通过 previous_response_id 续接的多轮对话。\nAPI Base URL：${examples.baseURL}\n完整文档：${docsURL}\n\n${examples.javascript}`;
}
