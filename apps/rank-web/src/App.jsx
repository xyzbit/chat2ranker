import { useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowLeft,
  ArrowRight,
  ArrowsClockwise,
  ArrowsInSimple,
  ArrowsOutSimple,
  CaretDown,
  ChartLineUp,
  Check,
  CheckCircle,
  CircleNotch,
  ClockCounterClockwise,
  Coins,
  Copy,
  Database,
  FileArrowUp,
  GitBranch,
  PaperPlaneRight,
  PencilSimple,
  Play,
  Plus,
  Robot,
  SidebarSimple,
  Smiley,
  Stop,
  Timer,
  User,
  WarningCircle,
  X,
} from "@phosphor-icons/react";
import { rankApi } from "./api.js";
import { parseImportedCases } from "./import-cases.js";
import { messageContentToMarkdown, parseMessageContent } from "./message-format.js";
import { agentConversationPrompt, experimentTitleFromMessage } from "./conversation-config.js";
import { filterMentionItems, mentionSearchQuery } from "./mention-search.js";
import { ModelConnectionCenter } from "./ModelConnectionCenter.jsx";

const ACTIVE_STATES = new Set(["queued", "preparing", "running", "scoring"]);
const runnerLabels = { dsh: "DeepSeek Harness", pi: "Pi", "claude-code": "Claude Code", codex: "Codex", hermes: "Hermes", mock: "Demo" };
const defaultModelSources = { dsh: "运行环境 DeepSeek", pi: "Pi Adapter 配置", "claude-code": "Execution Service · Claude Code 登录", codex: "Execution Service · Codex 登录", hermes: "Hermes 运行环境", mock: "Demo 内置模型" };

function modelSource(item, connections = []) {
  return connections.find((connection) => connection.id === item?.modelConnectionId)?.name || defaultModelSources[item?.runnerType] || "运行环境默认";
}

function modelSourceHint(runnerType, custom) {
  if (custom) return "连接已由 Execution Service 验证，密钥不会写入 Agent 或运行记录。";
  return ({
    dsh: "使用 Execution Service 的 DeepSeek 凭据。",
    codex: "复用 Execution Service 用户的 Codex 登录；任务配置保持隔离。",
    "claude-code": "复用 Execution Service 用户的 Claude Code 登录；任务不持久化 Session。",
    hermes: "使用 Hermes 所在运行环境的默认凭据。",
    pi: "模型和凭据由部署的 Pi Adapter 决定。",
    mock: "无需模型凭据，仅用于验证评测流程。",
  })[runnerType] || "由 Runner 所在运行环境提供。";
}

function SystemModelSetup({ catalog, connections, onComplete }) {
  const verified = connections.filter((item) => item.status === "verified");
  const [existingId, setExistingId] = useState(verified[0]?.id || "");
  const [provider, setProvider] = useState(catalog[0]?.id || "");
  const template = catalog.find((item) => item.id === provider) || catalog[0];
  const [model, setModel] = useState(template?.models[0]?.id || "");
  const [apiKey, setAPIKey] = useState("");
  const [adding, setAdding] = useState(!verified.length);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  function chooseProvider(value) {
    const next = catalog.find((item) => item.id === value);
    setProvider(value); setModel(next?.models[0]?.id || "");
  }

  async function submit(event) {
    event.preventDefault(); setBusy(true); setError("");
    try {
      let connection = verified.find((item) => item.id === existingId);
      if (adding) {
        connection = await rankApi.createModelConnection({ name: template.name, provider: template.id, protocol: template.protocol, baseUrl: template.baseUrl, apiKey, defaultModel: model });
        connection = await rankApi.verifyModelConnection(connection.id);
      }
      const selectedModel = adding ? model : connection.defaultModel;
      await rankApi.setSystemModel("control", { connectionId: connection.id, model: selectedModel });
      await rankApi.setSystemModel("judge", { connectionId: connection.id, model: selectedModel });
      await onComplete();
    } catch (setupError) { setError(setupError.message); }
    finally { setBusy(false); }
  }

  return <main className="system-setup"><form onSubmit={submit}>
    <span className="eyebrow">WELCOME TO RANK</span><h1>连接一个模型</h1><p>用于对话与评审。之后可分别切换，不会影响已完成的实验。</p>
    {!adding && <label><span>已有连接</span><select value={existingId} onChange={(event) => setExistingId(event.target.value)}>{verified.map((item) => <option key={item.id} value={item.id}>{item.name} · {item.defaultModel}</option>)}</select></label>}
    {adding && <><label><span>Provider</span><select value={provider} onChange={(event) => chooseProvider(event.target.value)}>{catalog.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label><label><span>模型</span><select value={model} onChange={(event) => setModel(event.target.value)}>{template?.models.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label><label><span>API Key</span><input type="password" autoComplete="new-password" required value={apiKey} onChange={(event) => setAPIKey(event.target.value)} placeholder="只保存在本机 Execution Service" /></label></>}
    {error && <div className="setup-error"><WarningCircle size={16} />{error}</div>}
    <button className="primary-action" type="submit" disabled={busy || (adding ? !apiKey || !model : !existingId)}>{busy ? <CircleNotch className="spin" size={17} /> : <CheckCircle size={17} />}验证并开始</button>
    {verified.length > 0 && <button className="setup-switch" type="button" onClick={() => setAdding((value) => !value)}>{adding ? "使用已有连接" : "添加新连接"}</button>}
    <small>Base URL 与默认价格来自内置官方目录；高级配置进入应用后再修改。</small>
  </form></main>;
}

function formatTime(value, withDate = false) {
  if (!value) return "";
  const date = new Date(value);
  return new Intl.DateTimeFormat("zh-CN", withDate
    ? { month: "numeric", day: "numeric", hour: "2-digit", minute: "2-digit" }
    : { hour: "2-digit", minute: "2-digit" }).format(date);
}

function formatDuration(ms = 0) {
  if (ms < 60_000) return `${Math.max(1, Math.round(ms / 1000))}s`;
  const minutes = Math.floor(ms / 60_000);
  return `${minutes}m ${Math.round((ms % 60_000) / 1000)}s`;
}

function formatCost(value) {
  const amount = Number(value);
  if (!Number.isFinite(amount)) return "—";
  if (amount > 0 && amount < 0.0001) return `$${amount.toFixed(6)}`;
  if (amount > 0 && amount < 0.01) return `$${amount.toFixed(4)}`;
  return `$${amount.toFixed(2)}`;
}

function displayedCost(item) {
  const amount = Number(item?.cost);
  if (!Number.isFinite(amount)) return "—";
  if (item?.costKnown === false) return "缺少计价配置";
  return `${item?.costEstimated ? "≈" : ""}${formatCost(amount)}`;
}

function displayedCostLabel(item) {
  if (item?.costKnown === false) return "成本";
  return item?.costEstimated ? "估算成本" : "总成本";
}

function a2uiPayload(event) {
	if (event?.payload && typeof event.payload === "object") return event.payload;
	try { return JSON.parse(event?.payload || "{}"); } catch { return {}; }
}

function eventLabel(type) {
  const labels = {
    "run.created": "创建运行", "run.status": "运行状态", "run.recovered": "恢复运行", "run.completed": "运行完成",
    "trial.started": "开始 Trial", "trial.retry": "重试 Trial", "trial.completed": "Trial 完成", "trial.invalid": "Trial 无效",
    "case.started": "开始用例", "candidate.queued": "Agent 排队", "candidate.running": "Agent 启动", "candidate.harness.started": "Harness 启动",
    "candidate.harness.output": "Agent 输出", "candidate.harness.stdout": "Agent 输出", "candidate.harness.stderr": "Agent 日志", "candidate.completed": "Agent 完成", "candidate.failed": "Agent 失败",
    "judge.queued": "评分排队", "judge.running": "评分启动", "judge.harness.started": "Judge 启动", "judge.harness.output": "Judge 输出",
    "judge.harness.stdout": "Judge 输出", "judge.harness.stderr": "Judge 日志", "judge.completed": "评分执行完成", "judge.failed": "评分失败", "judge.verdict": "评分结论",
    "artifact.available": "产物可用", "case.completed": "用例完成", "agent.message": "Agent 消息",
  };
  return labels[type] || type;
}

function eventDetail(event) {
  if (!event?.reason) return event?.caseId || event?.status || "";
  if (event.type.endsWith(".stderr") && /Reading additional input from stdin/i.test(event.reason)) return event.caseId || "";
  if (event.type.endsWith(".stdout")) {
    try {
      const value = JSON.parse(event.reason);
      if (value.type === "item.completed" && value.item?.type === "agent_message") return value.item.text;
      if (value.type === "turn.completed") return "模型响应完成";
      return event.caseId || "";
    } catch { return event.caseId || ""; }
  }
  return `${event.caseId || event.status || ""}${event.reason ? `${event.caseId || event.status ? " · " : ""}${event.reason}` : ""}`;
}

function Avatar({ role = "assistant" }) {
  return (
    <span className={`avatar avatar-${role}`} aria-hidden="true">
      {role === "assistant" ? <Smiley size={21} /> : <User size={20} />}
    </span>
  );
}

function InlineText({ text }) {
  return String(text).split(/(\*\*[^*]+\*\*|`[^`]+`|\[[^\]]+\]\(https?:\/\/[^)\s]+\))/g).filter(Boolean).map((part, index) => {
    if (part.startsWith("**") && part.endsWith("**")) return <strong key={index}>{part.slice(2, -2)}</strong>;
    if (part.startsWith("`") && part.endsWith("`")) return <code key={index}>{part.slice(1, -1)}</code>;
    const link = part.match(/^\[([^\]]+)]\((https?:\/\/[^)\s]+)\)$/);
    return link ? <a key={index} href={link[2]} target="_blank" rel="noreferrer">{link[1]}</a> : part;
  });
}

function MessageContent({ content, streaming = false, onRetry }) {
  const parsed = parseMessageContent(content, { streaming });
  return <div className={`message-content ${parsed.format}`}>{parsed.blocks.map((block, index) => {
    if (block.type === "summary") return <p className="message-summary" key={index}><InlineText text={block.text} /></p>;
    if (block.type === "paragraph") return <p key={index}><InlineText text={block.text} /></p>;
    if (block.type === "note") return <aside className={`message-note ${block.tone}`} key={index}><InlineText text={block.text} /></aside>;
    if (block.type === "list") {
      const List = block.ordered ? "ol" : "ul";
      return <List key={index}>{block.items.map((item, itemIndex) => <li key={itemIndex}><InlineText text={item} /></li>)}</List>;
    }
    if (block.type === "facts") return <dl className="message-facts" key={index}>{block.items.map((item, itemIndex) => <div key={itemIndex}><dt>{item.label}</dt><dd><InlineText text={item.value} /></dd></div>)}</dl>;
    if (block.type === "error") return <aside className="message-error" key={index}><div><WarningCircle size={15} /><strong>{block.text}</strong>{onRetry && <button type="button" onClick={onRetry}>重试</button>}</div>{block.detail && <details><summary>技术信息</summary><code>{block.detail}</code></details>}</aside>;
    if (block.type === "table") return <div className="message-table" key={index}><table><thead><tr>{block.headers.map((cell, cellIndex) => <th key={cellIndex}><InlineText text={cell} /></th>)}</tr></thead><tbody>{block.rows.map((row, rowIndex) => <tr key={rowIndex}>{row.map((cell, cellIndex) => <td key={cellIndex}><InlineText text={cell} /></td>)}</tr>)}</tbody></table></div>;
    return <pre key={index} data-language={block.language}><code>{block.code}</code></pre>;
  })}</div>;
}

async function writeClipboard(text) {
  const modern = navigator.clipboard?.writeText ? await navigator.clipboard.writeText(text).then(() => true, () => false) : false;
  if (modern) return true;
  const input = document.createElement("textarea");
  input.value = text;
  input.style.cssText = "position:fixed;opacity:0;pointer-events:none";
  document.body.append(input);
  try {
    input.select();
    return document.execCommand("copy");
  } catch { return false; }
  finally { input.remove(); }
}

function Message({ message, onRetry }) {
  const [copyState, setCopyState] = useState("idle");
  async function copyMessage() {
    setCopyState(await writeClipboard(messageContentToMarkdown(message.content)) ? "copied" : "failed");
    setTimeout(() => setCopyState("idle"), 4_000);
  }
  return (
    <article className={`message-row ${message.role}`}>
      {message.role === "assistant" && <Avatar />}
      <div className="message-stack">
        <div className="message-bubble">{message.role === "assistant" ? <MessageContent content={message.content} onRetry={onRetry} /> : message.content}</div>
        <div className="message-meta"><time className="message-time">{formatTime(message.createdAt)}{message.pending ? " · 发送中" : message.failed ? " · 发送失败" : ""}</time>{message.role === "assistant" && <button type="button" className={copyState} onClick={copyMessage} aria-label="复制为 Markdown" title="复制为 Markdown" aria-live="polite">{copyState === "copied" ? <Check size={12} /> : copyState === "failed" ? <WarningCircle size={12} /> : <Copy size={12} />}{copyState === "copied" ? "已复制为 Markdown" : copyState === "failed" ? "复制失败" : "复制"}</button>}</div>
      </div>
      {message.role === "user" && <Avatar role="user" />}
    </article>
  );
}

function A2UIRow({ children }) {
  return (
    <div className="a2ui-row">
      <Avatar />
      <div className="a2ui-content">{children}</div>
    </div>
  );
}

function ExperimentChart({ runs, onInspect, compact = false }) {
  const hasCost = (run) => run.costKnown !== false;
  const [metric, setMetric] = useState(() => runs.some((run) => run.status === "complete" && hasCost(run)) ? "cost" : "latency");
  const complete = runs.filter((run) => run.status === "complete");
  const points = complete.filter((run) => metric === "cost" ? hasCost(run) : run.durationMs > 0);
  const missing = complete.length - points.length;
  const valueOf = (run) => metric === "cost" ? Number(run.cost) : run.durationMs;
  const rawMax = Math.max(...points.map(valueOf), 0);
  const max = rawMax > 0 ? rawMax * 1.08 : metric === "cost" ? 0.01 : 1_000;
  const x = (run) => 50 + (valueOf(run) / max) * 448;
  const y = (run) => 18 + ((100 - run.passRate) / 100) * 154;
  const xLabel = (value) => metric === "cost" ? formatCost(value) : value === 0 ? "0s" : formatDuration(value);
  return (
    <section className={`experiment-chart ${compact ? "compact" : ""}`} aria-label="实验表现图表">
      <header>
        <div><ChartLineUp size={18} /><span><strong>实验表现</strong><small>{complete.length} 次完成运行</small></span></div>
        <div className="chart-switch" aria-label="横轴指标">
          <button type="button" className={metric === "cost" ? "selected" : ""} onClick={() => setMetric("cost")}><Coins size={13} />成本</button>
          <button type="button" className={metric === "latency" ? "selected" : ""} onClick={() => setMetric("latency")}><Timer size={13} />时延</button>
        </div>
      </header>
      {points.length ? <>
        <div className="chart-canvas">
          <svg viewBox="0 0 520 210" role="img" aria-label={`横轴为${metric === "cost" ? "成本" : "时延"}，纵轴为通过率`}>
            {[0, 25, 50, 75, 100].map((rate) => {
              const top = 18 + ((100 - rate) / 100) * 154;
              return <g key={rate}><line x1="50" y1={top} x2="498" y2={top} /><text x="42" y={top + 3} textAnchor="end">{rate}%</text></g>;
            })}
            {[0, .5, 1].map((ratio) => <text key={ratio} x={50 + ratio * 448} y="197" textAnchor={ratio === 0 ? "start" : ratio === 1 ? "end" : "middle"}>{xLabel(max * ratio)}</text>)}
            {points.map((run) => {
              const metricValue = metric === "cost" ? displayedCost(run) : xLabel(valueOf(run));
              return <g key={run.id} className="chart-point" role="button" tabIndex="0" aria-label={`${run.agentSnapshot?.handle || "Agent"}，通过率 ${run.passRate}%，${metric === "cost" ? "成本" : "时延"} ${metricValue}`} onClick={() => onInspect(run)} onKeyDown={(event) => (event.key === "Enter" || event.key === " ") && onInspect(run)}>
              <circle cx={x(run)} cy={y(run)} r="7" />
              <text className="point-label" x={x(run)} y={y(run) - 12} textAnchor="middle">{run.passRate}%</text>
              <title>{run.agentSnapshot?.handle || "Agent"} · {run.passRate}% · {metricValue}</title>
              </g>;
            })}
          </svg>
        </div>
        <footer><span>越靠上通过率越高；越靠左{metric === "cost" ? "成本越低" : "响应越快"}</span>{missing > 0 && <em>{missing} 次{metric === "cost" ? "缺少计价配置" : "未提供时延"}</em>}</footer>
      </> : <div className="chart-empty"><strong>暂无可绘制的{metric === "cost" ? "成本" : "时延"}数据</strong><span>完成运行并返回指标后会自动出现。</span></div>}
    </section>
  );
}

function ReadyCard({ experiment, agents, trialCount, onTrialCount, onPickDataset, onPickAgent, onSwitchDataset, onSelectAgents, onStart, busy }) {
	const unavailable = agents.find((agent) => agent.runtime && !agent.runtime.available);
	const estimate = agents.every((agent) => agent.runnerType === "mock") ? `预计 $${(experiment.dataset.caseCount * trialCount * agents.length * 0.034).toFixed(2)}` : "按运行时实际计费";
  return (
    <A2UIRow>
      <section className="ready-card" aria-label="确认运行">
        <header>
          <div><small>运行快照</small><h2>可以开始了</h2></div>
          <span className="ready-mark"><CheckCircle size={18} weight="fill" /> 已就绪</span>
        </header>
        <div className="snapshot-list">
          <div className="snapshot-item">
            <button type="button" className="snapshot-main" onClick={onPickDataset}>
              <Database size={18} /><span><small>测试集</small><strong>{experiment.dataset.name} v{experiment.dataset.version}</strong></span><em>{experiment.dataset.caseCount} 条</em>
            </button>
            <button type="button" className="asset-quick-switch" onClick={onSwitchDataset} title="切换测试集" aria-label="切换测试集"><ArrowsClockwise size={15} /></button>
          </div>
          <div className="snapshot-item snapshot-agents">
            <div className="snapshot-agent-head">
              <span><Robot size={18} /><span><small>Agent</small><strong>{agents.length > 1 ? `${agents.length} 个版本参与对比` : "已选择 1 个版本"}</strong></span></span>
              <button type="button" className="asset-quick-switch" onClick={onSelectAgents} title="选择 Agent" aria-label="选择 Agent"><Plus size={15} /></button>
            </div>
            <div className={`snapshot-agent-list ${agents.length > 2 ? "overflowing" : ""}`}>
              {agents.map((agent) => <button type="button" key={agent.id} onClick={() => onPickAgent(agent)}>
                <span><strong>{agent.handle} v{agent.version}</strong><small>{agent.runnerType} · {agent.model || "默认模型"}</small></span><ArrowRight size={13} />
              </button>)}
            </div>
          </div>
        </div>
        <div className="trial-choice" aria-label="每个用例运行次数">
          <span><strong>重复运行</strong><small>独立上下文，避免偶然结果</small></span>
          <div>
            <button type="button" className={trialCount === 1 ? "selected" : ""} onClick={() => onTrialCount(1)}>快速 · 1 次</button>
            <button type="button" className={trialCount === 5 ? "selected" : ""} onClick={() => onTrialCount(5)}>可靠 · 5 次</button>
          </div>
        </div>
        {unavailable && <div className="runtime-warning"><WarningCircle size={17} />{unavailable.handle}：{unavailable.runtime.reason}</div>}
        <footer>
          <span><Coins size={16} /> {estimate} · {experiment.dataset.caseCount * trialCount * agents.length} 个 Trial · {agents.length > 1 ? `${agents.length} 个独立 Run` : "并发 5"}</span>
          <button type="button" className="primary-action" onClick={onStart} disabled={busy || unavailable}>
            {busy ? <CircleNotch size={18} className="spin" /> : <Play size={18} weight="fill" />}
            {busy ? "正在创建" : agents.length > 1 ? "开始对比" : "开始运行"}
          </button>
        </footer>
      </section>
    </A2UIRow>
  );
}

function RunSteps({ run }) {
  const states = ["prepare", "run", "score"];
  const activeIndex = run.status === "queued" || run.status === "preparing" ? 0 : run.status === "running" ? 1 : 2;
  const complete = run.status === "complete";
  const multiTrial = Number(run.scheduledTrials) > 0;
  const completed = multiTrial ? (run.completedTrials || 0) : run.results.length;
  const scheduled = multiTrial ? (run.scheduledTrials || run.total) : run.total;
  const labels = ["准备数据", "Agent 运行", "评分"];
  return (
    <div className="run-steps">
      <div className={`step-line ${complete ? "complete" : ""}`} aria-hidden="true" />
      {states.map((state, index) => {
        const done = complete || index < activeIndex;
        const active = !complete && index === activeIndex && ACTIVE_STATES.has(run.status);
        return (
          <div className="step" key={state}>
            <span className={`step-dot ${done ? "done" : ""} ${active ? "active" : ""}`}>
              {done ? <Check size={12} weight="bold" /> : active ? <CircleNotch size={12} className="spin" /> : null}
            </span>
            <strong>{labels[index]}</strong>
            <time>{done ? "完成" : active ? (index === 1 ? `${completed}/${scheduled}` : "进行中") : "待开始"}</time>
          </div>
        );
      })}
    </div>
  );
}

function RunCard({ run, onCancel, onInspect }) {
  const multiTrial = Number(run.scheduledTrials) > 0;
  const failures = run.results.filter((item) => multiTrial ? !item.reliable : !item.passed);
  const complete = run.status === "complete";
  const active = ACTIVE_STATES.has(run.status);
  const scheduled = multiTrial ? (run.scheduledTrials || run.total) : run.total;
  const completedTrials = multiTrial ? (run.completedTrials || 0) : run.results.length;
  const progress = scheduled ? Math.round((completedTrials / scheduled) * 100) : 0;
  const latestProgress = [...run.events].reverse().find((event) => event.caseId || event.status || eventDetail(event));
  const statusLabel = { queued: "排队中", preparing: "准备中", running: "运行中", scoring: "评分中", complete: "完成", failed: "失败", cancelled: "已停止" }[run.status] || run.status;
  return (
    <A2UIRow>
      <div className="run-result-stack">
        <section className={`run-card status-${run.status}`} aria-live="polite">
          <header className="run-card-head">
            <div className="run-card-title"><span><small>#{run.id.slice(-6)}</small>{complete ? `评测已完成 · ${formatTime(run.completedAt, true)}` : `${run.agentSnapshot.handle} 正在执行`}</span><em>{run.datasetSnapshot.name} v{run.datasetSnapshot.version} · {run.caseCount} 个用例 · {run.agentSnapshot.handle} v{run.agentSnapshot.version}</em></div>
            <span className={`status-badge ${complete ? "complete" : run.status}`}>
              {complete ? <CheckCircle size={16} weight="fill" /> : active ? <CircleNotch size={16} className="spin" /> : <WarningCircle size={16} />}
              {statusLabel}
            </span>
          </header>
          {active && <div className="run-progress"><span style={{ width: `${Math.max(4, progress)}%` }} /></div>}
          <div className="metrics">
            <div className="metric"><strong>{complete ? `${run.passed}/${run.total}` : `${completedTrials}/${scheduled}`}</strong><span>{complete ? (multiTrial ? "通过 Trial" : "通过用例") : (multiTrial ? "完成 Trial" : "完成用例")}</span></div>
            <div className="metric"><strong className={complete ? "success" : ""}>{complete ? `${run.passRate}%` : `${progress}%`}</strong><span>{complete ? "通过率" : "进度"}</span></div>
            <div className="metric"><strong>{complete ? (multiTrial ? `${run.reliableCases}/${run.caseCount}` : formatDuration(run.durationMs)) : (multiTrial ? `×${run.trialCount}` : "×1")}</strong><span>{complete ? (multiTrial ? "稳定用例" : "总耗时") : "每用例运行"}</span></div>
            <div className="metric"><strong>{displayedCost(run)}</strong><span>{displayedCostLabel(run)}</span></div>
          </div>
          <RunSteps run={run} />
          {complete && multiTrial && run.evaluationComplete === false && <div className="run-warning"><WarningCircle size={15} />有 {run.infraFailures + run.gradingFailures} 个异常 Trial 未计入质量分母</div>}
          {active && latestProgress && <div className="live-event"><CircleNotch size={13} className="spin" /><code>{eventLabel(latestProgress.type)}</code><span>{eventDetail(latestProgress)}</span></div>}
          {active && <div className="run-controls"><button type="button" onClick={() => onCancel(run.id)}><Stop size={15} weight="fill" />停止运行</button></div>}
          {(run.status === "failed" || run.status === "cancelled") && <div className="run-error">{run.error || "运行已停止"}</div>}
        </section>
        {complete && (
          <div className="result-actions">
            <button type="button" onClick={() => onInspect({ kind: "run", data: run, failuresOnly: true })} disabled={!failures.length}>{failures.length ? `查看 ${failures.length} 个失败` : "全部通过"}<ArrowRight size={15} /></button>
            <button type="button" onClick={() => onInspect({ kind: "run", data: run })}>完整结果<ArrowRight size={15} /></button>
          </div>
        )}
      </div>
    </A2UIRow>
  );
}

const toolLabels = { rank_list_assets: "读取测试集和 Agent", rank_create_dataset: "创建测试集", rank_add_dataset_cases: "创建测试集新版本", rank_create_agent: "创建 Agent", rank_create_agent_version: "创建 Agent 新版本", rank_create_model_connection: "创建模型连接", rank_update_model_connection: "更新模型连接", rank_select_dataset: "选择测试集", rank_select_agent: "选择 Agent", rank_prepare_run: "准备运行", rank_show_experiment_results: "读取实验表现" };
const runStatusLabels = { queued: "排队中", preparing: "准备中", running: "运行中", scoring: "评分中", complete: "完成", failed: "失败", cancelled: "已停止" };

function TurnActivity({ turn }) {
  const draft = turn.events.filter((event) => event.type === "assistant.delta").map((event) => event.text).join("");
  const activity = turn.events.filter((event) => event.type !== "assistant.delta").slice(-3);
  return (
    <A2UIRow>
      <section className="turn-activity" aria-live="polite">
        {activity.map((event) => <div key={event.sequence} className={event.type === "turn.failed" ? "failed" : ""}>
          {event.type !== "tool.completed" && event.type !== "turn.failed" ? <CircleNotch size={13} className="spin" /> : event.type === "tool.completed" ? <Check size={13} /> : <WarningCircle size={13} />}
          <span>{event.label || (event.type === "tool.started" ? `正在${toolLabels[event.name] || `调用 ${event.name}`}…` : event.type === "tool.completed" ? `已完成${toolLabels[event.name] || event.name}` : event.error || "正在处理…")}</span>
        </div>)}
        {draft && <MessageContent content={draft} streaming />}
      </section>
    </A2UIRow>
  );
}

function PanelDetail({ target, modelConnections, onInspect, onAddCase, onEditCase, onEditAgent }) {
  const [artifact, setArtifact] = useState(null);
  useEffect(() => setArtifact(null), [target]);
  const { kind, data } = target;
  async function openArtifact(item) {
    setArtifact({ loading: true, title: item.kind });
    try {
      const value = await rankApi.getArtifact(target.run.id, data.caseId, item.path);
      setArtifact({ title: item.kind, ...value });
    } catch (error) { setArtifact({ title: item.kind, error: error.message }); }
  }
  const results = kind === "run" ? data.results.filter((item) => !target.failuresOnly || !(Number(data.scheduledTrials) > 0 ? item.reliable : item.passed)) : [];
  const candidateExecutions = new Set([data.executionId, ...(data.trials || []).map((item) => item.candidateExecutionId)].filter(Boolean));
  const judgeExecutions = new Set([data.judgeExecutionId, ...(data.trials || []).flatMap((item) => item.judgeExecutionIds || [])].filter(Boolean));
  const artifactRole = (item) => candidateExecutions.has(item.executionId) ? "Agent" : judgeExecutions.has(item.executionId) ? "Judge" : "产物";
  return (
    <>
          {kind === "dataset" && <>
            <section className="detail-summary"><span><small>用例</small><strong>{data.caseCount}</strong></span><span><small>版本</small><strong>v{data.version}</strong></span><span><small>来源</small><strong>{data.source || "—"}</strong></span></section>
            <section><div className="detail-section-head"><h3>用例列表</h3><button type="button" onClick={() => onAddCase(data)}><Plus size={14} />新增用例</button></div><div className="detail-list">{data.cases.map((item, index) => <article key={item.id}><span>{index + 1}</span><div><strong>{item.title}</strong><p>{item.input}</p><small>{Object.values(item.expected || {}).join(" · ")}</small></div><button type="button" className="edit-case" onClick={() => onEditCase(data, item)} aria-label={`编辑用例 ${item.title}`}><PencilSimple size={14} /></button></article>)}</div></section>
          </>}
          {kind === "agent" && <>
            <section className="detail-summary"><span><small>运行方式</small><strong>{runnerLabels[data.runnerType] || data.runnerType}</strong></span><span><small>模型</small><strong>{data.model || "运行环境默认"}</strong></span><span><small>状态</small><strong>{data.runtime?.available ? "可运行" : "未连接"}</strong></span></section>
            <section><div className="detail-section-head"><h3>配置</h3><button type="button" onClick={() => onEditAgent(data)}><PencilSimple size={14} />编辑配置</button></div><dl className="config-list"><dt>模型来源</dt><dd>{modelSource(data, modelConnections)}</dd><dt>Tools</dt><dd>{data.tools?.join(", ") || "—"}</dd><dt>Skills</dt><dd>{data.skills?.join(", ") || "—"}</dd><dt>System Prompt</dt><dd>{data.systemPrompt || "—"}</dd></dl></section>
          </>}
          {kind === "run" && <>
            <section className="run-snapshot-detail"><div><Database size={17} /><span><small>测试集 · {data.caseCount} 个用例</small><strong>{data.datasetSnapshot.name} v{data.datasetSnapshot.version}</strong></span></div><div><Robot size={17} /><span><small>Agent · {runnerLabels[data.agentSnapshot.runnerType] || data.agentSnapshot.runnerType}</small><strong>{data.agentSnapshot.handle} v{data.agentSnapshot.version}</strong></span></div></section>
            <section className="detail-summary"><span><small>{data.status === "complete" ? "通过率" : "状态"}</small><strong>{data.status === "complete" ? `${data.passRate}%` : runStatusLabels[data.status] || data.status}</strong></span><span><small>{displayedCostLabel(data)}</small><strong>{displayedCost(data)}</strong></span><span><small>耗时</small><strong>{formatDuration(data.durationMs)}</strong></span></section>
            <section><h3>{target.failuresOnly ? "失败用例" : "用例结果"}</h3><div className="result-list">{results.map((item) => <button type="button" key={item.caseId} onClick={() => onInspect({ kind: "case", data: item, run: data })}><span className={item.passed ? "pass" : "fail"}>{item.passed ? "通过" : "失败"}</span><div><strong>{item.title}</strong><small>{item.reason}</small></div><ArrowRight size={15} /></button>)}</div></section>
            <section><h3>运行日志</h3><div className="detail-events">{data.events.map((event) => <div key={event.sequence}><time>{formatTime(event.at)}</time><code>{eventLabel(event.type)}</code><span>{event.caseId || event.status || ""}</span></div>)}</div></section>
          </>}
          {kind === "case" && <>
            <section className="detail-summary"><span><small>结果</small><strong>{data.passed ? "通过" : "失败"}</strong></span><span><small>{displayedCostLabel(data)}</small><strong>{displayedCost(data)}</strong></span><span><small>Trial</small><strong>{data.trials?.length || 1}</strong></span></section>
            <section><h3>结论</h3><p className="detail-copy">{data.reason}</p></section>
            {data.trials?.length > 0 && <section><h3>多次运行</h3><div className="trial-list">{data.trials.map((trial) => <article key={trial.id}><strong>#{trial.trialIndex} · {trial.valid ? trial.passed ? "通过" : "失败" : "无效"}</strong><p>{trial.reason}</p></article>)}</div></section>}
            {data.artifacts?.length > 0 && <section><h3>轨迹与产物</h3><div className="artifact-list">{data.artifacts.map((item) => <button type="button" key={item.path} onClick={() => openArtifact(item)}><span>{artifactRole(item)} · {item.kind}</span><small>{item.path.split("/").at(-1)}</small></button>)}</div></section>}
            {artifact && <section className="artifact-content"><h3>{artifact.title}</h3>{artifact.loading ? <span>正在读取…</span> : artifact.error ? <span>{artifact.error}</span> : <pre>{artifact.content}{artifact.truncated ? "\n…内容已截断" : ""}</pre>}</section>}
          </>}
    </>
  );
}

function ExperimentPanel({ experiment, agentRuntime, modelConnections, modelCatalog, systemModels, target, tab, expanded, onTab, onTarget, onClose, onExpand, onPickDataset, onPickAgent, onAddCase, onEditCase, onEditAgent, onBindSystemModel, onSaveConnection, onSyncConnection, onDeleteConnection, onUseConnection }) {
  useEffect(() => {
    const close = (event) => event.key === "Escape" && onClose();
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [onClose]);
  const detailTitle = target?.kind === "dataset" ? `${target.data.name} v${target.data.version}`
    : target?.kind === "agent" ? `${target.data.handle} v${target.data.version}`
      : target?.kind === "run" ? `运行 #${target.data.id.slice(-6)}` : target?.data.title;
  const openTab = (next) => { onTarget(null); onTab(next); };
  return <>
    <button type="button" className="panel-scrim" onClick={onClose} aria-label="关闭实验工作区" />
    <aside id="experiment-panel" className={`experiment-panel ${expanded ? "expanded" : ""}`} aria-label="实验工作区">
      <header className="panel-head">
        <div><small>当前实验</small><h2>{experiment.title}</h2></div>
        <span className="panel-head-actions">
          <button type="button" onClick={onExpand} aria-label={expanded ? "收起宽屏工作区" : "展开宽屏工作区"} title={expanded ? "收起" : "展开"}>{expanded ? <ArrowsInSimple size={18} /> : <ArrowsOutSimple size={18} />}</button>
          <button type="button" onClick={onClose} aria-label="关闭实验工作区"><X size={19} /></button>
        </span>
      </header>
      <div className="panel-tabs" role="tablist" aria-label="实验信息">
        <button type="button" role="tab" aria-selected={tab === "config"} className={tab === "config" ? "selected" : ""} onClick={() => openTab("config")}>配置</button>
        <button type="button" role="tab" aria-selected={tab === "runs"} className={tab === "runs" ? "selected" : ""} onClick={() => openTab("runs")}>运行记录 <span>{experiment.runs.length}</span></button>
        <button type="button" role="tab" aria-selected={tab === "connections"} className={tab === "connections" ? "selected" : ""} onClick={() => openTab("connections")}>模型连接 <span>{modelConnections.length}</span></button>
      </div>
      <div className="panel-body">
        {target ? <>
          <button type="button" className="panel-back" onClick={() => onTarget(null)}><ArrowLeft size={15} />{tab === "config" ? "返回配置" : "返回运行记录"}</button>
          <div className="panel-detail-title"><small>{target.kind === "case" ? "CASE" : target.kind.toUpperCase()}</small><h3>{detailTitle}</h3></div>
          <PanelDetail target={target} modelConnections={modelConnections} onInspect={onTarget} onAddCase={onAddCase} onEditCase={onEditCase} onEditAgent={onEditAgent} />
        </> : tab === "config" ? <>
          <section className="panel-section">
            <div className="panel-section-head"><div><small>测试集</small><h3>{experiment.dataset ? `${experiment.dataset.name} v${experiment.dataset.version}` : "尚未选择"}</h3></div><button type="button" onClick={onPickDataset}>{experiment.dataset ? "切换" : "选择"}</button></div>
            {experiment.dataset ? <button type="button" className="panel-asset" onClick={() => onTarget({ kind: "dataset", data: experiment.dataset })}>
              <Database size={20} /><span><strong>{experiment.dataset.caseCount} 个用例</strong><small>{experiment.dataset.source || "可复用版本化测试集"}</small></span><ArrowRight size={16} />
            </button> : <p className="panel-empty-copy">选择或导入测试集后，可在这里查看全部用例。</p>}
          </section>
          <section className="panel-section">
            <div className="panel-section-head"><div><small>Agent</small><h3>{experiment.agent ? `${experiment.agent.handle} v${experiment.agent.version}` : "尚未选择"}</h3></div><button type="button" onClick={onPickAgent}>{experiment.agent ? "切换" : "选择"}</button></div>
            {experiment.agent ? <button type="button" className="panel-asset" onClick={() => onTarget({ kind: "agent", data: { ...experiment.agent, runtime: agentRuntime } })}>
              <Robot size={20} /><span><strong>{experiment.agent.runnerType} · {experiment.agent.model || "默认模型"}</strong><small>{experiment.agent.tools?.length || 0} 个工具 · {experiment.agent.skills?.length || 0} 个 Skill</small></span><ArrowRight size={16} />
            </button> : <p className="panel-empty-copy">选择 Agent 后，可在这里检查 Runner、模型和指令。</p>}
          </section>
          <p className="panel-version-note"><GitBranch size={14} />运行会冻结当前测试集和 Agent 版本。</p>
        </> : tab === "connections" ? <ModelConnectionCenter connections={modelConnections} catalog={modelCatalog} systemModels={systemModels} onBind={onBindSystemModel} onSave={onSaveConnection} onSync={onSyncConnection} onDelete={onDeleteConnection} onUse={onUseConnection} /> : <section className="panel-section run-history-section">
          {experiment.runs.some((run) => run.status === "complete") && <ExperimentChart compact runs={experiment.runs} onInspect={(run) => onTarget({ kind: "run", data: run })} />}
          <div className="panel-section-head"><div><small>当前实验</small><h3>{experiment.runs.length ? `${experiment.runs.length} 次运行` : "尚未运行"}</h3></div></div>
          {experiment.runs.length ? <div className="panel-run-list">{[...experiment.runs].reverse().map((run) => <button type="button" key={run.id} onClick={() => onTarget({ kind: "run", data: run })}>
            <span className={`panel-run-state ${run.status}`} aria-hidden="true" />
            <span><strong>{run.agentSnapshot ? `${run.agentSnapshot.handle} v${run.agentSnapshot.version}` : "Agent"}</strong><small>{formatTime(run.createdAt, true)} · {run.datasetSnapshot ? `${run.datasetSnapshot.name} v${run.datasetSnapshot.version}` : "测试集"} · {run.caseCount} 个用例</small></span>
            <span className="panel-run-result"><strong>{run.status === "complete" ? `${run.passRate}%` : runStatusLabels[run.status] || run.status}</strong><small>{displayedCost(run)}</small></span>
            <ArrowRight size={15} />
          </button>)}</div> : <div className="panel-empty"><ClockCounterClockwise size={24} /><strong>还没有运行记录</strong><span>准备好测试集和 Agent 后，从对话中的确认卡片开始运行。</span></div>}
        </section>}
      </div>
    </aside>
  </>;
}

function Picker({ type, datasets, agents, selectedId, selectedIds = [], onClose, onSelect, onConfirm, onImport, onCreateAgent, onCreateDatasetVersion, onCreateAgentVersion }) {
  const isDataset = type === "dataset";
	const isMulti = type === "agent-compare";
  const [expandedFamilyId, setExpandedFamilyId] = useState(null);
	const [checkedIds, setCheckedIds] = useState(selectedIds);
	const choose = (id) => isMulti ? setCheckedIds((items) => items.includes(id) ? items.length > 1 ? items.filter((item) => item !== id) : items : items.length < 4 ? [...items, id] : items) : onSelect(id);
	const dismiss = () => isMulti ? onConfirm(checkedIds) : onClose();
  return (
	<div className="modal-backdrop" role="presentation" onMouseDown={dismiss}>
      <section className="picker" role="dialog" aria-modal="true" aria-label={isDataset ? "选择测试集" : "选择 Agent"} onMouseDown={(event) => event.stopPropagation()}>
        <header>
          <div><small>{isDataset ? "DATASET" : "AGENT"}</small><h2>{isDataset ? "选择测试集" : "选择 Agent"}</h2></div>
		  <button type="button" onClick={dismiss} aria-label={isMulti ? "完成选择" : "关闭"}><X size={20} /></button>
        </header>
        {isDataset && (
          <button type="button" className="import-row" onClick={onImport}>
            <FileArrowUp size={20} /><span><strong>导入新用例</strong><small>支持 JSON、JSONL、CSV 和 TXT</small></span><Plus size={18} />
          </button>
        )}
        {!isDataset && (
          <button type="button" className="import-row" onClick={onCreateAgent}>
            <Plus size={20} /><span><strong>新建 Agent 配置</strong><small>选择 Runner、模型和工具</small></span><ArrowRight size={18} />
          </button>
        )}
		{isMulti && <p className="picker-hint">选择 1–4 个 Agent 版本；单选替换当前 Agent，多选时每个 Agent 创建一个独立 Run。</p>}
        <div className="picker-list asset-picker-list">
          {(isDataset ? datasets : agents).map((item) => {
            const connected = isDataset || item.runtime?.available;
            const expanded = expandedFamilyId === item.familyId;
            return (
              <div className={`asset-family ${expanded ? "expanded" : ""}`} key={item.familyId}>
                <div className="asset-family-main">
				  <button type="button" className={`asset-select ${isMulti && checkedIds.includes(item.id) ? "selected" : ""}`} onClick={() => choose(item.id)}>
                    <span className="picker-item-icon">{isDataset ? <Database size={20} /> : <Robot size={20} />}</span>
                    <span className="picker-copy">
                      <strong>{isDataset ? item.name : item.handle}</strong>
                      <small>{item.familyDescription || item.description}</small>
                      <em>{isDataset ? `${item.caseCount} 个用例` : `${item.runnerType} · ${item.model}`}</em>
                    </span>
					<span className={`availability ${connected ? "available" : ""}`}>{isMulti ? checkedIds.includes(item.id) ? <><Check size={13} />已选择</> : connected ? "选择" : "选择（未连接）" : selectedId === item.id ? "已选择" : connected ? "选择最新" : "选择（未连接）"}</span>
                  </button>
                  <button type="button" className={`version-toggle ${expanded ? "open" : ""}`} onClick={() => setExpandedFamilyId(expanded ? null : item.familyId)} aria-expanded={expanded}>
                    <GitBranch size={15} /><span>v{item.version}</span><small>{item.versionCount} 个版本</small><CaretDown size={14} />
                  </button>
                </div>
                {expanded && (
                  <div className="version-list">
                    {item.versions.map((version) => {
                      const versionConnected = isDataset || version.runtime?.available;
                      return (
						<button type="button" className={(isMulti ? checkedIds : [selectedId]).includes(version.id) ? "selected" : ""} key={version.id} onClick={() => choose(version.id)}>
                          <span className="version-number">v{version.version}</span>
                          <span className="version-copy">
                            <strong>{isDataset ? `${version.caseCount} 个用例` : `${version.runnerType} · ${version.model}`}</strong>
                            <small>{version.source || version.description}</small>
                          </span>
                          <span className="version-date">{formatTime(version.createdAt, true)}</span>
						  <span className={`availability ${versionConnected ? "available" : ""}`}>{isMulti ? checkedIds.includes(version.id) ? "已选择" : "选择" : selectedId === version.id ? "已选择" : versionConnected ? "选择" : "选择（未连接）"}</span>
                        </button>
                      );
                    })}
                    <button type="button" className="create-version" onClick={() => isDataset ? onCreateDatasetVersion(item) : onCreateAgentVersion(item)}>
                      <Plus size={15} />{isDataset ? "从文件创建新版本" : "基于当前配置创建新版本"}
                    </button>
                  </div>
                )}
              </div>
            );
          })}
        </div>
		{isMulti && <footer className="picker-confirm"><span>已选 {checkedIds.length}/4 · 关闭后自动保留</span><button type="button" onClick={() => onConfirm(checkedIds)}>完成</button></footer>}
      </section>
    </div>
  );
}

const capabilityDescriptions = {
  browser: "浏览页面与执行浏览器操作",
  files: "读取任务工作区中的文件",
  shell: "运行受控命令行工具",
  web_search: "检索公开网页信息",
  "citation-check": "校验引用与结论是否对应",
  "web-research": "执行带来源的 Web 研究流程",
};

function capabilityOptions(agents, field) {
  const defaults = field === "tools" ? ["web_search", "browser", "files", "shell"] : ["web-research", "citation-check"];
  const ids = new Set([...defaults, ...agents.flatMap((agent) => (agent.versions || [agent]).flatMap((version) => version[field] || []))]);
  return [...ids].sort().map((id) => ({ id, description: capabilityDescriptions[id] || "已在 Agent 配置中使用" }));
}

function CapabilitySelect({ label, value, options, onChange, kind }) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const normalized = query.trim();
  const visible = options.filter((item) => !normalized || `${item.id} ${item.description}`.toLowerCase().includes(normalized.toLowerCase()));
  const toggle = (id) => onChange(value.includes(id) ? value.filter((item) => item !== id) : [...value, id]);
  const addCustom = () => {
    if (normalized && !value.includes(normalized)) onChange([...value, normalized]);
    setQuery("");
  };
  return (
    <div className="capability-field wide">
      <span>{label}</span>
      <div className={`capability-control ${open ? "open" : ""}`}>
        <div className="capability-values">
          {value.map((id) => <button type="button" key={id} onClick={() => toggle(id)}>{id}<X size={11} /></button>)}
          <input value={query} onFocus={() => setOpen(true)} onChange={(event) => { setQuery(event.target.value); setOpen(true); }} onKeyDown={(event) => { if (event.key === "Enter") { event.preventDefault(); addCustom(); } else if (event.key === "Escape") setOpen(false); }} aria-label={`搜索或添加${label}`} placeholder={value.length ? "继续添加…" : `选择或输入 ${kind} ID`} />
        </div>
        <button type="button" className="capability-toggle" onClick={() => setOpen((value) => !value)} aria-expanded={open} aria-label={`展开${label}`}><CaretDown size={15} /></button>
      </div>
      {open && <div className="capability-menu">
        {visible.map((item) => <button type="button" key={item.id} className={value.includes(item.id) ? "selected" : ""} onClick={() => toggle(item.id)}><span>{item.id}<small>{item.description}</small></span>{value.includes(item.id) && <Check size={15} weight="bold" />}</button>)}
        {normalized && !options.some((item) => item.id === normalized) && <button type="button" className="custom-capability" onClick={addCustom}><Plus size={15} /><span>添加“{normalized}”<small>自定义 {kind} ID</small></span></button>}
        {!visible.length && !normalized && <span className="capability-empty">没有可选项</span>}
      </div>}
      <small>{kind === "Tool" ? "Tool 必须由所选 Runner Adapter 提供；可输入自定义 ID 后按回车。" : "Skill 是随 Agent 版本冻结的引用；执行环境必须安装对应 Skill。"}</small>
    </div>
  );
}

function AgentForm({ family, baseVersion, initialForm, toolOptions, skillOptions, modelConnections, runtimes, onManageConnections, onClose, onSubmit, onAskRank, busy }) {
  const isNewVersion = Boolean(family);
  const [form, setForm] = useState(initialForm || {
    name: family?.name || "",
    handle: family?.handle || "",
    runnerType: baseVersion?.runnerType || "dsh",
    model: baseVersion?.model?.startsWith("由") ? "" : baseVersion?.model || "",
    modelConnectionId: baseVersion?.modelConnectionId || "",
    preset: (baseVersion?.runnerType || "dsh") === "dsh" ? "headless" : "",
    systemPrompt: baseVersion?.systemPrompt || "",
    tools: baseVersion?.tools || ["web_search", "browser"],
    skills: baseVersion?.skills || [],
    description: baseVersion ? baseVersion.description || "" : "自定义 Agent 配置",
  });
  function update(field, value) { setForm((current) => ({ ...current, [field]: value })); }
  function selectRunner(runnerType) { setForm((current) => ({ ...current, runnerType, modelConnectionId: "", model: "", preset: runnerType === "dsh" ? "headless" : "" })); }
  const compatibleConnections = modelConnections.filter((item) => form.runnerType === "codex" ? item.protocol === "openai-responses" : form.runnerType === "hermes" ? item.protocol === "openai-chat-completions" : form.runnerType !== "claude-code");
  const selectedConnection = modelConnections.find((item) => item.id === form.modelConnectionId);
  const customConnections = ["dsh", "codex", "hermes"].includes(form.runnerType);
  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={onClose}>
      <form className="agent-form" onSubmit={(event) => { event.preventDefault(); onSubmit(form); }} onMouseDown={(event) => event.stopPropagation()}>
        <header>
          <div><small>AGENT VERSION</small><h2>{isNewVersion ? `${family.handle} 的新版本` : "新建 Agent 配置"}</h2></div>
          <button type="button" onClick={onClose} aria-label="关闭"><X size={20} /></button>
        </header>
        <div className="form-fields">
          <label><span>名称</span><input required disabled={isNewVersion} value={form.name} onChange={(event) => update("name", event.target.value)} placeholder="Research Agent" /></label>
          <label><span>Handle</span><input disabled={isNewVersion} value={form.handle} onChange={(event) => update("handle", event.target.value)} placeholder="@team/research" /></label>
          <label className="wide"><span>运行方式</span><select value={form.runnerType} onChange={(event) => selectRunner(event.target.value)}>{Object.entries(runnerLabels).map(([id,label]) => <option key={id} value={id}>{label}{runtimes?.[id]?.installed === false ? " · 未安装" : runtimes?.[id]?.available === false ? " · 未配置" : ""}</option>)}</select><small>{runtimes?.[form.runnerType]?.reason || "可运行"}</small></label>
          <label className="model-source-field"><span>模型来源{customConnections && <button type="button" onClick={() => onManageConnections(form)}><Plus size={13} />添加连接</button>}</span>{customConnections ? <select value={form.modelConnectionId} onChange={(event) => { const connection = modelConnections.find((item) => item.id === event.target.value); setForm((current) => ({ ...current, modelConnectionId: event.target.value, model: connection?.defaultModel || "" })); }}><option value="">{defaultModelSources[form.runnerType]}</option>{compatibleConnections.map((item) => <option key={item.id} value={item.id} disabled={item.status !== "verified"}>{item.name} · {item.status === "verified" ? "已验证" : "待验证"}</option>)}</select> : <div className="readonly-field">{defaultModelSources[form.runnerType]}</div>}<small>{modelSourceHint(form.runnerType, selectedConnection)}</small></label>
          <label><span>使用模型</span><input list="agent-models" value={form.model} onChange={(event) => update("model", event.target.value)} placeholder={selectedConnection?.defaultModel || "由运行环境决定"} /><datalist id="agent-models">{(selectedConnection?.models || []).map((model) => <option key={model} value={model} />)}</datalist><small>{form.model ? `将使用 ${form.model}` : `未指定时由 ${defaultModelSources[form.runnerType] || "运行环境"} 决定`}</small></label>
          <label className="wide"><span>版本说明</span><input value={form.description} onChange={(event) => update("description", event.target.value)} placeholder="这次修改了什么" /></label>
          <label className="wide"><span>System Prompt</span><textarea rows="3" value={form.systemPrompt} onChange={(event) => update("systemPrompt", event.target.value)} placeholder="Agent 在每个用例中遵循的稳定指令" /></label>
          <CapabilitySelect label="Runner Tools" kind="Tool" value={form.tools} options={toolOptions} onChange={(value) => update("tools", value)} />
          <CapabilitySelect label="Skills" kind="Skill" value={form.skills} options={skillOptions} onChange={(value) => update("skills", value)} />
        </div>
        <footer><button type="button" className="ask-rank" onClick={() => onAskRank(form)}>在对话中继续</button><span /><button type="button" onClick={onClose}>取消</button><button type="submit" className="primary-action" disabled={busy || !form.name.trim()}>{busy ? <CircleNotch size={18} className="spin" /> : <Plus size={18} />}{isNewVersion ? "创建版本并选择" : "创建并选择"}</button></footer>
      </form>
    </div>
  );
}

function CaseEditor({ dataset, item, onClose, onSave, busy, saveError }) {
  const editing = Boolean(item);
  const [error, setError] = useState("");
  const [form, setForm] = useState({
    id: item?.id || `case-${globalThis.crypto?.randomUUID?.().slice(0, 8) || Date.now()}`,
    title: item?.title || "",
    input: item?.input || "",
    expected: JSON.stringify(item?.expected || {}, null, 2),
  });
  function update(field, value) { setForm((current) => ({ ...current, [field]: value })); }
  function submit(event) {
    event.preventDefault();
    try {
      const expected = JSON.parse(form.expected || "{}");
      if (!expected || Array.isArray(expected) || typeof expected !== "object") throw new Error("期望结果必须是 JSON 对象");
      setError("");
      onSave({ id: form.id.trim(), title: form.title.trim(), input: form.input.trim(), expected });
    } catch (parseError) { setError(parseError.message); }
  }
  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={onClose}>
      <form className="agent-form case-form" onSubmit={submit} onMouseDown={(event) => event.stopPropagation()}>
        <header><div><small>DATASET VERSION</small><h2>{editing ? "编辑用例" : "新增用例"}</h2></div><button type="button" onClick={onClose} aria-label="关闭"><X size={20} /></button></header>
        <div className="form-fields">
          <label><span>用例 ID</span><input required disabled={editing} value={form.id} onChange={(event) => update("id", event.target.value)} /></label>
          <label><span>标题</span><input required value={form.title} onChange={(event) => update("title", event.target.value)} placeholder="用例名称" /></label>
          <label className="wide"><span>输入</span><textarea rows="5" required value={form.input} onChange={(event) => update("input", event.target.value)} placeholder="发送给 Agent 的完整任务" /></label>
          <label className="wide"><span>期望结果（JSON）</span><textarea className="expected-json" rows="7" value={form.expected} onChange={(event) => update("expected", event.target.value)} spellCheck="false" /><small>用于确定性校验、Rubric 或 Judge 的期望证据。</small></label>
          {(error || saveError) && <div className="form-error wide"><WarningCircle size={15} />{error || saveError}</div>}
        </div>
        <footer><p><GitBranch size={14} />不会修改 v{dataset.version}，将保存为 v{dataset.version + 1}</p><span /><button type="button" onClick={onClose}>取消</button><button type="submit" className="primary-action" disabled={busy || !form.id.trim() || !form.title.trim() || !form.input.trim()}>{busy ? <CircleNotch size={18} className="spin" /> : editing ? <PencilSimple size={17} /> : <Plus size={17} />}{editing ? "保存新版本" : "新增并保存"}</button></footer>
      </form>
    </div>
  );
}

function HistoryPanel({ experiments, currentId, onClose, onOpen, onNew }) {
  return (
    <div className="history-backdrop" role="presentation" onMouseDown={onClose}>
      <aside className="history-panel" role="dialog" aria-modal="true" aria-label="实验记录" onMouseDown={(event) => event.stopPropagation()}>
        <div className="history-head">
          <div><small>EXPERIMENTS</small><h2>实验记录</h2></div>
          <button type="button" onClick={onClose} aria-label="关闭"><X size={20} /></button>
        </div>
        <button type="button" className="new-experiment" onClick={onNew}><Plus size={18} />新建实验</button>
        <div className="history-list">
          {experiments.map((item) => (
            <button type="button" className={item.id === currentId ? "current" : ""} key={item.id} onClick={() => onOpen(item.id)}>
              <span><strong>{item.title}</strong><small>{item.runSummary ? `${item.runSummary.completed} 次运行 · ${item.runSummary.passed}/${item.runSummary.total} 通过 · ${displayedCost(item.runSummary)}` : item.datasetVersionId || item.agentVersionId ? "配置中" : "尚未配置"}</small></span>
              <time>{formatTime(item.updatedAt, true)}</time>
            </button>
          ))}
        </div>
      </aside>
    </div>
  );
}

export function App() {
  const [bootstrap, setBootstrap] = useState(null);
  const [experiment, setExperiment] = useState(null);
  const [composer, setComposer] = useState("");
  const [mentions, setMentions] = useState([]);
  const [mentionPicker, setMentionPicker] = useState(null);
  const [mentionIndex, setMentionIndex] = useState(0);
  const [pendingMessage, setPendingMessage] = useState(null);
  const [controlTurn, setControlTurn] = useState(null);
  const [inspector, setInspector] = useState(null);
  const [panelOpen, setPanelOpen] = useState(false);
  const [panelTab, setPanelTab] = useState("config");
  const [panelExpanded, setPanelExpanded] = useState(false);
  const [picker, setPicker] = useState(null);
  const [agentFormOpen, setAgentFormOpen] = useState(false);
  const [agentVersionTarget, setAgentVersionTarget] = useState(null);
  const [agentDraft, setAgentDraft] = useState(null);
  const [caseEditor, setCaseEditor] = useState(null);
  const [datasetVersionTarget, setDatasetVersionTarget] = useState(null);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [trialCount, setTrialCount] = useState(5);
	const [comparisonAgentIds, setComparisonAgentIds] = useState([]);
  const fileInputRef = useRef(null);
  const versionFileInputRef = useRef(null);
  const conversationEndRef = useRef(null);

  const agentRuntime = useMemo(() => bootstrap?.agents
    .flatMap((item) => item.versions || [item])
    .find((item) => item.id === experiment?.agentVersionId)?.runtime, [bootstrap, experiment]);
  const toolOptions = useMemo(() => capabilityOptions(bootstrap?.agents || [], "tools"), [bootstrap]);
  const skillOptions = useMemo(() => capabilityOptions(bootstrap?.agents || [], "skills"), [bootstrap]);
  const timeline = useMemo(() => {
    if (!experiment) return [];
    return [
      ...experiment.messages.map((message) => ({ type: "message", at: message.createdAt, data: message })),
      ...(pendingMessage ? [{ type: "message", at: pendingMessage.createdAt, data: pendingMessage }] : []),
      ...experiment.runs.map((run) => ({ type: "run", at: run.createdAt, data: run })),
      ...experiment.controlEvents.filter((event) => ["a2ui/prepare_run", "a2ui/show_experiment_results"].includes(event.type)).map((event) => ({ type: "a2ui", at: event.createdAt, data: event })),
    ].sort((a, b) => a.at.localeCompare(b.at));
  }, [experiment, pendingMessage]);
	const activeRuns = experiment?.runs.filter((run) => ACTIVE_STATES.has(run.status)) || [];
	const activeRunKey = activeRuns.map((run) => run.id).join(",");
	const runProgressKey = experiment?.runs.map((run) => `${run.id}:${run.status}:${run.results.length}`).join(",") || "";
	const allAgentVersions = useMemo(() => bootstrap?.agents.flatMap((item) => item.versions || [item]) || [], [bootstrap]);
  const mentionOptions = useMemo(() => {
    const query = mentionSearchQuery(composer);
    const datasets = mentionPicker === "agent" ? [] : filterMentionItems(bootstrap?.datasets || [], query, (item) => [item.name, item.description, item.source]);
    const agents = mentionPicker === "dataset" ? [] : filterMentionItems(bootstrap?.agents || [], query, (item) => [item.handle, item.name, item.runnerType, item.model]);
    return [...datasets.map((item) => ({ kind: "dataset", item })), ...agents.map((item) => ({ kind: "agent", item }))];
  }, [bootstrap, composer, mentionPicker]);

  useEffect(() => setMentionIndex(0), [composer, mentionPicker]);

  async function refreshBootstrap() {
    const next = await rankApi.bootstrap();
    setBootstrap(next);
    return next;
  }

  async function openExperiment(id) {
    const next = await rankApi.getExperiment(id);
    setExperiment(next);
    setInspector(null);
    localStorage.setItem("rank.activeExperiment", id);
  }

  function inspect(target) {
    setInspector(target);
    setPanelTab(target?.kind === "run" || target?.kind === "case" ? "runs" : "config");
    setPanelOpen(true);
  }

  async function createExperiment() {
    setBusy(true);
    try {
      const next = await rankApi.createExperiment();
      setExperiment(next);
      localStorage.setItem("rank.activeExperiment", next.id);
      setHistoryOpen(false);
      await refreshBootstrap();
    } catch (createError) {
      setError(createError.message);
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const nextBootstrap = await rankApi.bootstrap();
        if (cancelled) return;
        setBootstrap(nextBootstrap);
        const remembered = localStorage.getItem("rank.activeExperiment");
        const existing = nextBootstrap.experiments.find((item) => item.id === remembered) || nextBootstrap.experiments[0];
        const nextExperiment = existing ? await rankApi.getExperiment(existing.id) : await rankApi.createExperiment();
        if (!cancelled) {
          setExperiment(nextExperiment);
          localStorage.setItem("rank.activeExperiment", nextExperiment.id);
          if (!existing) setBootstrap(await rankApi.bootstrap());
        }
      } catch (loadError) {
        if (!cancelled) setError(`无法连接 Rank 本地 API：${loadError.message}`);
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => { cancelled = true; };
  }, []);

  useEffect(() => {
	if (!activeRuns.length) return undefined;
    const experimentId = experiment.id;
    let closed = false;
	const streams = activeRuns.map((activeRun) => {
	  let refreshing = false;
	  const stream = new EventSource(rankApi.runEventsURL(activeRun.id, activeRun.events.at(-1)?.sequence || 0));
	  const refreshRun = async () => {
		if (closed || refreshing) return;
		refreshing = true;
		try {
		  const nextRun = await rankApi.getRun(activeRun.id);
		  setExperiment((current) => current ? { ...current, runs: current.runs.map((run) => run.id === nextRun.id ? nextRun : run) } : current);
		  if (!ACTIVE_STATES.has(nextRun.status)) {
			stream.close();
			await Promise.all([openExperiment(experimentId), refreshBootstrap()]);
		  }
		} catch (streamError) { setError(streamError.message); }
		finally { refreshing = false; }
	  };
	  stream.onmessage = () => void refreshRun();
    const eventNames = ["run.created", "run.status", "run.recovered", "trial.started", "trial.retry", "trial.completed", "trial.invalid", "case.started", "candidate.queued", "candidate.running", "candidate.harness.started", "candidate.harness.output", "candidate.harness.stdout", "candidate.harness.stderr", "candidate.completed", "candidate.failed", "judge.queued", "judge.running", "judge.harness.started", "judge.harness.output", "judge.harness.stdout", "judge.harness.stderr", "judge.completed", "judge.failed", "judge.verdict", "agent.message", "artifact.available", "case.completed", "run.completed"];
    for (const name of eventNames) stream.addEventListener(name, () => void refreshRun());
	  stream.onerror = () => { if (!closed && stream.readyState === EventSource.CLOSED) void refreshRun(); };
	  return stream;
	});
	return () => { closed = true; streams.forEach((stream) => stream.close()); };
  }, [activeRunKey]);

	const latestPreparedRun = useMemo(() => {
	  const event = [...(experiment?.controlEvents || [])].reverse().find((item) => item.type === "a2ui/prepare_run");
	  const payload = event ? a2uiPayload(event) : {};
	  return payload.agentVersionId === experiment?.agentVersionId && payload.datasetVersionId === experiment?.datasetVersionId ? payload : null;
	}, [experiment?.id, experiment?.agentVersionId, experiment?.datasetVersionId, experiment?.controlEvents]);

	useEffect(() => {
	  if (!experiment?.agentVersionId) return;
	  setComparisonAgentIds(latestPreparedRun?.agentVersionIds?.length ? latestPreparedRun.agentVersionIds : [experiment.agentVersionId]);
	  setTrialCount([1, 5].includes(Number(latestPreparedRun?.trialCount)) ? Number(latestPreparedRun.trialCount) : 5);
	}, [experiment?.id, experiment?.agentVersionId, latestPreparedRun]);

  useEffect(() => {
    if (!controlTurn?.id) return undefined;
    let closed = false;
    const stream = new EventSource(rankApi.controlTurnEventsURL(experiment.id, controlTurn.id));
    const finish = async (event) => {
      const value = JSON.parse(event.data);
      setControlTurn((current) => current ? { ...current, events: [...current.events, value] } : current);
      if (value.type === "turn.completed") {
        closed = true;
        stream.close();
        await Promise.all([openExperiment(experiment.id), refreshBootstrap()]);
        setPendingMessage(null);
        setControlTurn(null);
      } else if (value.type === "turn.failed") {
        closed = true;
        stream.close();
        await Promise.all([openExperiment(experiment.id), refreshBootstrap()]);
        setPendingMessage(null);
        setControlTurn(null);
        setError("");
      }
    };
    for (const name of ["turn.started", "assistant.status", "assistant.delta", "tool.started", "tool.completed", "turn.completed", "turn.failed"]) stream.addEventListener(name, finish);
    stream.onerror = () => { if (!closed && stream.readyState === EventSource.CLOSED) setError("对话事件流已断开，请重试"); };
    return () => { closed = true; stream.close(); };
  }, [controlTurn?.id]);

  useEffect(() => {
    conversationEndRef.current?.scrollIntoView({ behavior: "smooth", block: "nearest" });
  }, [timeline.length, runProgressKey, controlTurn?.events.length]);

  async function updateSelection(patch) {
    setBusy(true);
    setError("");
    try {
      const command = patch.datasetVersionId ? "select_dataset" : "select_agent";
      const action = experiment.a2ui?.actions?.[command];
      if (!action) throw new Error("实验操作已过期，请刷新后重试");
      const result = await rankApi.executeCommand(experiment.id, action, patch);
      setExperiment(result.experiment);
      setPicker(null);
      await refreshBootstrap();
      return true;
    } catch (selectionError) {
      setError(selectionError.message);
      return false;
    } finally {
      setBusy(false);
    }
  }

  async function selectRunAgents(ids) {
    if (ids.length === 1 && ids[0] !== experiment.agentVersionId) {
      if (!await updateSelection({ agentVersionId: ids[0] })) return;
    } else setPicker(null);
    setComparisonAgentIds(ids);
  }

  async function sendMessage(quickText) {
    const quick = typeof quickText === "string";
    const text = (quick ? quickText : composer).trim();
    if (!text || busy || controlTurn) return;
    const message = { id: `pending-${crypto.randomUUID()}`, role: "user", content: text, createdAt: new Date().toISOString(), pending: true };
    setPendingMessage(message);
    if (!quick) setComposer("");
    setMentionPicker(null);
    setError("");
    if (!quick && experiment.title === "未命名实验") {
      const title = experimentTitleFromMessage(text);
      if (title !== "未命名实验") void rankApi.updateExperiment(experiment.id, { title }).then((next) => {
        setExperiment((current) => current?.id === next.id ? { ...current, title: next.title } : current);
        return refreshBootstrap();
      }).catch(() => {});
    }
    try {
      const turn = await rankApi.sendMessage(experiment.id, text, mentions);
      setMentions([]);
      setControlTurn({ id: turn.turnId, events: [] });
    } catch (messageError) {
      setPendingMessage({ ...message, pending: false, failed: true });
      setError(messageError.message);
    }
  }

  function updateComposer(value) {
    setComposer(value);
    setMentionPicker(/(?:^|\s)@[^\s@]*$/.test(value) ? "all" : null);
  }

  function addMention(kind, item) {
    const label = kind === "dataset" ? `${item.name} v${item.version}` : `${item.handle} v${item.version}`;
    const token = kind === "dataset" ? `@${item.name}` : item.handle;
    setComposer((value) => mentionPicker === "all" ? value.replace(/@[^\s@]*$/, `${token} `) : `${value}${value && !value.endsWith(" ") ? " " : ""}${token} `);
    setMentions((items) => [...items.filter((entry) => entry.kind !== kind), { kind, id: item.id, label }]);
    setMentionPicker(null);
  }

  async function startRun() {
    setBusy(true);
    setError("");
    try {
      const action = experiment.a2ui?.actions?.start_run;
      if (!action) throw new Error("运行确认已过期，请刷新后重试");
	  const result = await rankApi.executeCommand(experiment.id, action, { trialCount, agentVersionIds: comparisonAgentIds });
      setExperiment(result.experiment);
      await refreshBootstrap();
    } catch (runError) {
      setError(runError.message);
    } finally {
      setBusy(false);
    }
  }

  async function cancelRun(runId) {
    await rankApi.cancelRun(runId);
  }

  async function handleFile(event, versionFamily = null) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    setBusy(true);
    setError("");
    try {
      const cases = parseImportedCases(file.name, await file.text());
      const source = `文件 ${file.name}`;
      const dataset = versionFamily
        ? await rankApi.createDatasetVersion(versionFamily.familyId, { source, description: `${source} 导入的新版本`, cases })
        : await rankApi.createDataset({ name: file.name.replace(/\.[^.]+$/, ""), source, cases });
      await refreshBootstrap();
      const action = experiment.a2ui?.actions?.select_dataset;
      if (!action) throw new Error("实验操作已过期，请刷新后重试");
      const selected = await rankApi.executeCommand(experiment.id, action, { datasetVersionId: dataset.id });
      setExperiment(selected.experiment);
      setPicker(null);
    } catch (fileError) {
      setError(`导入失败：${fileError.message}`);
    } finally {
      setBusy(false);
      setDatasetVersionTarget(null);
    }
  }

  async function createAgent(form) {
    setBusy(true);
    setError("");
    try {
      const agent = agentVersionTarget
        ? await rankApi.createAgentVersion(agentVersionTarget.familyId, form)
        : await rankApi.createAgent(form);
      await refreshBootstrap();
      const action = experiment.a2ui?.actions?.select_agent;
      if (!action) throw new Error("实验操作已过期，请刷新后重试");
      const selected = await rankApi.executeCommand(experiment.id, action, { agentVersionId: agent.id });
      setExperiment(selected.experiment);
      setInspector({ kind: "agent", data: { ...selected.experiment.agent, runtime: agent.runtime } });
      setAgentFormOpen(false);
      setAgentVersionTarget(null);
      setAgentDraft(null);
    } catch (agentError) {
      setError(agentError.message);
    } finally {
      setBusy(false);
    }
  }

  async function saveModelConnection(id, input) {
    const saved = id ? await rankApi.updateModelConnection(id, input) : await rankApi.createModelConnection(input);
    const verified = input.apiKey || saved.hasCredential ? await rankApi.verifyModelConnection(saved.id) : saved;
    await refreshBootstrap();
    return verified;
  }

  async function deleteModelConnection(id) {
    await rankApi.deleteModelConnection(id);
    await refreshBootstrap();
  }

  async function syncModelConnection(id) {
    try { return await rankApi.verifyModelConnection(id); }
    finally { await refreshBootstrap(); }
  }

  async function bindSystemModel(role, connectionId) {
    const connection = (bootstrap.modelConnections || []).find((item) => item.id === connectionId);
    await rankApi.setSystemModel(role, { connectionId, model: connection?.defaultModel });
    await refreshBootstrap();
  }

  function manageModelConnections(form) {
    setAgentDraft(form);
    setAgentFormOpen(false);
    setInspector(null);
    setPanelTab("connections");
    setPanelOpen(true);
  }

  function useModelConnection(connection) {
    if (!agentDraft) return;
    setAgentDraft((current) => ({ ...current, modelConnectionId: connection.id, model: connection.defaultModel || current.model }));
    setPanelOpen(false);
    setAgentFormOpen(true);
  }

  async function saveDatasetCase(nextCase) {
    const { dataset, item } = caseEditor;
    setBusy(true);
    setError("");
    try {
      if (!item && dataset.cases.some((entry) => entry.id === nextCase.id)) throw new Error("用例 ID 已存在");
      const cases = item ? dataset.cases.map((entry) => entry.id === item.id ? nextCase : entry) : [...dataset.cases, nextCase];
      const version = await rankApi.createDatasetVersion(dataset.familyId, { source: dataset.source || "侧栏编辑", description: dataset.description, schema: dataset.schema || {}, rubric: dataset.rubric || {}, cases });
      await refreshBootstrap();
      const action = experiment.a2ui?.actions?.select_dataset;
      if (!action) throw new Error("实验操作已过期，请刷新后重试");
      const selected = await rankApi.executeCommand(experiment.id, action, { datasetVersionId: version.id });
      setExperiment(selected.experiment);
      setInspector({ kind: "dataset", data: selected.experiment.dataset });
      setCaseEditor(null);
    } catch (caseError) {
      setError(caseError.message);
    } finally {
      setBusy(false);
    }
  }

  function askRankToConfigureAgent(form) {
    setComposer(agentConversationPrompt(form, agentVersionTarget, bootstrap.modelConnections || []));
    setAgentFormOpen(false);
    setAgentVersionTarget(null);
    setAgentDraft(null);
  }

  function handleComposerKeyDown(event) {
    if (mentionPicker && !event.nativeEvent.isComposing) {
      if (event.key === "Escape") {
        event.preventDefault();
        setMentionPicker(null);
        return;
      }
      if (["ArrowDown", "ArrowUp"].includes(event.key) && mentionOptions.length) {
        event.preventDefault();
        setMentionIndex((index) => (index + (event.key === "ArrowDown" ? 1 : -1) + mentionOptions.length) % mentionOptions.length);
        return;
      }
      if (event.key === "Enter" && mentionOptions.length) {
        event.preventDefault();
        const option = mentionOptions[mentionIndex] || mentionOptions[0];
        addMention(option.kind, option.item);
        return;
      }
    }
    if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      void sendMessage();
    }
  }

  function renderTimelineItem(item, index) {
    if (item.type === "message") {
      const retrySource = item.data.role === "assistant" && parseMessageContent(item.data.content).blocks.some((block) => block.type === "error")
        ? timeline.slice(0, index).findLast((entry) => entry.type === "message" && entry.data.role === "user")?.data.content
        : "";
      return <Message message={item.data} onRetry={retrySource && !controlTurn ? () => sendMessage(retrySource) : undefined} key={item.data.id} />;
    }
    if (item.type === "run") return <RunCard run={item.data} onCancel={cancelRun} onInspect={inspect} key={item.data.id} />;
    const payload = a2uiPayload(item.data);
    if (item.data.type === "a2ui/show_experiment_results") {
      const ids = new Set((payload.runs || []).map((run) => run.id));
      const runs = ids.size ? experiment.runs.filter((run) => ids.has(run.id)) : experiment.runs;
      return <A2UIRow key={`a2ui-${item.data.id}`}><ExperimentChart runs={runs} onInspect={(run) => inspect({ kind: "run", data: run })} /></A2UIRow>;
    }
    const dataset = bootstrap.datasets.flatMap((entry) => entry.versions || [entry]).find((entry) => entry.id === payload.datasetVersionId) || experiment.dataset;
    const agent = bootstrap.agents.flatMap((entry) => entry.versions || [entry]).find((entry) => entry.id === payload.agentVersionId) || experiment.agent;
	const cardAgentIds = comparisonAgentIds.length ? comparisonAgentIds : payload.agentVersionIds || [];
	const selectedAgents = cardAgentIds.map((id) => allAgentVersions.find((entry) => entry.id === id)).filter(Boolean);
	const preparedAgent = allAgentVersions.find((entry) => entry.id === agent?.id) || agent;
	const cardAgents = dataset?.id === experiment.datasetVersionId
	  ? selectedAgents.length ? selectedAgents : [preparedAgent]
	  : [{ ...preparedAgent, runtime: { available: false, reason: "实验配置已变化，请重新点击“运行配置”生成确认卡片" } }];
	return dataset && agent ? <ReadyCard key={`a2ui-${item.data.id}`} experiment={{ ...experiment, dataset, agent }} agents={cardAgents} trialCount={trialCount} onTrialCount={setTrialCount} onPickDataset={() => inspect({ kind: "dataset", data: dataset })} onPickAgent={(target) => inspect({ kind: "agent", data: target })} onSwitchDataset={() => setPicker("dataset")} onSelectAgents={() => setPicker("agent-compare")} onStart={startRun} busy={busy} /> : null;
  }

  if (loading) return <div className="app-state"><CircleNotch size={24} className="spin" /><span>正在打开实验…</span></div>;
  if (!experiment || !bootstrap) return <div className="app-state error"><WarningCircle size={24} /><strong>Rank 没有启动</strong><span>{error}</span><button type="button" onClick={() => window.location.reload()}>重试</button></div>;
  const configuredRoles = new Set((bootstrap.systemModels || []).filter((item) => item.connection?.status === "verified").map((item) => item.role));
  if (!configuredRoles.has("control") || !configuredRoles.has("judge")) return <SystemModelSetup catalog={bootstrap.modelCatalog || []} connections={bootstrap.modelConnections || []} onComplete={async () => { await refreshBootstrap(); await openExperiment(experiment.id); }} />;

  return (
    <div className={`app-shell ${panelOpen ? "panel-open" : ""} ${panelOpen && panelExpanded ? "panel-expanded" : ""}`}>
      <header className="topbar">
        <div className="top-nav">
          <a className="brand" href="#top" aria-label="Rank 首页">Rank</a>
          <div className="top-actions">
            <button type="button" onClick={createExperiment}><Plus size={18} />新实验</button>
            <button type="button" onClick={() => setHistoryOpen(true)}><ClockCounterClockwise size={19} />实验记录</button>
          </div>
        </div>
        <button type="button" className={`panel-toggle ${panelOpen ? "active" : ""}`} onClick={() => setPanelOpen((open) => !open)} aria-label={panelOpen ? "关闭实验工作区" : "展开实验工作区"} aria-expanded={panelOpen} aria-controls="experiment-panel"><SidebarSimple size={21} weight={panelOpen ? "fill" : "regular"} /></button>
      </header>

      <main className="workspace" id="top">
        <section className="experiment-heading">
          <span className="eyebrow">EXPERIMENT</span>
          <h1>{experiment.title}</h1>
          <small>{experiment.runs.length ? `${experiment.runs.length} 次运行` : "尚未运行"}</small>
        </section>

        <section className="conversation" aria-label="实验对话">
          {timeline.map(renderTimelineItem)}

          {controlTurn && <TurnActivity turn={controlTurn} />}
          <div ref={conversationEndRef} />
        </section>

        <section className="composer" aria-label="实验输入">
          {mentionPicker && <div className="mention-menu" role="listbox" aria-label="选择测试集或 Agent">
            {mentionOptions.map(({ kind, item }, index) => <button type="button" role="option" aria-selected={index === mentionIndex} key={`${kind}-${item.id}`} onMouseEnter={() => setMentionIndex(index)} onClick={() => addMention(kind, item)}>{kind === "dataset" ? <Database size={17} /> : <Robot size={17} />}<span><strong>{kind === "dataset" ? item.name : item.handle}</strong><small>{kind === "dataset" ? `v${item.version} · ${item.caseCount} 个用例` : `v${item.version} · ${item.runnerType}`}</small></span></button>)}
            {!mentionOptions.length && <div className="mention-empty">没有匹配的测试集或 Agent</div>}
          </div>}
          {error && <div className="composer-error"><WarningCircle size={16} />{error}<button type="button" onClick={() => setError("")} aria-label="关闭错误"><X size={14} /></button></div>}
          {mentions.length > 0 && <div className="mention-chips">{mentions.map((item) => <span key={item.kind}><i>{item.kind === "dataset" ? "测试集" : "Agent"}</i>{item.label}<button type="button" onClick={() => setMentions((items) => items.filter((entry) => entry.kind !== item.kind))} aria-label={`移除 ${item.label}`}><X size={11} /></button></span>)}</div>}
          <div className="composer-main">
            <label className="sr-only" htmlFor="task-input">输入消息</label>
            <textarea id="task-input" rows="2" value={composer} onChange={(event) => updateComposer(event.target.value)} onKeyDown={handleComposerKeyDown} placeholder="描述目标、输入 @ 选择测试集或 Agent…" />
            <button className="send-button" type="button" onClick={sendMessage} disabled={!composer.trim() || busy || Boolean(controlTurn)} aria-label="发送消息">
              {controlTurn ? <CircleNotch size={19} className="spin" /> : <PaperPlaneRight size={19} weight="fill" />}
            </button>
          </div>
          <div className="quick-actions">
            <button type="button" onClick={() => sendMessage("准备运行")} disabled={busy || Boolean(controlTurn)}><Play size={18} />运行配置</button>
            <button type="button" onClick={() => sendMessage("查看当前实验的整体实验表现")} disabled={busy || Boolean(controlTurn)}><ChartLineUp size={18} />实验表现</button>
            <button type="button" onClick={() => fileInputRef.current?.click()}><FileArrowUp size={18} />导入用例</button>
          </div>
          <input ref={fileInputRef} className="sr-only" type="file" accept=".json,.jsonl,.csv,.txt" onChange={(event) => handleFile(event)} />
          <input ref={versionFileInputRef} className="sr-only" type="file" accept=".json,.jsonl,.csv,.txt" onChange={(event) => handleFile(event, datasetVersionTarget)} />
        </section>
      </main>

      {picker && <Picker
		type={picker}
        datasets={bootstrap.datasets}
        agents={bootstrap.agents}
		selectedId={picker === "dataset" ? experiment.datasetVersionId : experiment.agentVersionId}
		selectedIds={comparisonAgentIds}
        onClose={() => setPicker(null)}
		onSelect={(id) => updateSelection(picker === "dataset" ? { datasetVersionId: id } : { agentVersionId: id })}
		onConfirm={selectRunAgents}
        onImport={() => { setDatasetVersionTarget(null); fileInputRef.current?.click(); }}
        onCreateDatasetVersion={(family) => { setDatasetVersionTarget(family); versionFileInputRef.current?.click(); }}
        onCreateAgent={() => { setPicker(null); setAgentVersionTarget(null); setAgentDraft(null); setAgentFormOpen(true); }}
        onCreateAgentVersion={(family) => { setPicker(null); setAgentVersionTarget(family); setAgentDraft(null); setAgentFormOpen(true); }}
      />}
      {agentFormOpen && <AgentForm
        family={agentVersionTarget}
        baseVersion={agentVersionTarget}
        initialForm={agentDraft}
        toolOptions={toolOptions}
        skillOptions={skillOptions}
        modelConnections={bootstrap.modelConnections || []}
        runtimes={bootstrap.runtimes || {}}
        onManageConnections={manageModelConnections}
        onClose={() => { setAgentFormOpen(false); setAgentVersionTarget(null); setAgentDraft(null); }}
        onSubmit={createAgent}
        onAskRank={askRankToConfigureAgent}
        busy={busy}
      />}
      {caseEditor && <CaseEditor dataset={caseEditor.dataset} item={caseEditor.item} onClose={() => setCaseEditor(null)} onSave={saveDatasetCase} busy={busy} saveError={error} />}
      {historyOpen && <HistoryPanel experiments={bootstrap.experiments} currentId={experiment.id} onClose={() => setHistoryOpen(false)} onOpen={async (id) => { await openExperiment(id); setHistoryOpen(false); }} onNew={createExperiment} />}
      {panelOpen && <ExperimentPanel
        experiment={experiment}
        agentRuntime={agentRuntime}
        modelConnections={bootstrap.modelConnections || []}
        modelCatalog={bootstrap.modelCatalog || []}
        systemModels={bootstrap.systemModels || []}
        target={inspector}
        tab={panelTab}
        expanded={panelExpanded}
        onTab={setPanelTab}
        onTarget={setInspector}
        onClose={() => { setPanelOpen(false); setPanelExpanded(false); }}
        onExpand={() => setPanelExpanded((value) => !value)}
        onPickDataset={() => setPicker("dataset")}
        onPickAgent={() => setPicker("agent")}
        onAddCase={(dataset) => { setError(""); setCaseEditor({ dataset, item: null }); }}
        onEditCase={(dataset, item) => { setError(""); setCaseEditor({ dataset, item }); }}
        onEditAgent={(agent) => { setError(""); setAgentDraft(null); setAgentVersionTarget(agent); setAgentFormOpen(true); }}
        onBindSystemModel={bindSystemModel}
        onSaveConnection={saveModelConnection}
        onSyncConnection={syncModelConnection}
        onDeleteConnection={deleteModelConnection}
        onUseConnection={useModelConnection}
      />}
    </div>
  );
}
