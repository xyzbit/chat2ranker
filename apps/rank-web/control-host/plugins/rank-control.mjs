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

function rankMessage(...blocks) {
  return blocks.map((block) => JSON.stringify(block)).join("\n");
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
    const tail = options.messages.at(-1);
    const toolResult = tail?.content?.some((block) => block.type === "tool-result");
    const last = toolResult ? tail
      : options.messages.findLast((message) => message.source?.kind === "user")
        || options.messages.findLast((message) => message.role === "user" && !["agent-instructions", "skill-catalog"].includes(message.source?.kind) && !textOf(message).startsWith("Current runtime context."))
        || tail;
    let chunks;
    if (toolResult) {
      chunks = textChunks(rankMessage(
        { type: "summary", text: "实验状态已更新" },
        { type: "paragraph", text: "结构化结果已显示在当前会话中；运行仍需在确认卡片中显式开始。" },
      ));
    } else {
      const text = textOf(last);
      const dataset = text.match(/dataset-[a-zA-Z0-9-]+/i)?.[0];
      const agent = text.match(/agent-[a-zA-Z0-9-]+/i)?.[0];
      const embedded = jsonObjectIn(text);
      const callId = CallId(`rank-demo-${++this.#counter}`);
      if (/列出|有哪些|可选|list/i.test(text) && /测试集|dataset|agent/i.test(text)) {
        chunks = toolChunks(callId, "rank_list_assets", {});
      } else if (/新增|添加|追加|add|append/i.test(text) && /用例|case/i.test(text) && embedded?.baseDatasetVersionId && embedded?.cases) {
        chunks = toolChunks(callId, "rank_add_dataset_cases", embedded);
      } else if (/创建|新建|create/i.test(text) && /测试集|dataset/i.test(text) && embedded?.cases) {
        chunks = toolChunks(callId, "rank_create_dataset", embedded);
      } else if (/新版本|version/i.test(text) && /agent/i.test(text) && embedded?.baseAgentVersionId) {
        chunks = toolChunks(callId, "rank_create_agent_version", embedded);
      } else if (/更新|修改|update|edit/i.test(text) && /模型连接|llm connection|model connection/i.test(text) && embedded?.connectionId) {
        chunks = toolChunks(callId, "rank_update_model_connection", embedded);
      } else if (/创建|新建|添加|create|add/i.test(text) && /模型连接|llm connection|model connection/i.test(text) && embedded) {
        chunks = toolChunks(callId, "rank_create_model_connection", embedded);
      } else if (/切换|设置|使用|switch|set/i.test(text) && /对话模型|评审模型|judge|control/i.test(text) && embedded?.connectionId) {
        chunks = toolChunks(callId, "rank_set_system_model", embedded);
      } else if (/创建|新建|create/i.test(text) && /agent/i.test(text) && embedded?.name) {
        chunks = toolChunks(callId, "rank_create_agent", embedded);
	  } else if (/准备运行|准备评测|确认运行|开始运行|我要运行|run/i.test(text) && embedded?.agentVersionIds) {
		chunks = toolChunks(callId, "rank_prepare_run", embedded);
      } else if (dataset && /测试集|dataset/i.test(text)) {
        chunks = toolChunks(callId, "rank_select_dataset", { datasetVersionId: dataset });
      } else if (agent && /agent/i.test(text)) {
        chunks = toolChunks(callId, "rank_select_agent", { agentVersionId: agent });
      } else if (/(?:多个|多种|同时|并行).{0,8}agent|agent.{0,8}(?:多个|同时|并行)/i.test(text)) {
		chunks = textChunks(rankMessage(
		  { type: "summary", text: "支持多 Agent 对比" },
		  { type: "paragraph", text: "在运行确认卡中添加 2–4 个 Agent 版本即可；Rank 会创建一个对比组，并为每个 Agent 创建独立 Run。" },
		));
	  } else if (/实验(?:表现|数据|结果)|运行(?:表现|数据|结果)|对比.*(?:agent|运行)/i.test(text)) {
        chunks = toolChunks(callId, "rank_show_experiment_results", {});
      } else if (/准备运行|准备评测|确认运行|开始运行|我要运行|run/i.test(text)) {
        chunks = toolChunks(callId, "rank_prepare_run", {});
      } else {
        chunks = textChunks(rankMessage(
          { type: "summary", text: "先准备测试集和 Agent" },
          { type: "paragraph", text: "我负责对话和实验编排；发送消息不会直接启动评测。" },
        ));
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
		const [bootstrap, experiment] = await Promise.all([
		  requestJSON(`${this.apiURL}/api/bootstrap`, { signal: exec.signal }),
		  requestJSON(`${this.apiURL}/api/experiments/${binding.experimentId}`, { signal: exec.signal }),
		]);
        return {
		  currentExperiment: { id: experiment.id, datasetVersionId: experiment.datasetVersionId || "", agentVersionId: experiment.agentVersionId || "", dataset: experiment.dataset || null, agent: experiment.agent || null },
          datasets: bootstrap.datasets.map((dataset) => ({ id: dataset.id, name: dataset.name, version: dataset.version, caseCount: dataset.caseCount, source: dataset.source })),
		  agents: bootstrap.agents.map((agent) => ({ id: agent.id, handle: agent.handle, version: agent.version, runnerType: agent.runnerType, model: agent.model, preset: agent.preset || "", tools: agent.tools || [], skills: agent.skills || [], available: agent.runtime?.available === true, unavailableReason: agent.runtime?.reason || "" })),
		  modelConnections: (bootstrap.modelConnections || []).map((connection) => ({ id: connection.id, name: connection.name, provider: connection.provider || "custom", protocol: connection.protocol, baseUrl: connection.baseUrl, defaultModel: connection.defaultModel || "", models: connection.models || [], prices: connection.prices || {}, status: connection.status })),
		  modelCatalog: bootstrap.modelCatalog || [],
		  systemModels: bootstrap.systemModels || [],
        };
      },
    });
    const connectionParameters = {
      type: "object",
      properties: {
        name: { type: "string" },
        provider: { type: "string", description: "Provider id from the Rank model catalog, for example deepseek or custom." },
        protocol: { type: "string", enum: ["openai-chat-completions", "openai-responses"] },
        baseUrl: { type: "string" },
        defaultModel: { type: "string" },
        prices: {
          type: "object",
          additionalProperties: {
            type: "object",
            properties: { input: { type: "number", minimum: 0 }, output: { type: "number", minimum: 0 }, cacheRead: { type: "number", minimum: 0 }, cacheWrite: { type: "number", minimum: 0 } },
            additionalProperties: false,
          },
        },
      },
      additionalProperties: false,
    };
    const connectionBody = (args, bootstrap, current = {}) => {
      const provider = args.provider || current.provider || "custom";
      const template = (bootstrap.modelCatalog || []).find((item) => item.id === provider) || {};
      return {
        name: args.name || current.name || template.name || provider,
        provider,
        protocol: args.protocol || current.protocol || template.protocol || "openai-chat-completions",
        baseUrl: args.baseUrl || current.baseUrl || template.baseUrl || "",
        defaultModel: args.defaultModel || current.defaultModel || template.models?.[0]?.id || "",
        prices: args.prices || current.prices || {},
      };
    };
    agentCtx.tools.register({
      name: "rank_create_model_connection",
      description: "Create non-secret model connection metadata from conversation. Never request or accept an API key; after creation the user adds the credential and verifies it in the secure model-connection UI.",
      parameters: { ...connectionParameters, required: ["provider"] },
      output: objectOutput,
      execute: async (args, exec) => {
        const bootstrap = await requestJSON(`${this.apiURL}/api/bootstrap`, { signal: exec.signal });
        const connection = await requestJSON(`${this.apiURL}/api/model-connections`, {
          method: "POST", signal: exec.signal, headers: { "content-type": "application/json" }, body: JSON.stringify(connectionBody(args, bootstrap)),
        });
        return { connection, next: "在模型连接中添加 API Key 并验证后即可供 Agent 使用。" };
      },
    });
    agentCtx.tools.register({
      name: "rank_update_model_connection",
      description: "Update non-secret model connection metadata or local model prices. Never request or accept an API key.",
      parameters: { ...connectionParameters, properties: { connectionId: { type: "string" }, ...connectionParameters.properties }, required: ["connectionId"] },
      output: objectOutput,
      execute: async ({ connectionId, ...args }, exec) => {
        const bootstrap = await requestJSON(`${this.apiURL}/api/bootstrap`, { signal: exec.signal });
        const current = (bootstrap.modelConnections || []).find((item) => item.id === connectionId);
        if (!current) throw new Error("模型连接不存在");
        const connection = await requestJSON(`${this.apiURL}/api/model-connections/${encodeURIComponent(connectionId)}`, {
          method: "PATCH", signal: exec.signal, headers: { "content-type": "application/json" }, body: JSON.stringify(connectionBody(args, bootstrap, current)),
        });
        return { connection, next: connection.hasCredential ? "连接配置已更新，请重新验证。" : "请在模型连接中添加 API Key 并验证。" };
      },
    });
    agentCtx.tools.register({
      name: "rank_set_system_model",
      description: "Bind the global Control conversation model or Judge model to an existing verified connection. Never request an API key.",
      parameters: { type: "object", properties: { role: { type: "string", enum: ["control", "judge"] }, connectionId: { type: "string" }, model: { type: "string" } }, required: ["role", "connectionId"], additionalProperties: false },
      output: objectOutput,
      execute: async ({ role, connectionId, model }, exec) => {
        const bootstrap = await requestJSON(`${this.apiURL}/api/bootstrap`, { signal: exec.signal });
        const connection = (bootstrap.modelConnections || []).find((item) => item.id === connectionId);
        if (!connection || connection.status !== "verified") throw new Error("请选择一个已验证的模型连接");
        const binding = await requestJSON(`${this.apiURL}/api/system-models/${role}`, { method: "PUT", signal: exec.signal, headers: { "content-type": "application/json" }, body: JSON.stringify({ connectionId, model: model || connection.defaultModel }) });
        return { binding, applied: role === "control" ? "本轮结束后切换对话模型" : "后续评审立即使用新模型" };
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
      name: "rank_add_dataset_cases",
      description: "Add cases to an existing immutable DatasetVersion by creating and selecting a new version. Never mutate the base version.",
      parameters: {
        type: "object",
        properties: {
          baseDatasetVersionId: { type: "string" },
          cases: {
            type: "array", minItems: 1, maxItems: 200,
            items: {
              type: "object",
              properties: { id: { type: "string" }, title: { type: "string" }, input: { type: "string" }, expected: { type: "object", additionalProperties: true } },
              required: ["input"], additionalProperties: false,
            },
          },
        },
        required: ["baseDatasetVersionId", "cases"],
        additionalProperties: false,
      },
      output: objectOutput,
      execute: (args, exec) => command("add_dataset_cases", args, exec),
    });
    agentCtx.tools.register({
      name: "rank_create_agent",
      description: "Create and select the first immutable version of a Rank Agent configuration.",
      parameters: {
        type: "object",
        properties: {
          name: { type: "string" }, handle: { type: "string" },
          runnerType: { type: "string", enum: ["dsh", "pi", "claude-code", "codex", "hermes", "mock"] },
		  modelConnectionId: { type: "string" },
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
      name: "rank_create_agent_version",
      description: "Create and select a new immutable AgentVersion from an existing version, replacing only the supplied configuration fields.",
      parameters: {
        type: "object",
        properties: {
          baseAgentVersionId: { type: "string" },
          runnerType: { type: "string", enum: ["dsh", "pi", "claude-code", "codex", "hermes", "mock"] },
		  modelConnectionId: { type: "string" },
          model: { type: "string" }, preset: { type: "string" }, systemPrompt: { type: "string" }, description: { type: "string" },
          tools: { type: "array", items: { type: "string" } },
          skills: { type: "array", items: { type: "string" } },
        },
        required: ["baseAgentVersionId"],
        additionalProperties: false,
      },
      output: objectOutput,
      execute: (args, exec) => command("create_agent_version", args, exec),
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
        name: "rank_show_experiment_results",
        description: "Read and present the current experiment's overall performance across every Run and tested Agent version. Use this whenever the user asks to view experiment data, results, performance, history comparison, or previous runs; do not infer absence from conversation text.",
        parameters: { type: "object", properties: {}, additionalProperties: false },
        output: objectOutput,
        execute: (args, exec) => command("show_experiment_results", args, exec),
      });
    agentCtx.tools.register({
        name: "rank_prepare_run",
		description: "Prepare a structured run-confirmation card. This never starts a tested Agent process. When the user names comparison Agents or a repeat count, pass those exact immutable version IDs and trial count so the card opens with that configuration.",
		parameters: {
		  type: "object",
		  properties: {
			agentVersionIds: { type: "array", minItems: 1, maxItems: 4, items: { type: "string" } },
			trialCount: { type: "integer", enum: [1, 5] },
		  },
		  additionalProperties: false,
		},
        output: objectOutput,
        execute: (args, exec) => command("prepare_run", args, exec),
      });
    return { commit() {} };
  }
}
