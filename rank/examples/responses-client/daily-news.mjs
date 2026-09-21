import { createClient, dailyNews } from "./demo-client.mjs";

try {
  const result = await dailyNews(createClient(), process.env.RANK_MODEL);
  console.log(result.text);
  console.log("\nSources:");
  for (const source of result.sources) console.log(`[${source.number}] ${source.title}\n${source.url}`);
  console.log(JSON.stringify({ responseId: result.id, collectedAt: result.collectedAt, elapsedMs: result.elapsedMs, deployment: result.deployment }, null, 2));
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
