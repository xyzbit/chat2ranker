import { SelectField } from "./SelectField.jsx";
import { useEffect, useState } from "react";
import { ArrowLeft, ArrowRight, ArrowUpRight, ArrowsClockwise, ChatCircle, Plus, Robot } from "@phosphor-icons/react";
import { rankApi } from "./api.js";
import { AgentEvaluationDefaultsEditor } from "./ReleaseWorkflow.jsx";
import { AgentVersionComparison } from "./AgentVersionComparison.jsx";
import { CredentialForm } from "./CredentialForm.jsx";
import { formatDateTime } from "./i18n.js";
import { PageHeading, ErrorNotice, StatePill, EmptyState, Pagination, InlineConfirmation } from "./ManagementUI.jsx";

function cost(value, t) { return value.costKnown ? `$${Number(value.cost).toFixed(6)}` : t("missingPrice"); }
function duration(value) { return value.startedAt && value.completedAt ? `${((new Date(value.completedAt) - new Date(value.startedAt)) / 1000).toFixed(1)}s` : "—"; }
function agentLabel(agents, id) { const v = agents.flatMap((a) => a.versions || [a]).find((a) => a.id === id); return v ? `${v.name} · v${v.version}` : id; }

/** Agent browsing preserves the exact selected version across inspect and edit actions. */
export function AgentCenter({ agents, selectedId, onSelect: setSelectedId, datasets = [], locale, experiments, t, onCreate, onVersion, onUse, onTrial, onEvaluation }) {
  const [query, setQuery] = useState("");
  const [comparing, setComparing] = useState(() => new URLSearchParams(location.hash.split("?")[1]).has("compare"));
  const [tab, setTab] = useState("configuration");
  const [evaluations, setEvaluations] = useState(null);
  const [error, setError] = useState("");
  const selected = agents.flatMap((a) => a.versions || [a]).find((a) => a.id === selectedId);
  const family = agents.find((a) => a.familyId === selected?.familyId);
  useEffect(() => { setTab("configuration"); }, [selected?.familyId]);
  useEffect(() => {
    const update = () => setComparing(new URLSearchParams(location.hash.split("?")[1]).has("compare"));
    window.addEventListener("hashchange", update);
    return () => window.removeEventListener("hashchange", update);
  }, []);
  useEffect(() => {
    if (!selectedId) return;
    let cancelled = false; setEvaluations(null); setError("");
    rankApi.listAgentEvaluations(selectedId).then((all) => {
      if (!cancelled) setEvaluations(all);
    }).catch((e) => { if (!cancelled) setError(e.message); });
    return () => { cancelled = true; };
  }, [selectedId, experiments]);
  const visible = agents.filter((a) => `${a.name} ${a.handle} ${a.model} ${a.runnerType}`.toLowerCase().includes(query.toLowerCase()));
  if (comparing && selected) return <AgentVersionComparison key={selected.id} selected={selected} family={family} t={t} />;
  return <main className="management-page agent-library">
    {!selected ? <><PageHeading title={t("agentLibrary")}><button className="primary-action" onClick={onCreate}><Plus size={17} />{t("newAgent")}</button></PageHeading><input className="management-search" aria-label={t("findAgents")} placeholder={t("findAgents")} value={query} onChange={(e) => setQuery(e.target.value)} />
      <div className="management-list">{visible.map((agent) => <button className="agent-list-row" key={agent.familyId} onClick={() => setSelectedId(agent.id)}><Robot size={23} /><span><strong>{agent.name}</strong><small>{agent.handle}</small></span><span>{agent.model}</span><span>{agent.runnerType}</span><span>v{agent.version}</span><ArrowRight size={16} /></button>)}</div>
      {!visible.length && <EmptyState title={t(agents.length ? "noMatch" : "noAgents")} />}</> : <>
      <button className="text-action" onClick={() => setSelectedId("")}><ArrowLeft size={15} />{t("agentLibrary")}</button>
      <header className="agent-detail-heading"><div><h1>{selected.name}</h1><p>{selected.handle}</p></div>
        <div className="management-actions">
          <a className="inline-link" href={`#/deployments?publish=${encodeURIComponent(selected.id)}`}>{t("goPublish")}<ArrowUpRight size={15} /></a>
          <button className="secondary-action" onClick={() => onTrial(selected)}><ChatCircle size={16} />{t("tryVersion")}</button>
        </div>
      </header>
      <div className="agent-detail-toolbar">
        <div className="agent-detail-tabs" role="tablist" aria-label={t("agentDetails")} onKeyDown={event => {
          if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
          event.preventDefault();
          const next = event.key === "Home" ? "configuration" : event.key === "End" ? "agentEvaluation" : tab === "configuration" ? "agentEvaluation" : "configuration";
          setTab(next);
          document.getElementById(`agent-tab-${next}`)?.focus();
        }}>
          {["configuration", "agentEvaluation"].map(value => <button key={value} id={`agent-tab-${value}`} role="tab" aria-selected={tab === value} aria-controls={`agent-panel-${value}`} tabIndex={tab === value ? 0 : -1} onClick={() => setTab(value)}>{t(value)}</button>)}
        </div>
        <SelectField value={selectedId} onValueChange={setSelectedId} aria-label={t("versionHistory")} options={(family?.versions || [selected]).map(v => ({ value: v.id, label: `v${v.version}` }))} />
      </div>
      <ErrorNotice error={error} />
      <section id="agent-panel-configuration" role="tabpanel" aria-labelledby="agent-tab-configuration" hidden={tab !== "configuration"}>
      <div className="agent-config-heading"><div className="agent-runtime-line"><span>{t("runner")}: {selected.runnerType}</span><span>{t("model")}: {selected.model}</span></div><button onClick={() => onVersion(selected)}><Plus size={16} />{t("newVersion")}</button></div>
      {selected.description && <p className="agent-description">{selected.description}</p>}
      <section className="agent-prompt"><h2>{t("systemPrompt")}</h2><pre>{selected.systemPrompt || t("emptyValue")}</pre></section>
      <section className="agent-resources"><h2>{t("agentResources")}</h2><dl className="detail-facts">{["tools", "skills"].map(key => <div key={key}><dt>{t(key === "skills" ? "Skills" : key)}</dt><dd>{selected[key]?.join(", ") || t("emptyValue")}</dd></div>)}</dl></section>
      {(family?.versions?.length || 0) > 1 && <a className="inline-link" href={`#/agents?agent=${encodeURIComponent(selected.id)}&compare=1`}>{t("versionCompare")}<ArrowUpRight size={15} /></a>}
      </section>
      <section id="agent-panel-agentEvaluation" role="tabpanel" aria-labelledby="agent-tab-agentEvaluation" hidden={tab !== "agentEvaluation"}>
        <section className="agent-evaluation-settings"><h2>{t("evaluationSettings")}</h2><AgentEvaluationDefaultsEditor key={selected.familyId} familyId={selected.familyId} datasets={datasets} locale={locale} compact /></section>
        <section className="agent-evaluation-records">
          <header><h2>{t("evaluationEvidence")} <small>v{selected.version}</small></h2><a className="inline-link" href="#/agent" onClick={async event => { event.preventDefault(); try { await onUse(selected); } catch (e) { setError(e.message); } }}>{t("useForEvaluation")}<ArrowUpRight size={15} /></a></header>
          {!evaluations && !error && <p role="status">{t("loading")}</p>}{evaluations?.map((r) => <button className="evaluation-row" key={r.id} onClick={async () => { try { await onEvaluation(await rankApi.getRun(r.id)); } catch (e) { setError(e.message); } }}><span>{r.datasetName} · v{r.datasetVersion}</span><span>{r.total} {t("cases")}</span><span>{r.passed}/{r.total}</span><span>{cost(r, t)}</span><StatePill value={r.status} t={t} /></button>)}{evaluations?.length === 0 && <p>{t("noEvaluation")}</p>}
        </section>
      </section>
    </>}
  </main>;
}
export { ResponseCenter } from "./ResponseCenter.jsx";

/** API keys are shown once; list and revoke use only redacted persisted records. */
export function CredentialCenter({ tenantId, deployments, t, locale, embedded = false }) {
  const [items,setItems]=useState(null); const [error,setError]=useState(""); const [busy,setBusy]=useState(false);
  const requestedDeployment = new URLSearchParams(location.hash.split("?")[1]).get("deploymentId");
  const initialDeployment = deployments.find(item => item.id === requestedDeployment);
  const [creating,setCreating]=useState(Boolean(initialDeployment));
  const [secret,setSecret]=useState(""); const [revoke,setRevoke]=useState(null); const [copied,setCopied]=useState(false);
  async function refresh(){setItems(await rankApi.listCredentials(tenantId));}
  useEffect(() => { let stale=false;rankApi.listCredentials(tenantId).then((v)=>{if(!stale)setItems(v);}).catch((e)=>{if(!stale)setError(e.message);});return()=>{stale=true;}; },[tenantId]);
  async function confirmRevoke(){setBusy(true);setError("");try{await rankApi.revokeCredential(revoke.id,tenantId);setRevoke(null);await refresh();}catch(e){setError(e.message);}finally{setBusy(false);}}
  return <main className="management-page">{(!embedded || !creating) && <PageHeading title={embedded ? undefined : t("credentials")}>{!creating && <button className="primary-action" disabled={!deployments.length} onClick={()=>{setCreating(true);setSecret("");}}><Plus size={17}/>{t("createKey")}</button>}</PageHeading>}<ErrorNotice error={error}/>
    {secret && <section className="secret-reveal"><h2>{t("keyOnce")}</h2><code>{secret}</code><div className="management-actions"><button onClick={async()=>{try{await navigator.clipboard.writeText(secret);setCopied(true);}catch(e){setError(e.message);}}}>{t(copied?"copied":"copyKey")}</button><button onClick={()=>setSecret("")}>{t("done")}</button></div></section>}
    {creating && <CredentialForm tenantId={tenantId} deployments={deployments} t={t} onCancel={() => setCreating(false)} onCreated={async value => { setSecret(value.apiKey); setCopied(false); setCreating(false); await refresh(); }} />}
    {revoke && <InlineConfirmation title={`${t("revokeKey")} · ${revoke.name}`} hint={t("revokeHint")} confirmLabel={t("confirmRevoke")} busy={busy} onCancel={() => setRevoke(null)} onConfirm={confirmRevoke} t={t} />}
    {!items && !error && <p role="status">{t("loading")}</p>}{!creating && items?.length===0 && <EmptyState title={t("noCredentials")} hint={!deployments.length?t("emptyDeploymentsHint"):t("credentialsHint")}/>}
    {!creating && items?.map((v)=><section className="credential-row" key={v.id}><div className="detail-title"><h2>{v.name}</h2><StatePill value={v.revokedAt?"revoked":"active"} t={t}/></div><code>{v.keyPrefix}••••••••</code><p>{v.deploymentIds.map((id)=>deployments.find((d)=>d.id===id)?.name||id).join(" · ")}</p><div className="credential-meta"><span>{v.scopes.map((s)=>t(s)).join(" · ")}</span><time>{formatDateTime(locale,v.createdAt)}</time>{!v.revokedAt && <button onClick={()=>setRevoke(v)}>{t("revokeKey")}</button>}</div></section>)}
  </main>;
}
