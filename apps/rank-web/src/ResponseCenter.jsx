import { useEffect, useState } from "react";
import { ArrowLeft, ArrowUpRight, ArrowsClockwise, Check, Copy, Plus, X } from "@phosphor-icons/react";
import { rankApi } from "./api.js";
import { Modal } from "./Modal.jsx";
import { SelectField } from "./SelectField.jsx";
import { ErrorNotice, PageHeading, Pagination, StatePill } from "./ManagementUI.jsx";
import { formatDateTime } from "./i18n.js";

const done = value => ["completed", "failed", "cancelled", "requires_action"].includes(value);
const query = () => new URLSearchParams(location.hash.split("?")[1]);
const label = (agents, id) => { const a = agents.flatMap(v => v.versions || [v]).find(v => v.id === id); return a ? `${a.name} · v${a.version}` : id; };
const elapsed = response => response.startedAt && response.completedAt ? `${((new Date(response.completedAt) - new Date(response.startedAt)) / 1000).toFixed(1)}s` : "—";
const price = (response, t) => response.costKnown ? `$${Number(response.cost).toFixed(6)}` : t("missingPrice");

function CopyButton({ value, name, onError }) {
  const [copied, setCopied] = useState(false);
  return <button type="button" title={name} aria-label={name} onClick={async () => { try { await navigator.clipboard.writeText(value); setCopied(true); } catch (e) { onError(e.message); } }}>{copied ? <Check size={16}/> : <Copy size={16}/>}</button>;
}

/** Case capture requires reviewed text and an independently authored expectation. */
function CaptureCase({ detail, tenantId, locale, agents, onClose, onEvaluate }) {
  const en = locale === "en";
  const text = (zh, english) => en ? english : zh;
  const [datasets, setDatasets] = useState(null);
  const [target, setTarget] = useState("");
  const [name, setName] = useState("");
  const [title, setTitle] = useState("");
  const [input, setInput] = useState(detail.input || "");
  const [expected, setExpected] = useState("");
  const [reviewed, setReviewed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState(null);
  const [revision, setRevision] = useState(0);
  useEffect(() => { let stale = false; rankApi.bootstrap().then(b => { if (!stale) setDatasets(b.datasets); }).catch(e => { if (!stale) setError(e.message); }); return () => { stale = true; }; }, [revision]);
  const agent = agents.flatMap(v => v.versions || [v]).find(v => v.id === detail.response.agentVersionId);
  async function save(event) {
    event.preventDefault(); setBusy(true); setError("");
    try { setSaved(await rankApi.captureResponseCase(detail.response.id, { tenantId, ...(target ? { baseDatasetVersionId: target } : { datasetName: name }), title, input, expected, reviewed })); }
    catch (e) { setError(e.message); } finally { setBusy(false); }
  }
  return <Modal label={text("加入数据集", "Add to dataset")} onClose={busy ? () => {} : onClose}>
    <form className="response-capture management-form" onSubmit={save}>
      <header className="detail-title"><h2>{text("加入数据集", "Add to dataset")}</h2><button type="button" disabled={busy} aria-label={text("关闭", "Close")} onClick={onClose}><X size={19}/></button></header>
      <ErrorNotice error={error}/>
      {saved ? <><p role="status">{text("已保存", "Saved")} · {saved.name} v{saved.version} · {saved.caseCount} {text("条用例", "cases")}</p><p className="response-muted">{text("来源已保留，历史版本与评测结果不变。", "Source retained; earlier versions and evaluations are unchanged.")}</p><div className="management-actions">{agent && <button type="button" className="primary-action" disabled={busy} onClick={async () => { setBusy(true); try { if (onEvaluate) { await onEvaluate(agent, saved); onClose(); } else location.hash = `/responses?responseId=${encodeURIComponent(detail.response.id)}&evaluateDataset=${encodeURIComponent(saved.id)}`; } catch (e) { setError(e.message); } finally { setBusy(false); } }}>{text("去评测", "Evaluate")}<ArrowUpRight size={15}/></button>}<button type="button" onClick={onClose}>{text("完成", "Done")}</button></div></> : <>
        <label>{text("数据集", "Dataset")}<SelectField aria-label={text("数据集", "Dataset")} disabled={busy || !datasets} value={target} options={[{ value: "", label: text("新建数据集", "New dataset") }, ...(datasets || []).map(d => ({ value: d.id, label: `${d.name} · v${d.version}` }))]} onValueChange={value => { setTarget(value); setReviewed(false); }}/></label>
        {!target && <label>{text("数据集名称", "Dataset name")}<input required maxLength={100} value={name} onChange={e => setName(e.target.value)}/></label>}
        <label>{text("用例标题", "Case title")}<input required maxLength={200} value={title} onChange={e => { setTitle(e.target.value); setReviewed(false); }}/></label>
        {detail.response.previousResponseId && <p className="response-muted">{text("这是多轮调用。请将复现所需的历史信息整理进输入，保存的是独立文本用例。", "This is a continuation. Include the context needed to reproduce it; the saved case is an independent text input.")}</p>}
        {detail.instructions && <details><summary>{text("本轮追加指令", "Additional instructions")}</summary><pre className="readable-output">{detail.instructions}</pre></details>}
        {!!detail.callerTools?.length && <p className="response-muted">{text("外部工具不会自动随用例执行；请将所需工具结果整理成文本上下文。", "External tools are not replayed automatically; include the required tool results as text context.")}</p>}
        <label>{text("输入（可编辑脱敏）", "Input (review and redact)")}<textarea required rows={5} maxLength={100000} value={input} onChange={e => { setInput(e.target.value); setReviewed(false); }}/></label>
        <label>{text("期望结果", "Expected result")}<textarea required rows={3} maxLength={20000} value={expected} onChange={e => { setExpected(e.target.value); setReviewed(false); }} placeholder={text("描述应该怎样回答，以及哪些事实必须满足", "Describe the required answer and facts")}/></label>
        <label className="check-label"><input type="checkbox" checked={reviewed} onChange={e => setReviewed(e.target.checked)}/>{text("已检查敏感信息，并确认输入和期望结果", "I reviewed sensitive information, input and expected result")}</label>
        <footer className="management-actions"><button className="primary-action" disabled={busy || !datasets || !reviewed || !input.trim() || !expected.trim() || !title.trim() || (!target && !name.trim())}>{text("保存用例", "Save case")}</button>{error && <button type="button" onClick={() => { setRevision(v => v + 1); setTarget(""); setError(""); }}>{text("刷新数据集", "Refresh datasets")}</button>}</footer>
      </>}
    </form>
  </Modal>;
}

/** URL-addressable operator history and reviewed conversion into immutable evaluation data. */
export function ResponseCenter({ tenantId, deployments, agents, locale, t, scopedDeploymentId = "", onEvaluate }) {
  const en = locale === "en";
  const text = (zh, english) => en ? english : zh;
  const [params, setParams] = useState(query);
  const [search, setSearch] = useState(() => query().get("query") || "");
  const [page, setPage] = useState(null);
  const [detail, setDetail] = useState(null);
  const [revision, setRevision] = useState(0);
  const [error, setError] = useState("");
  const [tab, setTab] = useState("content");
  const [capture, setCapture] = useState(false);
  const selected = scopedDeploymentId ? "" : params.get("responseId") || "";
  const status = params.get("status") || "";
  const deployment = scopedDeploymentId || params.get("endpoint") || "";
  const conversation = params.get("conversationId") || "";
  const before = params.get("before") || "";
  const cursors = (params.get("cursors") || "").split(",").filter(Boolean);
  const searchQuery = params.get("query") || "";
  useEffect(() => { const update = () => setParams(query()); window.addEventListener("hashchange", update); return () => window.removeEventListener("hashchange", update); }, []);
  useEffect(() => { setSearch(searchQuery); }, [searchQuery]);
  function route(values) {
    const next = new URLSearchParams(params);
    if (values.before === "") next.delete("cursors");
    for (const [key, value] of Object.entries(values)) value ? next.set(key, value) : next.delete(key);
    if (scopedDeploymentId) next.set("endpoint", scopedDeploymentId);
    next.delete("deploymentId");
    location.hash = `/responses${next.size ? "?" + next : ""}`;
  }
  useEffect(() => {
    if (selected) return;
    let stale = false; setPage(null); setError("");
    rankApi.listResponses({ tenantId, status, deploymentId: deployment, query: searchQuery, conversationId: conversation, before }).then(value => { if (!stale) setPage(value); }).catch(e => { if (!stale) setError(e.message); });
    return () => { stale = true; };
  }, [tenantId, status, deployment, searchQuery, conversation, before, selected, revision]);
  useEffect(() => {
    if (!selected) { setDetail(null); return; }
    let stale = false, timer; setDetail(null); setError(""); setCapture(false); setTab("content");
    async function load() {
      try { const value = await rankApi.getResponse(selected, tenantId); if (stale) return; setDetail(value); if (!done(value.response.status)) timer = setTimeout(load, 1500); }
      catch (e) { if (!stale) setError(e.message); }
    }
    void load(); return () => { stale = true; clearTimeout(timer); };
  }, [tenantId, selected, revision]);
  useEffect(() => {
    const id = params.get("evaluateDataset");
    if (!id || !detail || !onEvaluate) return;
    let stale = false;
    rankApi.bootstrap().then(async b => {
      if (stale) return;
      const dataset = b.datasets.flatMap(v => v.versions || [v]).find(v => v.id === id);
      const agent = agents.flatMap(v => v.versions || [v]).find(v => v.id === detail.response.agentVersionId);
      if (!dataset || !agent) throw new Error(text("数据集或 Agent 不存在", "Dataset or Agent not found"));
      await onEvaluate(agent, dataset);
    }).catch(e => { if (!stale) setError(e.message); });
    return () => { stale = true; };
  }, [params.get("evaluateDataset"), detail?.response.id]);
  const response = detail?.response;
  return <section className={`management-page response-center ${scopedDeploymentId ? "scoped-responses" : ""}`}>
    <PageHeading title={t("responses")}><button title={t("refresh")} aria-label={t("refresh")} onClick={() => setRevision(v => v + 1)}><ArrowsClockwise size={16}/></button></PageHeading>
    <ErrorNotice error={error}/>
    {selected ? <>
      <button className="text-action" onClick={() => route({ responseId: "", evaluateDataset: "" })}><ArrowLeft size={15}/>{text("返回调用记录", "Back to invocations")}</button>
      {!detail && !error && <p role="status">{t("loading")}</p>}
      {response && <>
        <div className="detail-title response-title"><div><h2><a href={`#/deployments?deploymentId=${encodeURIComponent(response.deploymentId)}`}>{response.deploymentName || response.requestedReference}</a></h2><div className="response-id"><code>{response.id}</code><CopyButton value={response.id} name={text("复制请求 ID", "Copy request ID")} onError={setError}/><CopyButton value={location.href} name={text("复制详情链接", "Copy detail link")} onError={setError}/></div></div><div className="management-actions"><StatePill value={response.status} t={t}/><button disabled={!done(response.status)} onClick={() => setCapture(true)}><Plus size={16}/>{text("加入数据集", "Add to dataset")}</button></div></div>
        {response.failure && <div className="response-failure" role="alert"><strong>{response.failure.code}</strong><p>{response.failure.message}</p><p>{text(response.status === "cancelled" ? "调用已取消，继续交互请开启新会话。" : "检查该版本的模型连接、工具配置和下方执行事件。", response.status === "cancelled" ? "Invocation cancelled. Start a new conversation to continue." : "Check this version's connection, tools and execution events below.")}</p></div>}
        <dl className="detail-facts response-facts">
          <div><dt>{t("agentVersion")}</dt><dd><a href={`#/agents?agent=${encodeURIComponent(response.agentVersionId)}`}>{label(agents, response.agentVersionId)}<ArrowUpRight size={13}/></a></dd></div>
          <div><dt>{text("发起时间", "Requested at")}</dt><dd>{formatDateTime(locale, response.createdAt)}</dd></div>
          <div><dt>{t("latency")}</dt><dd>{elapsed(response)}</dd></div>
          <div><dt>Tokens</dt><dd>{response.usage.totalTokens} <small>{text("输入", "input")} {response.usage.inputTokens} · {text("输出", "output")} {response.usage.outputTokens}</small></dd></div>
          <div><dt>{t("cost")}</dt><dd>{price(response, t)}</dd></div>
        </dl>
        <div className="response-links">{response.previousResponseId && <a href={`#/responses?responseId=${encodeURIComponent(response.previousResponseId)}`}>{text("上一轮调用", "Previous turn")}<ArrowUpRight size={13}/></a>}<button className="inline-link" onClick={() => route({ responseId: "", conversationId: response.conversationId, before: "", query: "", status: "" })}>{text("查看整个会话", "View conversation")}<ArrowUpRight size={13}/></button></div>
        <div className="agent-detail-tabs" role="tablist" aria-label={text("调用详情", "Invocation detail")}>{[["content", text("输入与输出", "Input and output")], ["parameters", text("请求配置", "Request configuration")], ["events", text("执行事件", "Events")]].map(([key, name]) => <button role="tab" key={key} aria-selected={tab === key} onClick={() => setTab(key)}>{name}</button>)}</div>
        {tab === "content" && <div role="tabpanel"><section className="management-section"><h2>{t("input")}</h2><pre className="readable-output">{detail.input || text("无文本输入", "No text input")}</pre></section><section className="management-section"><h2>{t("output")}</h2><pre className="readable-output">{response.outputText || text(done(response.status) ? "未产生文本输出" : "等待输出…", done(response.status) ? "No text output" : "Waiting for output…")}</pre></section>{detail.toolCall && <section className="management-section"><h2>{t("toolCall")} · {detail.toolCall.name}</h2><pre className="readable-output">{JSON.stringify(detail.toolCall.arguments, null, 2)}</pre><p>{detail.toolCall.approvalStatus || ""}</p></section>}{detail.inputItems?.filter(v => v.type === "function_call_output").map(v => <section className="management-section" key={v.callId}><h2>{text("工具返回", "Tool output")} · {v.callId}</h2><pre className="readable-output">{v.output}</pre></section>)}</div>}
        {tab === "parameters" && <div role="tabpanel"><dl className="detail-facts"><div><dt>Conversation</dt><dd>{response.conversationId}</dd></div><div><dt>{t("releaseRevision")}</dt><dd>{response.releaseRevisionId || "—"}</dd></div><div><dt>Execution</dt><dd>{response.executionId || "—"}</dd></div></dl><section className="management-section"><h2>{text("追加指令", "Additional instructions")}</h2><pre className="readable-output">{detail.instructions || text("未设置", "Not set")}</pre></section><section className="management-section"><h2>{text("调用方工具", "Caller tools")}</h2>{detail.callerTools?.length ? <pre className="readable-output">{JSON.stringify(detail.callerTools, null, 2)}</pre> : <p>{text("未设置", "Not set")}</p>}</section><details><summary>{text("元数据与输出格式", "Metadata and output format")}</summary><pre className="readable-output">{JSON.stringify({ metadata: response.metadata, textFormat: response.textFormat }, null, 2)}</pre></details>{detail.inputItems?.some(v => v.contentDigest) && <section><h2>{text("附件", "Attachments")}</h2>{detail.inputItems.filter(v => v.contentDigest).map((v, i) => <p key={i}>{v.type} · {v.name || v.mediaType} · {v.contentDigest}</p>)}</section>}</div>}
        {tab === "events" && <div role="tabpanel"><ol className="event-timeline">{detail.events.map(event => <li key={event.sequence}><strong>{event.type}</strong><time>{formatDateTime(locale, event.createdAt)}</time><details><summary>{text("事件数据", "Event data")}</summary><pre className="readable-output">{JSON.stringify(event.data, null, 2)}</pre></details></li>)}</ol></div>}
        {capture && <CaptureCase detail={detail} tenantId={tenantId} locale={locale} agents={agents} onClose={() => setCapture(false)} onEvaluate={onEvaluate}/>}
      </>}
    </> : <>
      <form className="management-filters response-filters" onSubmit={event => { event.preventDefault(); route({ query: search.trim(), before: "" }); }}><input aria-label={text("搜索请求 ID 或端点", "Search request ID or endpoint")} placeholder={text("搜索请求 ID 或端点", "Search request ID or endpoint")} value={search} onChange={e => setSearch(e.target.value)}/><button type="submit">{text("搜索", "Search")}</button><SelectField aria-label={t("status")} value={status} onValueChange={value => route({ status: value, before: "" })} options={[{ value: "", label: t("allStatuses") }, ...["queued", "in_progress", "completed", "failed", "cancelled", "requires_action"].map(value => ({ value, label: t(value) }))]}/>{!scopedDeploymentId && <SelectField aria-label={t("deployments")} value={deployment} onValueChange={value => route({ endpoint: value, before: "" })} options={[{ value: "", label: t("allDeployments") }, ...deployments.map(d => ({ value: d.id, label: d.name }))]}/>}</form>
      {conversation && <div className="response-links"><span>{text("同一会话", "Same conversation")} · <code>{conversation}</code></span><button onClick={() => route({ conversationId: "", before: "" })}>{text("清除", "Clear")}</button></div>}
      {!page && !error && <p role="status">{t("loading")}</p>}
      {page?.items.length === 0 && <p>{text("没有匹配的调用记录", "No matching invocations")}</p>}
      {!!page?.items.length && <div className="management-table-wrap"><table className="management-table"><thead><tr>{[t("request"), t("agentVersion"), t("status"), t("latency"), "Tokens", t("cost"), text("发起时间", "Requested at")].map(name => <th key={name}>{name}</th>)}</tr></thead><tbody>{page.items.map(item => <tr key={item.id}><td><a className="table-link" onClick={event => { if (!event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey) { event.preventDefault(); route({ responseId: item.id }); } }} href={`#/responses?responseId=${encodeURIComponent(item.id)}${deployment ? "&endpoint=" + encodeURIComponent(deployment) : ""}`}>{item.deploymentName || item.requestedReference}<small>{item.id}</small></a></td><td>{label(agents, item.agentVersionId)}</td><td><StatePill value={item.status} t={t}/>{item.failure && <small className="response-failure-code" title={item.failure.message}>{item.failure.code}</small>}</td><td>{elapsed(item)}</td><td>{item.usage.totalTokens}</td><td>{price(item, t)}</td><td>{formatDateTime(locale, item.createdAt)}</td></tr>)}</tbody></table></div>}
      {page && <Pagination pageNumber={cursors.length + (before ? 2 : 1)} canPrevious={!!before} canNext={!!page.nextCursor} onPrevious={() => route({ before: cursors.at(-1) || "", cursors: cursors.slice(0, -1).join(",") })} onNext={() => route({ before: page.nextCursor, cursors: [...cursors, ...(before ? [before] : [])].join(",") })} t={t}/>}
    </>}
  </section>;
}
