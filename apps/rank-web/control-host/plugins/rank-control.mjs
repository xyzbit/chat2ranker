import { Service } from "@deepseek-ai/cordis";
import { CallId, LlmAdapter } from "@deepseek-ai/dsh-llm";

function textChunks(text) {
  return [
    { type: "block-start", index: 0, blockType: "text" },
    { type: "text-delta", index: 0, text },
    { type: "block-end", index: 0, block: { type: "text", text } },
    { type: "usage", usage: { inputTokens: 1, outputTokens: Math.max(1, text.length) } },
    { type: "finish", reason: { kind: "stop" } },
  ];
}

function toolChunks(id, name, args) {
  const argumentsJson = JSON.stringify(args);
  return [
    { type: "block-start", index: 0, blockType: "tool-call" },
    { type: "tool-call-delta", index: 0, id, name, argumentsDelta: argumentsJson },
    { type: "block-end", index: 0, block: { type: "tool-call", id, name, arguments: argumentsJson } },
    { type: "usage", usage: { inputTokens: 1, outputTokens: 1 } },
    { type: "finish", reason: { kind: "tool-calls" } },
  ];
}

function textOf(message) {
  return message?.content?.filter((block) => block.type === "text").map((block) => block.text).join("") || "";
}

function jsonObjectIn(text) {
  const start = text.indexOf("{");
  if (start < 0) return null;
  try { return JSON.parse(text.slice(start)); } catch { return null; }
}

class RankDemoAdapter extends LlmAdapter {
  #counter = 0;

  async resolveModel(provider, model) {
    return { provider, id: model, name: "Rank deterministic acceptance model", context: { contextWindow: 32_000 } };
  }

  async *stream(options) {
    const last = options.messages.at(-1);
    const toolResult = last?.content?.some((block) => block.type === "tool-result");
    let chunks;
    if (toolResult) {
      chunks = textChunks("实验状态已更新。我会把结构化结果显示在当前会话中；运行仍需你在确认卡片里显式开始。");
    } else {
      const text = textOf(last);
      const dataset = text.match(/dataset-[a-zA-Z0-9-]+/i)?.[0];
      const agent = text.match(/agent-[a-zA-Z0-9-]+/i)?.[0];
      const embedded = jsonObjectIn(text);
      const callId = CallId(`rank-demo-${++this.#counter}`);
      if (/列出|有哪些|可选|list/i.test(text) && /测试集|dataset|agent/i.test(text)) {
        chunks = toolChunks(callId, "rank_list_assets", {});
      } else if (/创建|新建|create/i.test(text) && /测试集|dataset/i.test(text) && embedded?.cases) {
        chunks = toolChunks(callId, "rank_create_dataset", embedded);
      } else if (/创建|新建|create/i.test(text) && /agent/i.test(text) && embedded?.name) {
        chunks = toolChunks(callId, "rank_create_agent", embedded);
      } else if (dataset && /测试集|dataset/i.test(text)) {
        chunks = toolChunks(callId, "rank_select_dataset", { datasetVersionId: dataset });
      } else if (agent && /agent/i.test(text)) {
        chunks = toolChunks(callId, "rank_select_agent", { agentVersionId: agent });
      } else if (/准备运行|准备评测|确认运行|run/i.test(text)) {
        chunks = toolChunks(callId, "rank_prepare_run", {});
      } else {
        chunks = textChunks("可以。先准备或选择测试集，再配置 Agent；我只负责对话和实验编排，不会因发送消息而直接启动评测。");
      }
    }
    for (const chunk of chunks) {
      if (options.signal?.aborted) throw new Error("aborted");
      yield chunk;
    }
  }
}

const objectOutput = {
  schema: { type: "object", additionalProperties: true },
  render(_args, value) {
    return [{ type: "text", text: JSON.stringify(value) }];
  },
};

async function requestJSON(url, options = {}) {
  const response = await fetch(url, options);
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(payload.error?.message || `Rank API ${response.status}`);
  return payload;
}

export default class RankControl extends Service {
  static inject = ["llm", "tools"];

  constructor(ctx, config = {}) {
    super(ctx, "rankControl", true);
    this.apiURL = String(config.rankApiURL || "http://127.0.0.1:8787").replace(/\/$/, "");
    this.controlToken = String(config.controlToken || "");
    this.demo = config.demo === true;
    if (this.demo) ctx.llm.registerAdapter(["rank-demo"], new RankDemoAdapter());
  }

  setupAgent(agentCtx, binding) {
    const command = async (type, payload, exec) => {
      const response = await requestJSON(`${this.apiURL}/api/internal/control/commands`, {
        method: "POST",
        signal: exec.signal,
        headers: {
          "content-type": "application/json",
          "x-rank-control-token": this.controlToken,
          "idempotency-key": `dsh-tool:${binding.controlSessionId}:${String(exec.callId)}`,
        },
        body: JSON.stringify({
          experimentId: binding.experimentId,
          controlSessionId: binding.controlSessionId,
          type,
          payload,
        }),
      });
      return response.command?.result || { accepted: true, command: type };
    };
    agentCtx.tools.register({
      name: "rank_list_assets",
      description: "List reusable Rank dataset and Agent versions before choosing or creating an asset.",
      parameters: { type: "object", properties: {}, additionalProperties: false },
      output: objectOutput,
      execute: async (_args, exec) => {
        const bootstrap = await requestJSON(`${this.apiURL}/api/bootstrap`, { signal: exec.signal });
        return {
          datasets: bootstrap.datasets.map((dataset) => ({ id: dataset.id, name: dataset.name, version: dataset.version, caseCount: dataset.caseCount, source: dataset.source })),
          agents: bootstrap.agents.map((agent) => ({ id: agent.id, handle: agent.handle, version: agent.version, runnerType: agent.runnerType, model: agent.model, preset: agent.preset || "", skills: agent.skills || [], available: agent.runtime?.available === true, unavailableReason: agent.runtime?.reason || "" })),
        };
      },
    });
    agentCtx.tools.register({
      name: "rank_create_dataset",
      description: "Create and select a reusable immutable DatasetVersion from cases prepared in the conversation.",
      parameters: {
        type: "object",
        properties: {
          name: { type: "string" },
          source: { type: "string" },
          description: { type: "string" },
          schema: { type: "object", additionalProperties: true },
          rubric: { type: "object", additionalProperties: true },
          cases: {
            type: "array", minItems: 1, maxItems: 200,
            items: {
              type: "object",
              properties: { id: { type: "string" }, title: { type: "string" }, input: { type: "string" }, expected: { type: "object", additionalProperties: true } },
              required: ["input"], additionalProperties: false,
            },
          },
        },
        required: ["name", "cases"],
        additionalProperties: false,
      },
      output: objectOutput,
      execute: (args, exec) => command("create_dataset", args, exec),
    });
    agentCtx.tools.register({
      name: "rank_create_agent",
      description: "Create and select the first immutable version of a Rank Agent configuration.",
      parameters: {
        type: "object",
        properties: {
          name: { type: "string" }, handle: { type: "string" },
          runnerType: { type: "string", enum: ["dsh", "pi", "claude-code", "codex", "hermes", "mock"] },
          model: { type: "string" }, preset: { type: "string" }, systemPrompt: { type: "string" }, description: { type: "string" },
          tools: { type: "array", items: { type: "string" } },
          skills: { type: "array", items: { type: "string" } },
        },
        required: ["name", "runnerType"],
        additionalProperties: false,
      },
      output: objectOutput,
      execute: (args, exec) => command("create_agent", args, exec),
    });
    agentCtx.tools.register({
        name: "rank_select_dataset",
        description: "Select one existing immutable DatasetVersion for the current Rank experiment.",
        parameters: {
          type: "object",
          properties: { datasetVersionId: { type: "string", description: "Exact DatasetVersion id from Rank." } },
          required: ["datasetVersionId"],
          additionalProperties: false,
        },
        output: objectOutput,
        execute: (args, exec) => command("select_dataset", args, exec),
      });
    agentCtx.tools.register({
        name: "rank_select_agent",
        description: "Select one existing immutable AgentVersion for the current Rank experiment.",
        parameters: {
          type: "object",
          properties: { agentVersionId: { type: "string", description: "Exact AgentVersion id from Rank." } },
          required: ["agentVersionId"],
          additionalProperties: false,
        },
        output: objectOutput,
        execute: (args, exec) => command("select_agent", args, exec),
      });
    agentCtx.tools.register({
        name: "rank_prepare_run",
        description: "Prepare a structured run-confirmation card. This never starts a tested Agent process.",
        parameters: { type: "object", properties: {}, additionalProperties: false },
        output: objectOutput,
        execute: (args, exec) => command("prepare_run", args, exec),
      });
    return { commit() {} };
  }
}
