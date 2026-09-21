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
    return { provider, id: model, name: "AgentKiln deterministic acceptance model", context: { contextWindow: 32_000 } };
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
      if (/打开|设置|配置|open|configure/i.test(text) && embedded?.section) {
        chunks = toolChunks(callId, "rank_open_configuration", embedded);
      } else if (/列出|有哪些|可选|list/i.test(text) && /测试集|dataset|agent/i.test(text)) {
        chunks = toolChunks(callId, "rank_list_assets", {});
      } else if (/新增|添加|追加|add|append/i.test(text) && /用例|case/i.test(text) && embedded?.baseDatasetVersionId && embedded?.cases) {
        chunks = toolChunks(callId, "rank_add_dataset_cases", embedded);
      } else if (/更新|修改|version|update/i.test(text) && embedded?.baseDatasetVersionId) {
        chunks = toolChunks(callId, "rank_create_dataset_version", embedded);
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
      } else if (/创建|新建|create/i.test(text) && /端点|endpoint|部署|deployment/i.test(text) && embedded?.name) {
        chunks = toolChunks(callId, "rank_create_deployment", embedded);
      } else if (/默认|default/i.test(text) && /测试集|dataset/i.test(text) && embedded) {
        chunks = toolChunks(callId, "rank_set_evaluation_defaults", embedded);
      } else if (/门槛|release policy/i.test(text) && embedded?.deploymentId) {
        chunks = toolChunks(callId, "rank_set_release_policy", embedded);
      } else if (/试用|试试|try agent|playground/i.test(text)) {
        chunks = toolChunks(callId, "rank_open_trial", embedded || (agent ? { agentVersionId: agent } : {}));
      } else if (/发布进度|发布状态|release status/i.test(text)) {
        chunks = toolChunks(callId, "rank_release_status", {});
      } else if (/创建|新建|create/i.test(text) && /agent/i.test(text) && embedded?.name) {
        chunks = toolChunks(callId, "rank_create_agent", embedded);
	  } else if (/发布|上线|publish|release/i.test(text)) {
		chunks = toolChunks(callId, "rank_prepare_release", embedded || {});
	  } else if (/准备运行|准备评测|确认运行|开始运行|我要运行|run/i.test(text) && embedded?.agentVersionIds) {
		chunks = toolChunks(callId, "rank_prepare_run", embedded);
      } else if (dataset && /测试集|dataset/i.test(text)) {
        chunks = toolChunks(callId, "rank_select_dataset", { datasetVersionId: dataset });
      } else if (agent && /agent/i.test(text)) {
        chunks = toolChunks(callId, "rank_select_agent", { agentVersionId: agent });
      } else if (/(?:多个|多种|同时|并行).{0,8}agent|agent.{0,8}(?:多个|同时|并行)/i.test(text)) {
		chunks = textChunks(rankMessage(
		  { type: "summary", text: "支持多 Agent 对比" },
		  { type: "paragraph", text: "在运行确认卡中添加 2–4 个 Agent 版本即可；AgentKiln 会创建一个对比组，并为每个 Agent 创建独立 Run。" },
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
  if (!response.ok) throw new Error(payload.error?.message || `AgentKiln API ${response.status}`);
  return payload;
}

export default class RankControl extends Service {
  static inject = ["llm", "tools"];

  async requestJSON(url, options = {}) {
    return requestJSON(url, { ...options, headers: { ...options.headers, "x-rank-control-token": this.controlToken } });
  }

  constructor(ctx, config = {}) {
    super(ctx, "rankControl", true);
    this.apiURL = String(config.rankApiURL || "http://127.0.0.1:8787").replace(/\/$/, "");
    this.controlToken = String(config.controlToken || "");
    this.demo = config.demo === true;
    if (this.demo) ctx.llm.registerAdapter(["rank-demo"], new RankDemoAdapter());
  }

  setupAgent(agentCtx, binding) {
    const command = async (type, payload, exec) => {
      const response = await this.requestJSON(`${this.apiURL}/api/internal/control/commands`, {
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
      description: "List reusable AgentKiln dataset and Agent versions before choosing or creating an asset.",
      parameters: { type: "object", properties: {}, additionalProperties: false },
      output: objectOutput,
      execute: async (_args, exec) => {
		const [bootstrap, experiment] = await Promise.all([
		  this.requestJSON(`${this.apiURL}/api/bootstrap`, { signal: exec.signal }),
		  this.requestJSON(`${this.apiURL}/api/experiments/${binding.experimentId}`, { signal: exec.signal }),
		]);
		const evaluationDefaults = Object.fromEntries(await Promise.all(bootstrap.agents.map(async agent => [agent.familyId, await this.requestJSON(`${this.apiURL}/api/agent-families/${encodeURIComponent(agent.familyId)}/evaluation-defaults`, { signal: exec.signal })])));
        return {
		  evaluationDefaults,
		  currentExperiment: { id: experiment.id, datasetVersionId: experiment.datasetVersionId || "", agentVersionId: experiment.agentVersionId || "", dataset: experiment.dataset || null, agent: experiment.agent || null },
          datasets: bootstrap.datasets.map((dataset) => ({ id: dataset.id, familyId: dataset.familyId, name: dataset.name, version: dataset.version, caseCount: dataset.caseCount, source: dataset.source })),
		  agents: bootstrap.agents.map((agent) => ({ id: agent.id, familyId: agent.familyId, handle: agent.handle, version: agent.version, runnerType: agent.runnerType, model: agent.model, preset: agent.preset || "", tools: agent.tools || [], skills: agent.skills || [], available: agent.runtime?.available === true, unavailableReason: agent.runtime?.reason || "" })),
		  modelConnections: (bootstrap.modelConnections || []).map((connection) => ({ id: connection.id, name: connection.name, provider: connection.provider || "custom", protocol: connection.protocol, baseUrl: connection.baseUrl, defaultModel: connection.defaultModel || "", models: connection.models || [], prices: connection.prices || {}, status: connection.status })),
		  modelCatalog: bootstrap.modelCatalog || [],
		  systemModels: bootstrap.systemModels || [],
		  deployments: (bootstrap.deployments || []).map((deployment) => ({ id: deployment.id, name: deployment.name, agentFamilyId: deployment.agentFamilyId, aliases: deployment.aliases.map((alias) => ({ name: alias.name, agentVersionId: alias.currentRelease?.agentVersionId || "" })) })),
        };
      },
    });
    const connectionParameters = {
      type: "object",
      properties: {
        name: { type: "string" },
        provider: { type: "string", description: "Provider id from the AgentKiln model catalog, for example deepseek or custom." },
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
        const bootstrap = await this.requestJSON(`${this.apiURL}/api/bootstrap`, { signal: exec.signal });
        const connection = await this.requestJSON(`${this.apiURL}/api/model-connections`, {
          method: "POST", signal: exec.signal, headers: { "content-type": "application/json" }, body: JSON.stringify(connectionBody(args, bootstrap)),
        });
        await command("open_configuration", { section: "model_connections", connectionId: connection.id }, exec);
        return { connection, next: "在会话中的连接表单添加 API Key 并验证。" };
      },
    });
    agentCtx.tools.register({
      name: "rank_update_model_connection",
      description: "Update non-secret model connection metadata or local model prices. Never request or accept an API key.",
      parameters: { ...connectionParameters, properties: { connectionId: { type: "string" }, ...connectionParameters.properties }, required: ["connectionId"] },
      output: objectOutput,
      execute: async ({ connectionId, ...args }, exec) => {
        const bootstrap = await this.requestJSON(`${this.apiURL}/api/bootstrap`, { signal: exec.signal });
        const current = (bootstrap.modelConnections || []).find((item) => item.id === connectionId);
        if (!current) throw new Error("模型连接不存在");
        const connection = await this.requestJSON(`${this.apiURL}/api/model-connections/${encodeURIComponent(connectionId)}`, {
          method: "PATCH", signal: exec.signal, headers: { "content-type": "application/json" }, body: JSON.stringify(connectionBody(args, bootstrap, current)),
        });
        await command("open_configuration", { section: "model_connections", connectionId: connection.id }, exec);
        return { connection, next: "连接配置已更新，请在会话中的安全表单验证。" };
      },
    });
    agentCtx.tools.register({
      name: "rank_set_system_model",
      description: "Bind the global Control conversation model or Judge model to an existing verified connection. Never request an API key.",
      parameters: { type: "object", properties: { role: { type: "string", enum: ["control", "judge"] }, connectionId: { type: "string" }, model: { type: "string" } }, required: ["role", "connectionId"], additionalProperties: false },
      output: objectOutput,
      execute: async ({ role, connectionId, model }, exec) => {
        const bootstrap = await this.requestJSON(`${this.apiURL}/api/bootstrap`, { signal: exec.signal });
        const connection = (bootstrap.modelConnections || []).find((item) => item.id === connectionId);
        if (!connection || connection.status !== "verified") throw new Error("请选择一个已验证的模型连接");
        const binding = await this.requestJSON(`${this.apiURL}/api/system-models/${role}`, { method: "PUT", signal: exec.signal, headers: { "content-type": "application/json" }, body: JSON.stringify({ connectionId, model: model || connection.defaultModel }) });
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
      name: "rank_create_dataset_version",
      description: "Create and select a new immutable dataset version, preserving omitted fields. Replace the full cases array to edit or remove cases; use rank_add_dataset_cases for additions. Read the base dataset before changing cases. Rubric, schema and description can be changed independently without resending cases.",
      parameters: { type: "object", properties: {
        baseDatasetVersionId: { type: "string" }, description: { type: "string" },
        rubric: { type: "object", additionalProperties: true }, schema: { type: "object", additionalProperties: true },
        cases: { type: "array", minItems: 1, maxItems: 200, items: { type: "object", properties: { id: { type: "string" }, title: { type: "string" }, input: { type: "string" }, expected: { type: "object", additionalProperties: true } }, required: ["input"], additionalProperties: false } },
      }, required: ["baseDatasetVersionId"], additionalProperties: false },
      output: objectOutput,
      execute: (args, exec) => command("create_dataset_version", args, exec),
    });
    agentCtx.tools.register({
      name: "rank_set_release_policy",
      description: "Update an endpoint's numeric publication thresholds for future releases. Omitted fields stay unchanged (new defaults: 100% pass, 100% valid coverage, 24h). This does not publish a version or change already running release checks. Use rank_open_configuration with release_policy to review fields when requested.",
      parameters: { type: "object", properties: {
        deploymentId: { type: "string" }, minPassRate: { type: "integer", minimum: 0, maximum: 100 }, minValidTrialRate: { type: "integer", minimum: 1, maximum: 100 }, maxEvidenceAgeHours: { type: "integer", minimum: 1 },
      }, required: ["deploymentId"], additionalProperties: false },
      output: objectOutput,
      execute: (args, exec) => command("set_release_policy", args, exec),
    });
    agentCtx.tools.register({
      name: "rank_create_agent",
      description: "Create and select the first immutable version of a AgentKiln Agent configuration.",
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
        description: "Select one existing immutable DatasetVersion for the current AgentKiln experiment.",
        parameters: {
          type: "object",
          properties: { datasetVersionId: { type: "string", description: "Exact DatasetVersion id from AgentKiln." } },
          required: ["datasetVersionId"],
          additionalProperties: false,
        },
        output: objectOutput,
        execute: (args, exec) => command("select_dataset", args, exec),
      });
    agentCtx.tools.register({
        name: "rank_select_agent",
        description: "Select one existing immutable AgentVersion for the current AgentKiln experiment.",
        parameters: {
          type: "object",
          properties: { agentVersionId: { type: "string", description: "Exact AgentVersion id from AgentKiln." } },
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
    agentCtx.tools.register({
      name: "rank_open_configuration",
      description: "Present a durable in-conversation configuration action. Opens the actual secure form without leaving chat: model_connections (credential entry, verify, delete, pricing), local_connections (discover/import), system_models, preferences (language and companion), api_keys (create/revoke/permissions), webhooks (create/enable/disable), release_policy (publication thresholds), endpoint_integration (select/create key and executable examples). Use deploymentId from assets for endpoint sections and optional connectionId for an exact model connection. Never accept or return secrets. Use direct mutation tools when the user already supplied non-secret values; use this for secrets, browsing and reviewing settings. Opening a form does not mean a change has been saved.",
      parameters: { type: "object", properties: {
        section: { type: "string", enum: ["model_connections", "local_connections", "system_models", "preferences", "api_keys", "webhooks", "release_policy", "endpoint_integration"] },
        deploymentId: { type: "string" }, connectionId: { type: "string" },
      }, required: ["section"], additionalProperties: false },
      output: objectOutput,
      execute: (args, exec) => command("open_configuration", args, exec),
    });
    agentCtx.tools.register({
      name: "rank_open_trial",
        description: "Open a separate Agent playground conversation to try the Agent directly. Use when users ask to try or chat with an Agent. No dataset or evaluation is required. Returns an actionable handoff; never impersonate the tested Agent or execute a trial from control chat. Draft can carry the user's original message; exclude secrets.",
        parameters: { type: "object", properties: { agentVersionId: { type: "string" }, draft: { type: "string" } }, additionalProperties: false },
        output: objectOutput,
        execute: (args, exec) => command("open_trial", args, exec),
      });
    agentCtx.tools.register({
        name: "rank_prepare_release",
		description: "Prepare a release-confirmation card comparing an endpoint's current version with a candidate immutable Agent version. Use separate endpoints for environments. Call these serving targets endpoints (端点) in replies. This never publishes by itself.",
		parameters: {
		  type: "object",
		  properties: {
			deploymentId: { type: "string", description: "Deployment id from rank_list_assets. Omit when the selected Agent family has one Deployment." },
			agentVersionId: { type: "string", description: "Candidate immutable Agent version. Defaults to the experiment selection." },
			developmentDatasetVersionId: { type: "string", description: "Optional exact development DatasetVersion for this review; omission resolves the Agent default." },
			regressionDatasetVersionId: { type: "string", description: "Optional exact regression DatasetVersion for this release; omission resolves the Agent default." },
		  },
		  additionalProperties: false,
		},
        output: objectOutput,
        execute: (args, exec) => command("prepare_release", args, exec),
      });
    agentCtx.tools.register({
      name: "rank_create_deployment",
      description: "Create a named endpoint for an Agent family with immutable execution limits. Omit agentFamilyId to use the selected Agent. Defaults: 60 requests/minute, 4 concurrent responses, 120000ms timeout. Ask only for a name if missing; then prepare release for confirmation.",
      parameters: { type: "object", properties: { name: { type: "string", pattern: "^[a-z][a-z0-9-]{0,62}$" }, agentFamilyId: { type: "string" }, requestsPerMinute: { type: "integer", minimum: 1 }, maxConcurrentResponses: { type: "integer", minimum: 1 }, executionTimeoutMs: { type: "integer", minimum: 1 } }, required: ["name"], additionalProperties: false },
      output: objectOutput,
      execute: (args, exec) => command("create_deployment", args, exec),
    });
    agentCtx.tools.register({
      name: "rank_set_evaluation_defaults",
      description: "Set optional default development and regression Dataset families shared by all versions of an Agent. Omitted fields stay unchanged; empty string clears a default. Dataset remains generic. Use familyId from assets, never a dataset version ID.",
      parameters: { type: "object", properties: { agentFamilyId: { type: "string" }, developmentDatasetId: { type: "string" }, regressionDatasetId: { type: "string" } }, additionalProperties: false },
      output: objectOutput,
      execute: (args, exec) => command("set_evaluation_defaults", args, exec),
    });
    agentCtx.tools.register({
      name: "rank_release_status",
      description: "Read durable release pipeline progress and results for this conversation.",
      parameters: { type: "object", properties: {}, additionalProperties: false },
      output: objectOutput,
      execute: (args, exec) => command("release_status", args, exec),
    });
    return { commit() {} };
  }
}
