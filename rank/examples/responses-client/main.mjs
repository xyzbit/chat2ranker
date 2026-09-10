import { runExample } from "./client.mjs";

try {
  await runExample({
    baseURL: process.env.RANK_BASE_URL || "http://127.0.0.1:8787/v1",
    apiKey: process.env.RANK_API_KEY,
    model: process.env.RANK_MODEL,
    prompt: process.argv.slice(2).join(" "),
  });
} catch (error) {
  // SDK errors can include request headers; print only the diagnostic message.
  console.error(`Failed: ${error.message}`);
  process.exitCode = 1;
}
