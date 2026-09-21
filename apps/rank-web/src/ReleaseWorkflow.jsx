import { useEffect, useRef, useState } from "react";
import { ArrowRight, ArrowsClockwise, CheckCircle, CircleNotch, RocketLaunch, X } from "@phosphor-icons/react";
import { rankApi } from "./api.js";
import { SelectField } from "./SelectField.jsx";
import { ErrorNotice } from "./ManagementUI.jsx";
import { ReleaseChecks } from "./ReleasePolicy.jsx";
import { versionLabel } from "./DeploymentDetails.jsx";
import { formatDateTime } from "./i18n.js";

const active = task => ["ready", "regression"].includes(task.status);
const words = {
  zh: { defaults: "默认测试集", development: "开发评测集", regression: "发布回归集", none: "未设置", run: "发布前运行回归测试", start: "开始发布", compare: "运行版本对比", comparing: "正在评测版本", missing: "暂无同口径评测记录", first: "首次发布", passed: "通过率", coverage: "有效覆盖率", cost: "总费用", duration: "耗时", current: "当前版本", candidate: "待发布版本", delta: "变化", refresh: "刷新评测", ready: "等待发布", regressionStatus: "回归测试中", published: "已发布", blocked: "回归未通过", conflict: "发布目标已变化", cancelled: "已取消", skipped: "本次未运行回归", details: "发布详情", override: "填写原因后继续发布", reason: "继续发布的原因", retry: "重新确认发布", records: "发布任务", rules: "通过率至少", coverageRule: "有效覆盖率至少", noPrice: "未配置价格", cases: "用例", trials: "次 / 用例", completed: "已完成", versions: "评测记录", save: "保存", saved: "已保存", cancel: "取消发布", loading: "加载中", reload: "重新加载", noTasks: "暂无发布任务" },
  en: { defaults: "Default datasets", development: "Development evaluation", regression: "Release regression", none: "Not set", run: "Run regression before publishing", start: "Start release", compare: "Evaluate versions", comparing: "Evaluating versions", missing: "No comparable evaluations", first: "First release", passed: "Pass rate", coverage: "Valid coverage", cost: "Total cost", duration: "Duration", current: "Current version", candidate: "Candidate version", delta: "Change", refresh: "Refresh evaluations", ready: "Waiting to publish", regressionStatus: "Running regression", published: "Published", blocked: "Regression failed", conflict: "Release target changed", cancelled: "Cancelled", skipped: "Regression skipped", details: "Release details", override: "Publish with reason", reason: "Reason to proceed", retry: "Review a new release", records: "Release tasks", rules: "Minimum pass rate", coverageRule: "Minimum valid coverage", noPrice: "Missing pricing", cases: "cases", trials: "trials / case", completed: "completed", versions: "Evaluation records", save: "Save", saved: "Saved", cancel: "Cancel release", loading: "Loading", reload: "Reload", noTasks: "No release tasks" },
};
function copy(locale) { return words[locale?.startsWith("en") ? "en" : "zh"]; }
const datasetVersions = datasets => datasets.flatMap(item => item.versions || [item]);
const datasetOptions = (datasets, c) => [{ value: "", label: c.none }, ...datasetVersions(datasets).map(item => ({ value: item.id, label: `${item.name} v${item.version} · ${item.caseCount ?? item.cases?.length ?? 0} ${c.cases}` }))];

/** Family defaults resolve at draft creation and never rewrite immutable Agent versions. */
export function AgentEvaluationDefaultsEditor({ familyId, datasets, locale, compact = false }) {
  const c = copy(locale);
  const [value, setValue] = useState(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    let stale = false;
    setValue(null); setError(""); setSaved(false);
    rankApi.getAgentDefaults(familyId).then(result => { if (!stale) setValue(result); }).catch(e => { if (!stale) setError(e.message); });
    return () => { stale = true; };
  }, [familyId, revision]);
  async function save(event) {
    event.preventDefault(); setBusy(true); setError(""); setSaved(false);
    try { setValue(await rankApi.saveAgentDefaults(familyId, { ...value, expectedRevision: value.revision })); setSaved(true); }
    catch (e) { setError(e.message); } finally { setBusy(false); }
  }
  return <section className={compact ? "agent-defaults" : "management-section"}>{!compact && <h2>{c.defaults}</h2>}<ErrorNotice error={error} />
    {error && <button type="button" onClick={() => setRevision(v => v + 1)}>{c.reload}</button>}
    {value && <form className="management-form" onSubmit={save}><div className="form-grid">
      {[["developmentDatasetId", c.development], ["regressionDatasetId", c.regression]].map(([key, label]) => <label key={key}>{label}<SelectField aria-label={label} value={value[key]} disabled={busy} onValueChange={id => { setValue(v => ({ ...v, [key]: id })); setSaved(false); }} options={[{ value: "", label: c.none }, ...datasets.map(item => ({ value: item.familyId, label: item.name }))]} /></label>)}
    </div><div className="management-actions"><button className="primary-action" disabled={busy}>{c.save}</button><span role="status">{saved ? c.saved : ""}</span></div></form>}
  </section>;
}

function EvidenceComparison({ draft, agents, locale }) {
  const c = copy(locale);
  if (!draft.currentEvidence && !draft.candidateEvidence) return <p className="release-muted">{locale === "en" ? "No development evaluation yet. You can select a dataset above to evaluate this version." : "暂无开发评测记录，可选择上方数据集评测此版本。"}</p>;
  const rate = (e, field) => e?.[field] ? e.passed / e[field] * 100 : null;
  const metrics = [
    [c.passed, e => rate(e, "validTrials"), v => `${v.toFixed(1)}%`, " pp"],
    [c.coverage, e => e?.scheduledTrials ? e.validTrials / e.scheduledTrials * 100 : null, v => `${v.toFixed(1)}%`, " pp"],
    [c.cost, e => e?.costKnown ? e.cost : null, v => `$${v.toFixed(4)}`, ""],
    [c.duration, e => e ? e.durationMs / 1000 : null, v => `${v.toFixed(1)}s`, "s"],
  ];
  return <div className="release-metrics"><table><thead><tr><th /><th>{c.current}<strong>{draft.currentAgentVersionId ? versionLabel(agents, draft.currentAgentVersionId) : c.first}</strong></th><th>{c.candidate}<strong>{versionLabel(agents, draft.agentVersionId)}</strong></th><th>{c.delta}</th></tr></thead><tbody>
    {metrics.map(([label, get, format, unit]) => { const before = get(draft.currentEvidence); const after = get(draft.candidateEvidence); return <tr key={label}><th>{label}</th><td>{before == null ? label === c.cost && draft.currentEvidence ? c.noPrice : "—" : format(before)}</td><td>{after == null ? label === c.cost && draft.candidateEvidence ? c.noPrice : "—" : format(after)}</td><td>{draft.comparable && before != null && after != null ? `${after > before ? "+" : ""}${(after - before).toFixed(label === c.cost ? 4 : 1)}${unit}` : "—"}</td></tr>; })}
  </tbody></table>{!draft.comparable && draft.currentAgentVersionId && <p className="release-muted">{c.missing}</p>}
  <details><summary>{c.versions}</summary>{[draft.currentEvidence, draft.candidateEvidence].filter(Boolean).map((e, i) => <p className="release-muted" key={`${i}-${e.runId}`}>{versionLabel(agents, e.agentVersionId)} · {formatDateTime(locale, e.completedAt)} · {e.trialCount} {c.trials}</p>)}</details></div>;
}

/** The same review and durable task view is mounted in the deployment dialog and chat event. */
export function ReleaseWorkflow({ deployment, alias, agentVersionId, datasets, agents, locale, t, experimentId = "", reviewCommandId = "", initialTask = null, initialSelection = {}, allowRetry = true, onReleased }) {
  const c = copy(locale);
  const [draft, setDraft] = useState(null);
  const [selection, setSelection] = useState(initialSelection);
  const [revision, setRevision] = useState(0);
  const [task, setTask] = useState(initialTask);
  const [restored, setRestored] = useState(!reviewCommandId);
  const [regression, setRegression] = useState(true);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [reason, setReason] = useState("");
  const [comparison, setComparison] = useState(null);
  const [run, setRun] = useState(null);
  const attempt = useRef(null);
  const onReleasedRef = useRef(onReleased);
  onReleasedRef.current = onReleased;
  const notified = useRef(initialTask?.status === "published" ? initialTask.id : "");
  useEffect(() => {
    if (initialTask) setTask(current => current?.id === initialTask.id ? initialTask : current);
  }, [initialTask]);
  useEffect(() => {
    if (!reviewCommandId || restored) return;
    let stale = false;
    rankApi.listReleaseTasks({ experimentId }).then(items => { if (!stale) { setTask(items.find(v => v.reviewCommandId === reviewCommandId) || null); setRestored(true); } }).catch(e => { if (!stale) setError(e.message); });
    return () => { stale = true; };
  }, [experimentId, reviewCommandId, revision, restored]);
  const selectionKey = JSON.stringify(selection);
  useEffect(() => {
    if (task || !restored) return;
    let stale = false;
    setDraft(null); setError("");
    rankApi.releaseDraft(deployment.id, { alias, agentVersionId, ...JSON.parse(selectionKey) }).then(value => {
      if (!stale) {
        setDraft(value);
        if (!Object.hasOwn(JSON.parse(selectionKey), "developmentDatasetVersionId")) setSelection({ developmentDatasetVersionId: value.developmentDatasetVersionId, regressionDatasetVersionId: value.regressionDatasetVersionId });
      }
    }).catch(e => { if (!stale) setError(e.message); });
    return () => { stale = true; };
  }, [deployment.id, alias, agentVersionId, selectionKey, revision, task, restored]);
  useEffect(() => {
    if (!task) return;
    let stale = false; let timer;
    async function poll() {
      try {
        const next = await rankApi.getReleaseTask(task.id);
        const nextRun = next.runId ? await rankApi.getRun(next.runId) : null;
        if (stale) return;
        setTask(next); setRun(nextRun);
        if (next.status === "published" && notified.current !== next.id) { notified.current = next.id; await onReleasedRef.current?.(); }
        if (active(next)) timer = setTimeout(poll, 1000);
      } catch (e) { if (!stale) { setError(e.message); timer = setTimeout(poll, 3000); } }
    }
    void poll();
    return () => { stale = true; clearTimeout(timer); };
  }, [task?.id, task?.status]);
  useEffect(() => {
    if (!comparison) return;
    let stale = false; let timer;
    async function poll() {
      try {
        const view = await rankApi.getExperiment(comparison);
        if (stale) return;
        if (view.runs?.length && view.runs.every(r => ["complete", "failed", "cancelled"].includes(r.status))) { setComparison(null); setRevision(v => v + 1); }
        else timer = setTimeout(poll, 1000);
      } catch (e) { if (!stale) { setError(e.message); setComparison(null); } }
    }
    void poll(); return () => { stale = true; clearTimeout(timer); };
  }, [comparison]);
  async function act(fn) { setBusy(true); setError(""); try { await fn(); } catch (e) { setError(e.message); } finally { setBusy(false); } }
  function start() {
    return act(async () => {
      const input = { alias, agentVersionId, developmentDatasetVersionId: draft.developmentDatasetVersionId, regressionDatasetVersionId: draft.regressionDatasetVersionId, expectedReleaseRevisionId: draft.expectedReleaseRevisionId, expectedPolicyRevision: draft.policy.revision, expectedCandidateRunId: draft.candidateEvidence?.runId || "", expectedCurrentRunId: draft.currentEvidence?.runId || "", runRegression: regression, trialCount: 1, experimentId, reviewCommandId, actor: "local-operator" };
      const serialized = JSON.stringify(input);
      if (attempt.current?.input !== serialized) attempt.current = { input: serialized, key: crypto.randomUUID() };
      setTask(await rankApi.startReleaseTask(deployment.id, input, attempt.current.key));
    });
  }
  const shown = task || draft;
  const datasetName = id => { const ds = datasetVersions(datasets).find(v => v.id === id); return ds ? `${ds.name} v${ds.version} · ${ds.caseCount ?? ds.cases?.length ?? 0} ${c.cases}` : c.none; };
  return <div className="release-workflow">
    <ErrorNotice error={error} />
    {!shown && !error && <p role="status">{c.loading}</p>}
    {error && !task && <button type="button" onClick={() => setRevision(v => v + 1)}><ArrowsClockwise size={15} />{c.reload}</button>}
    {shown && <>
      {!task ? <label>{c.development}<SelectField aria-label={c.development} disabled={busy || !!comparison} value={draft.developmentDatasetVersionId} options={datasetOptions(datasets, c)} onValueChange={id => setSelection(v => ({ ...v, developmentDatasetVersionId: id }))} /></label> : <p className="release-muted">{c.development} · {datasetName(task.developmentDatasetVersionId)}</p>}
      <EvidenceComparison draft={shown} agents={agents} locale={locale} />
      {!task && <div className="management-actions"><button type="button" disabled={busy || !!comparison || !draft.developmentDatasetVersionId} onClick={() => act(async () => { const value = await rankApi.startReleaseComparison(deployment.id, { alias, agentVersionId, ...selection }); setComparison(value.id); })}>{comparison ? <CircleNotch className="spin" size={15} /> : <ArrowsClockwise size={15} />}{comparison ? c.comparing : c.compare}</button><button type="button" disabled={busy || !!comparison} onClick={() => setRevision(v => v + 1)}>{c.refresh}</button></div>}
      {!task ? <><label className="check-label"><input type="checkbox" checked={regression} disabled={busy} onChange={e => setRegression(e.target.checked)} />{c.run}</label>
        {regression && <label>{c.regression}<SelectField aria-label={c.regression} value={draft.regressionDatasetVersionId} disabled={busy} options={datasetOptions(datasets, c)} onValueChange={id => setSelection(v => ({ ...v, regressionDatasetVersionId: id }))} /></label>}
        {regression && <p className="release-muted">{draft.regressionDatasetVersionId ? `${c.rules} ${draft.policy.minPassRate}% · ${c.coverageRule} ${draft.policy.minValidTrialRate}%` : locale === "en" ? "Select a regression dataset, or disable regression for this release." : "请选择回归数据集，或关闭本次发布前回归。"}</p>}
        <div className="management-actions"><button type="button" className="primary-action" disabled={busy || !!comparison || (regression && !draft.regressionDatasetVersionId)} onClick={start}>{busy ? <CircleNotch size={16} className="spin" /> : <RocketLaunch size={16} />}{c.start}</button></div>
      </> : <>
        <ol className="release-pipeline" aria-label={c.records}><li data-state={task.runRegression ? task.status === "regression" ? "active" : task.evaluation?.passed ? "done" : "pending" : "skipped"}>{task.runRegression ? c.regression : c.skipped}</li><ArrowRight size={16} /><li data-state={task.status === "published" ? "done" : "pending"}>{c.published}</li></ol>
        <p role="status" className="release-task-status" data-status={task.status}>{active(task) && <CircleNotch size={16} className="spin" />}{task.status === "published" && <CheckCircle size={16} />}{task.status === "regression" ? c.regressionStatus : c[task.status]}</p>
        {run && <p className="release-muted">{datasetName(task.regressionDatasetVersionId)} · {run.completedTrials}/{run.scheduledTrials} {c.completed}</p>}
        {task.error && <p className="release-error">{task.error}</p>}
        <details><summary>{c.details}</summary>{task.evaluation && <ReleaseChecks evaluation={task.evaluation} t={t} />}{task.reason && <p>{task.reason}</p>}
          {task.status === "blocked" && <div className="release-override"><label>{c.reason}<textarea aria-label={c.reason} rows={2} value={reason} onChange={e => setReason(e.target.value)} /></label><button type="button" disabled={busy || !reason.trim()} onClick={() => act(async () => setTask(await rankApi.overrideReleaseTask(task.id, reason.trim())))}>{c.override}</button></div>}
          {run?.results?.map(result => <p className="release-case-result" key={result.caseId}><span>{result.title || result.caseId}</span><span>{result.passCount}/{result.trialCount}</span><span>{result.reason}</span></p>)}
        </details>
        <div className="management-actions">{active(task) && <button type="button" disabled={busy} onClick={() => act(async () => setTask(await rankApi.cancelReleaseTask(task.id)))}><X size={15} />{c.cancel}</button>}
          {allowRetry && !active(task) && task.status !== "published" && <button type="button" disabled={busy} onClick={() => { attempt.current = null; setTask(null); setRun(null); setReason(""); }}>{c.retry}</button>}
        </div>
      </>}
    </>}
  </div>;
}

export function ReleaseTaskHistory({ deployment, datasets, agents, locale, t, onReleased }) {
  const c = copy(locale);
  const [items, setItems] = useState(null);
  const [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  const releaseKey = deployment.aliases.map(alias => alias.currentRelease?.id || "").join(",");
  useEffect(() => {
    let stale = false; let timer;
    async function poll() {
      try { const value = await rankApi.listReleaseTasks({ deploymentId: deployment.id }); if (!stale) { setItems(value); setError(""); if (value.some(active)) timer = setTimeout(poll, 1500); } }
      catch (e) { if (!stale) setError(e.message); }
    }
    void poll(); return () => { stale = true; clearTimeout(timer); };
  }, [deployment.id, revision, releaseKey]);
  return <section className="management-section release-task-history"><div className="detail-title"><h2>{c.records}</h2><button type="button" onClick={() => setRevision(v => v + 1)} aria-label={c.refresh}><ArrowsClockwise size={16} /></button></div><ErrorNotice error={error} />
    {items?.length === 0 && <p>{c.noTasks}</p>}{items?.map(task => <details key={task.id}><summary><span>{versionLabel(agents, task.agentVersionId)}</span><span>{task.status === "regression" ? c.regressionStatus : c[task.status]}</span><time>{formatDateTime(locale, task.createdAt)}</time></summary><ReleaseWorkflow deployment={deployment} alias={task.alias} agentVersionId={task.agentVersionId} datasets={datasets} agents={agents} locale={locale} t={t} initialTask={task} allowRetry={false} onReleased={onReleased} /></details>)}
  </section>;
}
