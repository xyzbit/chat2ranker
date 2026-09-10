import { SelectField } from "./SelectField.jsx";
import { useEffect, useState } from "react";
import { ArrowLeft, ArrowRight, ArrowsClockwise, Plus, Robot } from "@phosphor-icons/react";
import { rankApi } from "./api.js";
import { formatDateTime } from "./i18n.js";
import { PageHeading, ErrorNotice, StatePill, EmptyState, Pagination, InlineConfirmation } from "./ManagementUI.jsx";

function cost(value, t) { return value.costKnown ? `$${Number(value.cost).toFixed(6)}` : t("missingPrice"); }
function duration(value) { return value.startedAt && value.completedAt ? `${((new Date(value.completedAt) - new Date(value.startedAt)) / 1000).toFixed(1)}s` : "—"; }
function agentLabel(agents, id) { const v = agents.flatMap((a) => a.versions || [a]).find((a) => a.id === id); return v ? `${v.name} · v${v.version}` : id; }

/** Agent browsing preserves the exact selected version across inspect and edit actions. */
export function AgentCenter({ agents, deployments, experiments, t, onCreate, onVersion, onUse, onEvaluation }) {
  const [query, setQuery] = useState("");
  const [selectedId, setSelectedId] = useState("");
  const [compareId, setCompareId] = useState("");
  const [evaluations, setEvaluations] = useState(null);
  const [error, setError] = useState("");
  const selected = agents.flatMap((a) => a.versions || [a]).find((a) => a.id === selectedId);
  const family = agents.find((a) => a.familyId === selected?.familyId);
  const compared = family?.versions?.find((a) => a.id === compareId && a.id !== selectedId);
  useEffect(() => {
    if (!selectedId) return;
    let cancelled = false; setEvaluations(null); setError("");
    rankApi.listAgentEvaluations(selectedId).then((all) => {
      if (!cancelled) setEvaluations(all);
    }).catch((e) => { if (!cancelled) setError(e.message); });
    return () => { cancelled = true; };
  }, [selectedId, experiments]);
  const visible = agents.filter((a) => `${a.name} ${a.handle} ${a.model} ${a.runnerType}`.toLowerCase().includes(query.toLowerCase()));
  return <main className="management-page">
    <PageHeading title={t("agentLibrary")} hint={t("agentLibraryHint")}><button className="primary-action" onClick={onCreate}><Plus size={17} />{t("newAgent")}</button></PageHeading>
    {!selected ? <><input className="management-search" aria-label={t("findAgents")} placeholder={t("findAgents")} value={query} onChange={(e) => setQuery(e.target.value)} />
      <div className="management-list">{visible.map((agent) => <button className="agent-list-row" key={agent.familyId} onClick={() => setSelectedId(agent.id)}><Robot size={23} /><span><strong>{agent.name}</strong><small>{agent.handle}</small></span><span>{agent.model}</span><span>{agent.runnerType}</span><span>v{agent.version}</span><ArrowRight size={16} /></button>)}</div>
      {!visible.length && <EmptyState title={t(agents.length ? "noMatch" : "noAgents")} />}</> : <>
      <button className="text-action" onClick={() => { setSelectedId(""); setCompareId(""); }}><ArrowLeft size={15} />{t("agentLibrary")}</button>
      <div className="detail-title"><div><h2>{selected.name}</h2><p>{selected.handle}</p></div><div className="management-actions"><button onClick={() => onUse(selected)}>{t("useInChat")}</button><button className="primary-action" onClick={() => onVersion(selected)}>{t("newVersion")}</button></div></div>
      <div className="management-filters"><label>{t("versionHistory")}<SelectField value={selectedId} onValueChange={nextValue => { setSelectedId(nextValue); if (nextValue === compareId) setCompareId(""); }} aria-label={t("versionHistory")} options={[...(family?.versions.map(v => ({ value: v.id, label: <>v{v.version} · {v.description}</> })) ?? [])]} /></label><label>{t("compareWith")}<SelectField value={compareId} onValueChange={setCompareId} aria-label={t("compareWith")} options={[{ value: "", label: t("noComparison") }, ...(family?.versions.filter(v => v.id !== selectedId).map(v => ({ value: v.id, label: <>v{v.version}</> })) ?? [])]} /></label></div>
      <div className="version-comparison">{[compared, selected].filter(Boolean).map((v) => <section key={v.id}><h3>v{v.version}{v.id === selectedId && compared ? " ←" : ""}</h3><dl className="detail-facts">{["runnerType", "model", "description", "tools", "skills"].map((key) => <div key={key}><dt>{t(({ runnerType: "runner", skills: "Skills" })[key] || key)}</dt><dd>{Array.isArray(v[key]) ? v[key].join(", ") || "—" : v[key] || "—"}</dd></div>)}</dl><h3>{t("systemPrompt")}</h3><pre className="readable-output">{v.systemPrompt || t("emptyValue")}</pre></section>)}</div>
      <section className="management-section"><h2>{t("connectedDeployments")}</h2><p>{deployments.filter((d) => d.agentFamilyId === selected.familyId).map((d) => d.name).join(" · ") || "—"}</p></section>
      <section className="management-section"><h2>{t("evaluationEvidence")}</h2><ErrorNotice error={error} />{!evaluations && !error && <p role="status">{t("loading")}</p>}{evaluations?.map((r) => <button className="evaluation-row" key={r.id} onClick={async () => { try { await onEvaluation(await rankApi.getRun(r.id)); } catch (e) { setError(e.message); } }}><span>{r.datasetName} · v{r.datasetVersion}</span><span>{r.total} {t("cases")}</span><span>{r.passed}/{r.total}</span><span>{cost(r, t)}</span><StatePill value={r.status} t={t} /></button>)}{evaluations?.length === 0 && <p>{t("noEvaluation")}</p>}</section>
    </>}
  </main>;
}

/** Bounded request history; stale fetches cannot replace a newer filter or selection. */
export function ResponseCenter({ tenantId, deployments, agents, locale, t, scopedDeploymentId = "" }) {
  const [status, setStatus] = useState(""); const [deployment, setDeployment] = useState(scopedDeploymentId);
  const [cursors, setCursors] = useState([""]); const [page, setPage] = useState(null);
  const [selected, setSelected] = useState(null); const [detail, setDetail] = useState(null);
  const [revision, setRevision] = useState(0); const [error, setError] = useState("");
  const cursor = cursors.at(-1);
  useEffect(() => { let stale = false; setPage(null); setError(""); rankApi.listResponses({ tenantId, status, deploymentId: deployment, before: cursor }).then((v) => { if (!stale) setPage(v); }).catch((e) => { if (!stale) setError(e.message); }); return () => { stale = true; }; }, [tenantId, status, deployment, cursor, revision]);
  useEffect(() => { if (!selected) return; let stale = false; setDetail(null); setError(""); rankApi.getResponse(selected, tenantId).then((v) => { if (!stale) setDetail(v); }).catch((e) => { if (!stale) setError(e.message); }); return () => { stale = true; }; }, [selected, tenantId, revision]);
  return <section className={`management-page ${scopedDeploymentId ? "scoped-responses" : ""}`}><PageHeading title={t("responses")} hint={t("responsesHint")}><button onClick={() => setRevision((v) => v + 1)}><ArrowsClockwise size={16} />{t("refresh")}</button></PageHeading><ErrorNotice error={error} />
    {selected ? <><button className="text-action" onClick={() => setSelected(null)}><ArrowLeft size={15} />{t("responses")}</button>{!detail && !error && <p role="status">{t("loading")}</p>}{detail && <>
      <div className="detail-title"><h2>{detail.response.requestedReference}</h2><StatePill value={detail.response.status} t={t} /></div>
      <dl className="detail-facts">{[[t("request"),detail.response.id],[t("agentVersion"),agentLabel(agents,detail.response.agentVersionId)],[t("releaseRevision"),detail.response.releaseRevisionId || "—"],["Conversation",detail.response.conversationId],["Execution",detail.response.executionId || "—"],[t("latency"),duration(detail.response)],["Tokens",detail.response.usage.totalTokens],[t("cost"),cost(detail.response,t)]].map(([label,value]) => <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>
      {detail.response.failure && <ErrorNotice error={`${detail.response.failure.code}: ${detail.response.failure.message}`} />}
      <section className="management-section"><h2>{t("input")}</h2><pre className="readable-output">{detail.input || "—"}</pre></section>
      <section className="management-section"><h2>{t("output")}</h2><pre className="readable-output">{detail.response.outputText || "—"}</pre></section>
      {detail.toolCall && <section className="management-section"><h2>{t("toolCall")} · {detail.toolCall.name}</h2><pre className="readable-output">{JSON.stringify(detail.toolCall.arguments,null,2)}</pre></section>}
      <section className="management-section"><h2>{t("events")}</h2><ol className="event-timeline">{detail.events.map((e) => <li key={e.sequence}><strong>{e.type}</strong><time>{formatDateTime(locale,e.createdAt)}</time><details><summary>{t("configuration")}</summary><pre className="readable-output">{JSON.stringify(e.data,null,2)}</pre></details></li>)}</ol></section>
    </>}</> : <>
      <div className="management-filters"><SelectField aria-label={t("status")} value={status} onValueChange={nextValue => { setStatus(nextValue); setCursors([""]); }} options={[{ value: "", label: t("allStatuses") }, ...["queued", "in_progress", "completed", "failed", "cancelled", "requires_action"].map(s => ({ value: s, label: t(s) }))]} />{!scopedDeploymentId && <SelectField aria-label={t("deployments")} value={deployment} onValueChange={nextValue => { setDeployment(nextValue); setCursors([""]); }} options={[{ value: "", label: t("allDeployments") }, ...deployments.map(d => ({ value: d.id, label: d.name }))]} />}</div>
      {!page && !error && <p role="status">{t("loading")}</p>}{page?.items.length === 0 && <EmptyState title={t("noResponses")} hint={t("noResponsesHint")} />}
      {!!page?.items.length && <div className="management-table-wrap"><table className="management-table"><thead><tr>{["request","agentVersion","status","latency","cost","publishedAt"].map((k) => <th key={k}>{t(k)}</th>)}</tr></thead><tbody>{page.items.map((r) => <tr key={r.id}><td><button className="table-link" onClick={() => setSelected(r.id)}>{r.requestedReference}<small>{r.id}</small></button></td><td>{agentLabel(agents,r.agentVersionId)}</td><td><StatePill value={r.status} t={t} /></td><td>{duration(r)}</td><td>{cost(r,t)}</td><td>{formatDateTime(locale,r.createdAt)}</td></tr>)}</tbody></table></div>}
      <Pagination pageNumber={cursors.length} canPrevious={cursors.length > 1 && Boolean(page)} canNext={Boolean(page?.nextCursor)} onPrevious={() => setCursors((v) => v.slice(0, -1))} onNext={() => setCursors((v) => [...v, page.nextCursor])} t={t} />
    </>}
  </section>;
}

/** API keys are shown once; list and revoke use only redacted persisted records. */
export function CredentialCenter({ tenantId, deployments, t, locale }) {
  const [items,setItems]=useState(null); const [error,setError]=useState(""); const [busy,setBusy]=useState(false);
  const [creating,setCreating]=useState(false); const [name,setName]=useState(""); const [ids,setIds]=useState([]);
  const [scopes,setScopes]=useState(["responses:create","responses:read"]); const [secret,setSecret]=useState(""); const [revoke,setRevoke]=useState(null); const [copied,setCopied]=useState(false);
  async function refresh(){setItems(await rankApi.listCredentials(tenantId));}
  useEffect(() => { let stale=false;rankApi.listCredentials(tenantId).then((v)=>{if(!stale)setItems(v);}).catch((e)=>{if(!stale)setError(e.message);});return()=>{stale=true;}; },[tenantId]);
  function toggle(setter,id){setter((v)=>v.includes(id)?v.filter((x)=>x!==id):[...v,id]);}
  async function create(e){e.preventDefault();setBusy(true);setError("");try{const v=await rankApi.createCredential({tenantId,name,scopes,deploymentIds:ids});setSecret(v.apiKey);setCopied(false);setCreating(false);setName("");await refresh();}catch(e){setError(e.message);}finally{setBusy(false);}}
  async function confirmRevoke(){setBusy(true);setError("");try{await rankApi.revokeCredential(revoke.id,tenantId);setRevoke(null);await refresh();}catch(e){setError(e.message);}finally{setBusy(false);}}
  return <main className="management-page"><PageHeading title={t("credentials")} hint={t("credentialsHint")}><button className="primary-action" disabled={!deployments.length} onClick={()=>{setCreating(true);setSecret("");}}><Plus size={17}/>{t("createKey")}</button></PageHeading><ErrorNotice error={error}/>
    {secret && <section className="secret-reveal"><h2>{t("keyOnce")}</h2><code>{secret}</code><div className="management-actions"><button onClick={async()=>{try{await navigator.clipboard.writeText(secret);setCopied(true);}catch(e){setError(e.message);}}}>{t(copied?"copied":"copyKey")}</button><button onClick={()=>setSecret("")}>{t("done")}</button></div></section>}
    {creating && <form className="management-form" onSubmit={create}><h2>{t("createKey")}</h2><label>{t("credentialName")}<input required maxLength={120} value={name} onChange={(e)=>setName(e.target.value)}/></label><fieldset><legend>{t("deployments")}</legend>{deployments.map((d)=><label className="check-label" key={d.id}><input type="checkbox" checked={ids.includes(d.id)} onChange={()=>toggle(setIds,d.id)}/>{d.name}</label>)}</fieldset><fieldset><legend>{t("scopes")}</legend>{["responses:create","responses:read","responses:cancel","responses:approve"].map((s)=><label className="check-label" key={s}><input type="checkbox" checked={scopes.includes(s)} onChange={()=>toggle(setScopes,s)}/>{t(s)}</label>)}</fieldset><div className="management-actions"><button type="button" onClick={()=>setCreating(false)}>{t("cancel")}</button><button className="primary-action" disabled={busy||!name.trim()||!ids.length||!scopes.length}>{t("createKey")}</button></div></form>}
    {revoke && <InlineConfirmation title={`${t("revokeKey")} · ${revoke.name}`} hint={t("revokeHint")} confirmLabel={t("confirmRevoke")} busy={busy} onCancel={() => setRevoke(null)} onConfirm={confirmRevoke} t={t} />}
    {!items && !error && <p role="status">{t("loading")}</p>}{items?.length===0 && <EmptyState title={t("noCredentials")} hint={!deployments.length?t("emptyDeploymentsHint"):t("credentialsHint")}/>}
    {items?.map((v)=><section className="credential-row" key={v.id}><div className="detail-title"><h2>{v.name}</h2><StatePill value={v.revokedAt?"revoked":"active"} t={t}/></div><code>{v.keyPrefix}••••••••</code><p>{v.deploymentIds.map((id)=>deployments.find((d)=>d.id===id)?.name||id).join(" · ")}</p><div className="credential-meta"><span>{v.scopes.map((s)=>t(s)).join(" · ")}</span><time>{formatDateTime(locale,v.createdAt)}</time>{!v.revokedAt && <button onClick={()=>setRevoke(v)}>{t("revokeKey")}</button>}</div></section>)}
  </main>;
}
