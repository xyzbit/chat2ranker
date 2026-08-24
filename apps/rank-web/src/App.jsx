import { useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowRight,
  CaretDown,
  Check,
  CheckCircle,
  CircleNotch,
  ClockCounterClockwise,
  Coins,
  Database,
  FileArrowUp,
  GitBranch,
  PaperPlaneRight,
  Play,
  Plus,
  Robot,
  Smiley,
  Stop,
  User,
  WarningCircle,
  X,
} from "@phosphor-icons/react";
import { rankApi } from "./api.js";
import { parseImportedCases } from "./import-cases.js";

const ACTIVE_STATES = new Set(["queued", "preparing", "running", "scoring"]);

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

function Avatar({ role = "assistant" }) {
  return (
    <span className={`avatar avatar-${role}`} aria-hidden="true">
      {role === "assistant" ? <Smiley size={21} /> : <User size={20} />}
    </span>
  );
}

function Message({ message }) {
  return (
    <article className={`message-row ${message.role}`}>
      {message.role === "assistant" && <Avatar />}
      <div className="message-stack">
        <div className="message-bubble">{message.content}</div>
        <time className="message-time">{formatTime(message.createdAt)}</time>
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

function SetupCard({ experiment, onPickDataset, onPickAgent }) {
  const items = [
    {
      id: "dataset",
      icon: Database,
      label: "测试集",
      value: experiment.dataset ? `${experiment.dataset.name} · ${experiment.dataset.caseCount} 个用例` : "选择已有，或导入新用例",
      ready: Boolean(experiment.dataset),
      action: onPickDataset,
    },
    {
      id: "agent",
      icon: Robot,
      label: "Agent",
      value: experiment.agent ? `${experiment.agent.handle} · v${experiment.agent.version}` : "选择预设或已配置 Agent",
      ready: Boolean(experiment.agent),
      action: onPickAgent,
    },
  ];
  return (
    <A2UIRow>
      <section className="setup-card" aria-label="准备实验">
        <header>
          <span>准备实验</span>
          <small>{items.filter((item) => item.ready).length}/2</small>
        </header>
        <div className="setup-items">
          {items.map((item) => {
            const Icon = item.icon;
            return (
              <button type="button" className="setup-item" onClick={item.action} key={item.id}>
                <span className={`setup-icon ${item.ready ? "ready" : ""}`}>{item.ready ? <Check size={17} weight="bold" /> : <Icon size={19} />}</span>
                <span><strong>{item.label}</strong><small>{item.value}</small></span>
                <ArrowRight size={17} />
              </button>
            );
          })}
        </div>
      </section>
    </A2UIRow>
  );
}

function ReadyCard({ experiment, agentRuntime, trialCount, onTrialCount, onPickDataset, onPickAgent, onStart, busy }) {
  const unavailable = agentRuntime && !agentRuntime.available;
  const estimate = experiment.agent.runnerType === "mock" ? `预计 $${(experiment.dataset.caseCount * trialCount * 0.034).toFixed(2)}` : "按运行时实际计费";
  return (
    <A2UIRow>
      <section className="ready-card" aria-label="确认运行">
        <header>
          <div><small>运行快照</small><h2>可以开始了</h2></div>
          <span className="ready-mark"><CheckCircle size={18} weight="fill" /> 已就绪</span>
        </header>
        <div className="snapshot-list">
          <button type="button" onClick={onPickDataset}>
            <Database size={18} />
            <span><small>测试集</small><strong>{experiment.dataset.name} v{experiment.dataset.version}</strong></span>
            <em>{experiment.dataset.caseCount} 条</em>
          </button>
          <button type="button" onClick={onPickAgent}>
            <Robot size={18} />
            <span><small>Agent</small><strong>{experiment.agent.handle} v{experiment.agent.version}</strong></span>
            <em>{experiment.agent.runnerType}</em>
          </button>
        </div>
        <div className="trial-choice" aria-label="每个用例运行次数">
          <span><strong>重复运行</strong><small>独立上下文，避免偶然结果</small></span>
          <div>
            <button type="button" className={trialCount === 1 ? "selected" : ""} onClick={() => onTrialCount(1)}>快速 · 1 次</button>
            <button type="button" className={trialCount === 5 ? "selected" : ""} onClick={() => onTrialCount(5)}>可靠 · 5 次</button>
          </div>
        </div>
        {unavailable && <div className="runtime-warning"><WarningCircle size={17} />{agentRuntime.reason}</div>}
        <footer>
          <span><Coins size={16} /> {estimate} · {experiment.dataset.caseCount * trialCount} 个 Trial · 并发 5</span>
          <button type="button" className="primary-action" onClick={onStart} disabled={busy || unavailable}>
            {busy ? <CircleNotch size={18} className="spin" /> : <Play size={18} weight="fill" />}
            {busy ? "正在创建" : "开始运行"}
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

function RunCard({ run, onCancel }) {
  const [showFailures, setShowFailures] = useState(false);
  const [showLog, setShowLog] = useState(false);
  const [artifactView, setArtifactView] = useState(null);
  const multiTrial = Number(run.scheduledTrials) > 0;
  const failures = run.results.filter((item) => multiTrial ? !item.reliable : !item.passed);
  const complete = run.status === "complete";
  const active = ACTIVE_STATES.has(run.status);
  const scheduled = multiTrial ? (run.scheduledTrials || run.total) : run.total;
  const completedTrials = multiTrial ? (run.completedTrials || 0) : run.results.length;
  const progress = scheduled ? Math.round((completedTrials / scheduled) * 100) : 0;
  const latestProgress = [...run.events].reverse().find((event) => event.caseId || event.status);
  const statusLabel = { queued: "排队中", preparing: "准备中", running: "运行中", scoring: "评分中", complete: "完成", failed: "失败", cancelled: "已停止" }[run.status] || run.status;
  return (
    <A2UIRow>
      <div className="run-result-stack">
        <section className={`run-card status-${run.status}`} aria-live="polite">
          <header className="run-card-head">
            <div><small>#{run.id.slice(-6)}</small><span>{complete ? `评测已完成 · ${formatTime(run.completedAt, true)}` : `${run.agentSnapshot.handle} 正在执行`}</span></div>
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
            <div className="metric"><strong>{run.costKnown === false ? "未提供" : run.cost == null ? "—" : formatCost(run.cost)}</strong><span>总成本</span></div>
          </div>
          <RunSteps run={run} />
          {complete && multiTrial && run.evaluationComplete === false && <div className="run-warning"><WarningCircle size={15} />有 {run.infraFailures + run.gradingFailures} 个异常 Trial 未计入质量分母</div>}
          {active && latestProgress && <div className="live-event"><CircleNotch size={13} className="spin" /><code>{eventLabel(latestProgress.type)}</code><span>{latestProgress.caseId || latestProgress.status}{latestProgress.reason ? ` · ${latestProgress.reason}` : ""}</span></div>}
          {active && <div className="run-controls"><button type="button" onClick={() => onCancel(run.id)}><Stop size={15} weight="fill" />停止运行</button></div>}
          {(run.status === "failed" || run.status === "cancelled") && <div className="run-error">{run.error || "运行已停止"}</div>}
        </section>
        {complete && (
          <div className="result-actions">
            <button type="button" className={showFailures ? "open" : ""} onClick={() => setShowFailures((value) => !value)} disabled={!failures.length}>
              {failures.length ? `查看 ${failures.length} 个失败` : "全部通过"}<CaretDown size={16} />
            </button>
            <button type="button" className={showLog ? "open" : ""} onClick={() => setShowLog((value) => !value)}>
              运行日志<CaretDown size={16} />
            </button>
          </div>
        )}
        {showFailures && (
          <div className="failure-list">
            {failures.map((failure) => (
              <div className="failure-item" key={failure.caseId}>
                <span className="failure-number">{failure.caseId}</span>
                <span className="failure-copy"><strong>{failure.title}</strong><small>{multiTrial ? `${failure.passCount}/${failure.validTrials} 次通过 · ` : ""}{failure.reason}</small></span>
                <span className="failure-cost">{failure.costKnown === false ? "成本未知" : failure.cost == null ? "—" : `$${failure.cost.toFixed(3)}`}</span>
                {failure.artifacts?.length > 0 && <button type="button" className="trace-action" onClick={async () => {
                  const artifact = failure.artifacts.find((item) => item.kind === "trace") || failure.artifacts.find((item) => item.kind === "result") || failure.artifacts[0];
                  setArtifactView({ title: `${failure.title} · ${artifact.kind}`, loading: true });
                  try {
                    const value = await rankApi.getArtifact(run.id, failure.caseId, artifact.path);
                    setArtifactView({ title: `${failure.title} · ${artifact.kind}`, content: value.content, truncated: value.truncated });
                  } catch (artifactError) {
                    setArtifactView({ title: `${failure.title} · ${artifact.kind}`, error: artifactError.message });
                  }
                }}>查看轨迹</button>}
                {failure.trials?.length > 0 && <details className="trial-details">
                  <summary>查看 {failure.trials.length} 次运行</summary>
                  <div>{failure.trials.map((trial) => <span key={trial.id} className={trial.valid ? (trial.passed ? "pass" : "fail") : "invalid"}>
                    <strong>#{trial.trialIndex} {trial.valid ? (trial.passed ? "通过" : "未通过") : "无效"}</strong>
                    <small>{trial.reason}</small>
                  </span>)}</div>
                </details>}
              </div>
            ))}
          </div>
        )}
        {artifactView && (
          <div className="artifact-view">
            <header><strong>{artifactView.title}</strong><button type="button" onClick={() => setArtifactView(null)} aria-label="关闭轨迹"><X size={15} /></button></header>
            {artifactView.loading ? <span>正在读取…</span> : artifactView.error ? <span>{artifactView.error}</span> : <pre>{artifactView.content}{artifactView.truncated ? "\n…内容已截断" : ""}</pre>}
          </div>
        )}
        {showLog && (
          <div className="event-log">
            {run.events.map((event, index) => (
              <div key={`${event.type}-${index}`}><time>{formatTime(event.at)}</time><code>{eventLabel(event.type)}</code><span title={event.reason || event.output || ""}>{event.caseId || event.status || ""}{event.reason ? ` · ${event.reason}` : ""}</span></div>
            ))}
          </div>
        )}
      </div>
    </A2UIRow>
  );
}

function Picker({ type, datasets, agents, selectedId, onClose, onSelect, onImport, onCreateAgent, onCreateDatasetVersion, onCreateAgentVersion }) {
  const isDataset = type === "dataset";
  const [expandedFamilyId, setExpandedFamilyId] = useState(null);
  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={onClose}>
      <section className="picker" role="dialog" aria-modal="true" aria-label={isDataset ? "选择测试集" : "选择 Agent"} onMouseDown={(event) => event.stopPropagation()}>
        <header>
          <div><small>{isDataset ? "DATASET" : "AGENT"}</small><h2>{isDataset ? "选择测试集" : "选择 Agent"}</h2></div>
          <button type="button" onClick={onClose} aria-label="关闭"><X size={20} /></button>
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
        <div className="picker-list asset-picker-list">
          {(isDataset ? datasets : agents).map((item) => {
            const connected = isDataset || item.runtime?.available;
            const expanded = expandedFamilyId === item.familyId;
            return (
              <div className={`asset-family ${expanded ? "expanded" : ""}`} key={item.familyId}>
                <div className="asset-family-main">
                  <button type="button" className="asset-select" onClick={() => onSelect(item.id)}>
                    <span className="picker-item-icon">{isDataset ? <Database size={20} /> : <Robot size={20} />}</span>
                    <span className="picker-copy">
                      <strong>{isDataset ? item.name : item.handle}</strong>
                      <small>{item.familyDescription || item.description}</small>
                      <em>{isDataset ? `${item.caseCount} 个用例` : `${item.runnerType} · ${item.model}`}</em>
                    </span>
                    <span className={`availability ${connected ? "available" : ""}`}>{selectedId === item.id ? "已选择" : connected ? "选择最新" : "选择（未连接）"}</span>
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
                        <button type="button" className={selectedId === version.id ? "selected" : ""} key={version.id} onClick={() => onSelect(version.id)}>
                          <span className="version-number">v{version.version}</span>
                          <span className="version-copy">
                            <strong>{isDataset ? `${version.caseCount} 个用例` : `${version.runnerType} · ${version.model}`}</strong>
                            <small>{version.source || version.description}</small>
                          </span>
                          <span className="version-date">{formatTime(version.createdAt, true)}</span>
                          <span className={`availability ${versionConnected ? "available" : ""}`}>{selectedId === version.id ? "已选择" : versionConnected ? "选择" : "选择（未连接）"}</span>
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
      </section>
    </div>
  );
}

function AgentForm({ family, baseVersion, onClose, onSubmit, busy }) {
  const isNewVersion = Boolean(family);
  const [form, setForm] = useState({
    name: family?.name || "",
    handle: family?.handle || "",
    runnerType: baseVersion?.runnerType || "dsh",
    model: baseVersion?.model?.startsWith("由") ? "" : baseVersion?.model || "",
    preset: baseVersion?.preset || "headless",
    systemPrompt: baseVersion?.systemPrompt || "",
    tools: baseVersion?.tools?.join(", ") || "web_search, browser",
    skills: baseVersion?.skills?.join(", ") || "",
    description: baseVersion?.description || "自定义 Agent 配置",
  });
  function update(field, value) { setForm((current) => ({ ...current, [field]: value })); }
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
          <label><span>Runner</span><select value={form.runnerType} onChange={(event) => update("runnerType", event.target.value)}><option value="dsh">DeepSeek Harness</option><option value="pi">Pi</option><option value="claude-code">Claude Code</option><option value="codex">Codex</option><option value="hermes">Hermes</option><option value="mock">Demo</option></select></label>
          <label><span>模型（可选）</span><input value={form.model} onChange={(event) => update("model", event.target.value)} placeholder="由运行时决定" /></label>
          <label><span>Preset</span><input value={form.preset} onChange={(event) => update("preset", event.target.value)} placeholder="headless" /></label>
          <label className="wide"><span>版本说明</span><input value={form.description} onChange={(event) => update("description", event.target.value)} placeholder="这次修改了什么" /></label>
          <label className="wide"><span>System Prompt</span><textarea rows="3" value={form.systemPrompt} onChange={(event) => update("systemPrompt", event.target.value)} placeholder="Agent 在每个用例中遵循的稳定指令" /></label>
          <label className="wide"><span>Runner 工具 ID</span><input value={form.tools} onChange={(event) => update("tools", event.target.value)} placeholder="web_search, browser, files" /><small>使用英文逗号分隔；能力由 Runner 插件提供</small></label>
          <label className="wide"><span>Skill 引用</span><input value={form.skills} onChange={(event) => update("skills", event.target.value)} placeholder="web-research, citation-check" /><small>运行会冻结这些 Skill 标识</small></label>
        </div>
        <footer><button type="button" onClick={onClose}>取消</button><button type="submit" className="primary-action" disabled={busy || !form.name.trim()}>{busy ? <CircleNotch size={18} className="spin" /> : <Plus size={18} />}{isNewVersion ? "创建版本并选择" : "创建并选择"}</button></footer>
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
              <span><strong>{item.title}</strong><small>{item.latestRun ? `${item.latestRun.passed}/${item.latestRun.total} 通过${item.latestRun.costKnown === false || item.latestRun.cost == null ? "" : ` · ${formatCost(item.latestRun.cost)}`}` : item.datasetVersionId || item.agentVersionId ? "配置中" : "尚未配置"}</small></span>
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
  const [picker, setPicker] = useState(null);
  const [agentFormOpen, setAgentFormOpen] = useState(false);
  const [agentVersionTarget, setAgentVersionTarget] = useState(null);
  const [datasetVersionTarget, setDatasetVersionTarget] = useState(null);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [trialCount, setTrialCount] = useState(5);
  const fileInputRef = useRef(null);
  const versionFileInputRef = useRef(null);
  const conversationEndRef = useRef(null);

  const agentRuntime = useMemo(() => bootstrap?.agents
    .flatMap((item) => item.versions || [item])
    .find((item) => item.id === experiment?.agentVersionId)?.runtime, [bootstrap, experiment]);
  const timeline = useMemo(() => {
    if (!experiment) return [];
    return [
      ...experiment.messages.map((message) => ({ type: "message", at: message.createdAt, data: message })),
      ...experiment.runs.map((run) => ({ type: "run", at: run.createdAt, data: run })),
    ].sort((a, b) => a.at.localeCompare(b.at));
  }, [experiment]);
  const activeRun = experiment?.runs.find((run) => ACTIVE_STATES.has(run.status));

  async function refreshBootstrap() {
    const next = await rankApi.bootstrap();
    setBootstrap(next);
    return next;
  }

  async function openExperiment(id) {
    const next = await rankApi.getExperiment(id);
    setExperiment(next);
    localStorage.setItem("rank.activeExperiment", id);
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
        const existing = nextBootstrap.experiments.find((item) => item.id === remembered);
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
    if (!activeRun) return undefined;
    const experimentId = experiment.id;
    let refreshing = false;
    let refreshPending = false;
    let closed = false;
    const lastSequence = activeRun.events.at(-1)?.sequence || 0;
    const stream = new EventSource(rankApi.runEventsURL(activeRun.id, lastSequence));
    const refreshRun = async () => {
      if (closed) return;
      if (refreshing) {
        refreshPending = true;
        return;
      }
      refreshing = true;
      try {
        const nextRun = await rankApi.getRun(activeRun.id);
        setExperiment((current) => current ? { ...current, runs: current.runs.map((run) => run.id === nextRun.id ? nextRun : run) } : current);
        if (!ACTIVE_STATES.has(nextRun.status)) {
          closed = true;
          stream.close();
          await openExperiment(experimentId);
          await refreshBootstrap();
        }
      } catch (streamError) {
        setError(streamError.message);
      } finally {
        refreshing = false;
        if (refreshPending && !closed) {
          refreshPending = false;
          void refreshRun();
        }
      }
    };
    stream.onmessage = () => void refreshRun();
    const eventNames = ["run.created", "run.status", "run.recovered", "trial.started", "trial.retry", "trial.completed", "trial.invalid", "case.started", "candidate.queued", "candidate.running", "candidate.harness.started", "candidate.harness.output", "candidate.harness.stdout", "candidate.harness.stderr", "candidate.completed", "candidate.failed", "judge.queued", "judge.running", "judge.harness.started", "judge.harness.output", "judge.harness.stdout", "judge.harness.stderr", "judge.completed", "judge.failed", "judge.verdict", "agent.message", "artifact.available", "case.completed", "run.completed"];
    for (const name of eventNames) stream.addEventListener(name, () => void refreshRun());
    stream.onerror = () => {
      if (!closed && stream.readyState === EventSource.CLOSED) void refreshRun();
    };
    return () => { closed = true; stream.close(); };
  }, [activeRun?.id]);

  useEffect(() => {
    conversationEndRef.current?.scrollIntoView({ behavior: "smooth", block: "nearest" });
  }, [timeline.length, activeRun?.results.length]);

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
    } catch (selectionError) {
      setError(selectionError.message);
    } finally {
      setBusy(false);
    }
  }

  async function sendMessage() {
    const text = composer.trim();
    if (!text || busy) return;
    setBusy(true);
    setComposer("");
    setError("");
    try {
      setExperiment(await rankApi.sendMessage(experiment.id, text));
      await refreshBootstrap();
    } catch (messageError) {
      setComposer(text);
      setError(messageError.message);
    } finally {
      setBusy(false);
    }
  }

  async function startRun() {
    setBusy(true);
    setError("");
    try {
      const action = experiment.a2ui?.actions?.start_run;
      if (!action) throw new Error("运行确认已过期，请刷新后重试");
      const result = await rankApi.executeCommand(experiment.id, action, { trialCount });
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
      const input = { ...form, tools: form.tools.split(",").map((item) => item.trim()).filter(Boolean), skills: form.skills.split(",").map((item) => item.trim()).filter(Boolean) };
      const agent = agentVersionTarget
        ? await rankApi.createAgentVersion(agentVersionTarget.familyId, input)
        : await rankApi.createAgent(input);
      await refreshBootstrap();
      const action = experiment.a2ui?.actions?.select_agent;
      if (!action) throw new Error("实验操作已过期，请刷新后重试");
      const selected = await rankApi.executeCommand(experiment.id, action, { agentVersionId: agent.id });
      setExperiment(selected.experiment);
      setAgentFormOpen(false);
      setAgentVersionTarget(null);
    } catch (agentError) {
      setError(agentError.message);
    } finally {
      setBusy(false);
    }
  }

  function handleComposerKeyDown(event) {
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
      event.preventDefault();
      void sendMessage();
    }
  }

  if (loading) return <div className="app-state"><CircleNotch size={24} className="spin" /><span>正在打开实验…</span></div>;
  if (!experiment || !bootstrap) return <div className="app-state error"><WarningCircle size={24} /><strong>Rank 没有启动</strong><span>{error}</span><button type="button" onClick={() => window.location.reload()}>重试</button></div>;

  return (
    <div className="app-shell">
      <header className="topbar">
        <a className="brand" href="#top" aria-label="Rank 首页">Rank</a>
        <div className="top-actions">
          <button type="button" onClick={createExperiment}><Plus size={18} />新实验</button>
          <button type="button" onClick={() => setHistoryOpen(true)}><ClockCounterClockwise size={19} />实验记录</button>
        </div>
      </header>

      <main className="workspace" id="top">
        <section className="experiment-heading">
          <span className="eyebrow">EXPERIMENT</span>
          <h1>{experiment.title}</h1>
          <small>{experiment.runs.length ? `${experiment.runs.length} 次运行` : "尚未运行"}</small>
        </section>

        <section className="conversation" aria-label="实验对话">
          {timeline.map((item) => item.type === "message"
            ? <Message message={item.data} key={item.data.id} />
            : <RunCard run={item.data} onCancel={cancelRun} key={item.data.id} />)}

          {!activeRun && (!experiment.dataset || !experiment.agent) && <SetupCard experiment={experiment} onPickDataset={() => setPicker("dataset")} onPickAgent={() => setPicker("agent")} />}
          {!activeRun && experiment.dataset && experiment.agent && <ReadyCard experiment={experiment} agentRuntime={agentRuntime} trialCount={trialCount} onTrialCount={setTrialCount} onPickDataset={() => setPicker("dataset")} onPickAgent={() => setPicker("agent")} onStart={startRun} busy={busy} />}
          <div ref={conversationEndRef} />
        </section>

        <section className="composer" aria-label="实验输入">
          {error && <div className="composer-error"><WarningCircle size={16} />{error}<button type="button" onClick={() => setError("")} aria-label="关闭错误"><X size={14} /></button></div>}
          <div className="composer-main">
            <label className="sr-only" htmlFor="task-input">输入消息</label>
            <textarea id="task-input" rows="2" value={composer} onChange={(event) => setComposer(event.target.value)} onKeyDown={handleComposerKeyDown} placeholder="描述目标、补充要求，或粘贴测试数据…" />
            <button className="send-button" type="button" onClick={sendMessage} disabled={!composer.trim() || busy} aria-label="发送消息">
              {busy ? <CircleNotch size={19} className="spin" /> : <PaperPlaneRight size={19} weight="fill" />}
            </button>
          </div>
          <div className="quick-actions">
            <button type="button" onClick={() => setPicker("dataset")}><Database size={18} />{experiment.dataset ? experiment.dataset.name : "选择测试集"}</button>
            <button type="button" onClick={() => setPicker("agent")}><Robot size={18} />{experiment.agent ? experiment.agent.handle : "选择 Agent"}</button>
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
        onClose={() => setPicker(null)}
        onSelect={(id) => updateSelection(picker === "dataset" ? { datasetVersionId: id } : { agentVersionId: id })}
        onImport={() => { setDatasetVersionTarget(null); fileInputRef.current?.click(); }}
        onCreateDatasetVersion={(family) => { setDatasetVersionTarget(family); versionFileInputRef.current?.click(); }}
        onCreateAgent={() => { setPicker(null); setAgentVersionTarget(null); setAgentFormOpen(true); }}
        onCreateAgentVersion={(family) => { setPicker(null); setAgentVersionTarget(family); setAgentFormOpen(true); }}
      />}
      {agentFormOpen && <AgentForm
        family={agentVersionTarget}
        baseVersion={agentVersionTarget}
        onClose={() => { setAgentFormOpen(false); setAgentVersionTarget(null); }}
        onSubmit={createAgent}
        busy={busy}
      />}
      {historyOpen && <HistoryPanel experiments={bootstrap.experiments} currentId={experiment.id} onClose={() => setHistoryOpen(false)} onOpen={async (id) => { await openExperiment(id); setHistoryOpen(false); }} onNew={createExperiment} />}
    </div>
  );
}
