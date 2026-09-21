#!/usr/bin/env node
if (process.argv.includes("--version")) {
  console.log("codex-fixture 1.0");
} else {
  console.log(JSON.stringify({ type: "thread.started", diagnostic: "raw-protocol-marker" }));
  await new Promise(resolve => setTimeout(resolve, 100));
  console.log(JSON.stringify({ type: "item.completed", item: { type: "agent_message", text: "Native answer" } }));
  console.log(JSON.stringify({ type: "turn.completed", usage: { input_tokens: 12, output_tokens: 3 } }));
}
