import { SelectField } from "./SelectField.jsx";
import { useEffect, useState } from "react";
import { Modal } from "./Modal.jsx";
import {
  ArrowRight,
  ArrowLeft,
  BellRinging,
  CircleNotch,
  Clock,
  Copy,
  Gauge,
  GitBranch,
  Plus,
  ShieldCheck,
  Users,
  WarningCircle,
  X,
} from "@phosphor-icons/react";
import { rankApi } from "./api.js";
import { formatDateTime } from "./i18n.js";
import { ResponseCenter } from "./ManagementPages.jsx";
import { ErrorNotice, StatePill, PageHeading } from "./ManagementUI.jsx";
import { DeploymentForm, ReleaseHistory, versionLabel } from "./DeploymentDetails.jsx";
import { ReleasePolicyEditor } from "./ReleasePolicy.jsx";
import { ReleaseWorkflow, ReleaseTaskHistory } from "./ReleaseWorkflow.jsx";
import { EndpointIntegration } from "./EndpointIntegration.jsx";

function publishedRoute(deployment) {
  return ["production", "staging", "latest"].map(name => deployment.aliases.find(item => item.name === name)).find(item => item?.currentRelease);
}

function ReleaseDialog({ deployment, agents, datasets, initialVersionId, locale, t, onClose, onReleased }) {
  const versions = agents.filter(item => item.familyId === deployment.agentFamilyId).flatMap(item => item.versions || [item]).sort((a, b) => b.version - a.version);
  const [agentVersionId, setAgentVersionId] = useState(initialVersionId || versions[0]?.id || "");
  return <Modal label={t("releaseVersion")} className="deployment-modal-backdrop" onClose={onClose}>
    <section className="release-dialog" aria-label={t("releaseVersion")} onMouseDown={event => event.stopPropagation()}>
      <header><h2>{t("releaseVersion")}</h2><button type="button" onClick={onClose} aria-label={t("close")}><X size={20} /></button></header>
      <div className="release-dialog-body">
        {initialVersionId ? <div className="release-agent-target"><strong>{versionLabel(agents, initialVersionId)}</strong></div> : <div className="form-grid">
          <label>{t("targetVersion")}<SelectField aria-label={t("targetVersion")} value={agentVersionId} onValueChange={setAgentVersionId} options={versions.map(v => ({ value: v.id, label: versionLabel(agents, v.id) }))} /></label>
        </div>}
        <ReleaseWorkflow key={agentVersionId} deployment={deployment} alias="production" agentVersionId={agentVersionId} datasets={datasets} agents={agents} locale={locale} t={t} onReleased={onReleased} />
      </div>
    </section>
  </Modal>;
}

const webhookEvents = ["response.completed", "response.failed", "response.cancelled", "response.requires_action"];

function WebhookDialog({ deployment, t, onClose, onCreated }) {
  const [name, setName] = useState("");
  const [url, setURL] = useState("");
  const [events, setEvents] = useState(["response.completed", "response.failed", "response.cancelled"]);
  const [created, setCreated] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(event) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      const endpoint = await rankApi.createWebhookEndpoint({
        tenantId: deployment.tenantId,
        name,
        url,
        events,
        deploymentIds: [deployment.id],
      });
      setCreated(endpoint);
      await onCreated();
    } catch (createError) {
      setError(createError.message);
    } finally {
      setBusy(false);
    }
  }

  function toggleEvent(value) {
    setEvents((current) => current.includes(value) ? current.filter((item) => item !== value) : [...current, value]);
  }

  return (
    <Modal label={t("addWebhook")} className="deployment-modal-backdrop" onClose={onClose}>
      <form className="release-dialog webhook-dialog" aria-label={t("addWebhook")} onSubmit={submit} onMouseDown={(event) => event.stopPropagation()}>
        <header><div><small>STANDARD WEBHOOKS</small><h2>{created ? t("webhookReady") : t("addWebhook")}</h2></div><button type="button" onClick={onClose} aria-label={t("close")}><X size={20} /></button></header>
        {created ? <div className="webhook-secret-panel">
          <ShieldCheck size={24} weight="duotone" />
          <strong>{t("saveSigningSecret")}</strong>
          <p>{t("signingSecretOnce")}</p>
          <div><code>{created.signingSecret}</code><button type="button" onClick={() => navigator.clipboard?.writeText(created.signingSecret)} aria-label={t("copySigningSecret")}><Copy size={16} /></button></div>
        </div> : <div className="release-dialog-body">
          <label><span>{t("webhookName")}</span><input required maxLength="80" value={name} onChange={(event) => setName(event.target.value)} placeholder={t("webhookNamePlaceholder")} /></label>
          <label><span>{t("endpointUrl")}</span><input required type="url" value={url} onChange={(event) => setURL(event.target.value)} placeholder="https://example.com/webhooks/rank" /></label>
          <fieldset className="webhook-event-options"><legend>{t("eventTypes")}</legend>{webhookEvents.map((value) => <label key={value}><input type="checkbox" checked={events.includes(value)} onChange={() => toggleEvent(value)} /><span>{value}</span></label>)}</fieldset>
          <p className="webhook-security-note"><ShieldCheck size={15} />{t("webhookSecurityNote")}</p>
          {error && <p className="release-error"><WarningCircle size={15} />{error}</p>}
        </div>}
        <footer>{created ? <button className="primary-action" type="button" onClick={onClose}>{t("done")}</button> : <><button type="button" onClick={onClose}>{t("cancel")}</button><button className="primary-action" type="submit" disabled={busy || !name.trim() || !url.trim() || events.length === 0}>{busy ? <CircleNotch className="spin" size={17} /> : <Plus size={17} />}{t("createEndpoint")}</button></>}</footer>
      </form>
    </Modal>
  );
}

function WebhookHistory({ endpoint, t }) {
  const [items,setItems]=useState(null);const [error,setError]=useState("");const [busy,setBusy]=useState("");const [refresh,setRefresh]=useState(0);
  useEffect(()=>{let stale=false;setError("");rankApi.listWebhookDeliveries(endpoint.id).then((v)=>{if(!stale)setItems(v);}).catch((e)=>{if(!stale)setError(e.message);});return()=>{stale=true;};},[endpoint.id,refresh]);
  async function retry(id){setBusy(id);setError("");try{await rankApi.retryWebhookDelivery(endpoint.id,id);setRefresh((v)=>v+1);}catch(e){setError(e.message);}finally{setBusy("");}}
  return <section className="webhook-delivery-history"><div className="detail-title"><h3>{endpoint.name} · {t("deliveryHistory")}</h3><button onClick={()=>setRefresh((v)=>v+1)}>{t("refresh")}</button></div><ErrorNotice error={error}/>{!items&&!error&&<p>{t("loading")}</p>}{items?.length===0&&<p>{t("noDeliveries")}</p>}{items?.map((v)=><div className="delivery-row" key={v.id}><strong>{v.eventType}</strong><StatePill value={v.status} t={t}/><small>{v.responseId}</small><span>{t("attempts")}: {v.attempts}</span>{v.lastError&&<p>{v.lastError}</p>}{v.status==="failed"&&<button disabled={!endpoint.enabled||Boolean(busy)} onClick={()=>retry(v.id)}>{t("retryDelivery")}</button>}</div>)}</section>;
}

function WebhookAccess({ deployment, endpoints, busyId, t, onCreate, onToggle }) {
  const [selected,setSelected]=useState("");
  const scoped = endpoints.filter((endpoint) => endpoint.deploymentIds.includes(deployment.id));
  return <section className="webhook-access">
    <header><div><span className="section-icon"><BellRinging size={18} /></span><span><h2>{t("webhooks")}</h2><p>{t("webhookHint")}</p></span></div><button className="secondary-action" type="button" onClick={onCreate}><Plus size={15} />{t("addEndpoint")}</button></header>
    {scoped.length ? <div className="webhook-list">{scoped.map((endpoint) => <article key={endpoint.id}>
      <div className="webhook-state"><i className={endpoint.enabled ? "enabled" : ""} /><span><strong>{endpoint.name}</strong><code>{endpoint.url}</code></span></div>
      <div className="webhook-events">{endpoint.events.map((event) => <span key={event}>{event.replace("response.", "")}</span>)}</div>
      <button type="button" disabled={busyId === endpoint.id} onClick={() => onToggle(endpoint)}>{busyId === endpoint.id ? <CircleNotch className="spin" size={14} /> : endpoint.enabled ? t("disable") : t("enable")}</button>
      <button type="button" onClick={()=>setSelected((id)=>id===endpoint.id?"":endpoint.id)}>{t("deliveryHistory")}</button>
    </article>)}</div> : <div className="webhook-empty"><BellRinging size={24} weight="duotone" /><strong>{t("noWebhooks")}</strong><p>{t("noWebhooksHint")}</p></div>}
    {scoped.find((e)=>e.id===selected) && <WebhookHistory key={selected} endpoint={scoped.find((e)=>e.id===selected)} t={t}/>}
  </section>;
}

/** Shared webhook configuration keeps signing secrets in browser-owned forms. */
export function WebhookSettings({ deployment, t }) {
  const [endpoints, setEndpoints] = useState([]);
  const [revision, setRevision] = useState(0);
  const [creating, setCreating] = useState(false);
  const [busyId, setBusyId] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    let stale = false;
    rankApi.listWebhookEndpoints(deployment.tenantId).then(value => {
      if (!stale) setEndpoints(value);
    }).catch(error => { if (!stale) setError(error.message); });
    return () => { stale = true; };
  }, [deployment.tenantId, revision]);
  async function toggle(endpoint) {
    setBusyId(endpoint.id); setError("");
    try { await rankApi.setWebhookEndpointEnabled(endpoint.id, !endpoint.enabled); setRevision(value => value + 1); }
    catch (error) { setError(error.message); }
    finally { setBusyId(""); }
  }
  return <><ErrorNotice error={error}/><WebhookAccess deployment={deployment} endpoints={endpoints} busyId={busyId} t={t} onCreate={() => setCreating(true)} onToggle={toggle}/>{creating && <WebhookDialog deployment={deployment} t={t} onClose={() => setCreating(false)} onCreated={() => setRevision(value => value + 1)}/>}</>;
}

export function DeploymentCenter({ deployments, agents, datasets = [], tenantId, locale, t, onRefresh }) {
  const [intent, setIntent] = useState(location.hash);
  const [selectedId, setSelectedId] = useState("");
  const [resolvedDeployment, setResolvedDeployment] = useState(null);
  const [query, setQuery] = useState("");
  const [resolving, setResolving] = useState(true);
  const [initialVersionId, setInitialVersionId] = useState("");
  const [tab, setTab] = useState("overview");
  const [releaseOpen, setReleaseOpen] = useState(false);
  const [integrationOpen, setIntegrationOpen] = useState(false);
  const [error,setError] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  useEffect(() => {
    const update = () => setIntent(location.hash);
    window.addEventListener("hashchange", update);
    return () => window.removeEventListener("hashchange", update);
  }, []);
  useEffect(() => {
    let stale = false;
    const params = new URLSearchParams(intent.split("?")[1]);
    const version = params.get("publish") || "";
    const reference = params.get("deployment")?.split("@")[0];
    setError(""); setReleaseOpen(false); setIntegrationOpen(false); setTab("overview"); setInitialVersionId(version); setResolvedDeployment(null);
    if (version) {
      setResolving(true); setCreateOpen(false); setSelectedId("");
      rankApi.agentDeployments(version).then(items => {
        if (stale) return;
        setResolvedDeployment(items[0] || null);
        setSelectedId(items[0]?.id || ""); setCreateOpen(!items.length); setResolving(false);
        if (items.length) {
          if (!items[0].aliases.some(alias => alias.currentRelease?.agentVersionId === version)) setTab("releases");
          window.history.replaceState(null, "", `#/deployments?deploymentId=${encodeURIComponent(items[0].id)}`);
        }
      }).catch(e => { if (!stale) { setError(e.message); setResolving(false); } });
    } else {
      setSelectedId(params.get("deploymentId") || deployments.find(item => item.name === reference)?.id || "");
      setCreateOpen(params.has("create")); setResolving(false);
    }
    return () => { stale = true; };
  }, [intent]);
  const deployment = deployments.find((item) => item.id === selectedId) || resolvedDeployment;
  const initialAgent = agents.flatMap(item => item.versions || [item]).find(item => item.id === initialVersionId);
  function openDeployment(id) {
    setSelectedId(id); setTab("overview"); setCreateOpen(false);
    window.history.replaceState(null, "", `#/deployments?deploymentId=${encodeURIComponent(id)}`);
  }

  if (resolving) return <main className="management-page"><p role="status">{t("loading")}</p></main>;
  if (createOpen && initialVersionId && !initialAgent) return <main className="management-page"><a className="inline-link" href="#/agents"><ArrowLeft size={15}/>{t("agentLibrary")}</a><ErrorNotice error={t("noMatch")} /></main>;
  if (createOpen) {
    const slug = initialAgent ? initialAgent.handle.replace(/^@/, "").replace(/[^a-z0-9-]+/gi, "-").toLowerCase().replace(/^-+|-+$/g, "").slice(0, 45) || "agent" : "";
    const baseName = slug && !/^[a-z]/.test(slug) ? `agent-${slug}` : slug;
    let name = baseName;
    for (let suffix = 2; name && deployments.some(item => item.name === name); suffix++) name = `${baseName}-${suffix}`;
    return <main className="management-page"><DeploymentForm tenantId={tenantId} agents={agents} initialAgent={initialAgent} initialName={name} t={t} onCancel={() => { location.hash = initialAgent ? `/agents?agent=${encodeURIComponent(initialAgent.id)}` : "/deployments"; setCreateOpen(false); }} onCreated={async (v) => { await onRefresh(); openDeployment(v.id); setReleaseOpen(true); }} /></main>;
  }
  if (!deployment) {
    const visible = deployments.filter(item => `${item.name} ${agents.find(agent => agent.familyId === item.agentFamilyId)?.name || ""}`.toLowerCase().includes(query.toLowerCase()));
    return <main className="management-page deployment-list"><PageHeading title={t("deployments")}><a className="primary-action" href="#/deployments?create=1"><Plus size={17} />{t("createDeployment")}</a></PageHeading><ErrorNotice error={error} />
      {!!deployments.length && <input className="management-search" aria-label={t("findDeployments")} placeholder={t("findDeployments")} value={query} onChange={event => setQuery(event.target.value)} />}
      <div className="deployment-list-head"><span>{t("name")}</span><span>{t("deployedAgent")}</span><span>{t("agentVersion")}</span><span>{t("status")}</span><span>{t("publishedAt")}</span></div>
      {visible.map(item => {
        const current = publishedRoute(item)?.currentRelease;
        const agent = agents.find(entry => entry.familyId === item.agentFamilyId);
        const version = agent?.versions?.find(entry => entry.id === current?.agentVersionId) || (agent?.id === current?.agentVersionId ? agent : null);
        return <a className="deployment-list-row" key={item.id} href={`#/deployments?deploymentId=${encodeURIComponent(item.id)}`}><strong>{item.name}</strong><span>{agent?.name || "—"}</span><span>{version ? `v${version.version}` : "—"}</span><span>{t(current ? "published" : "unpublished")}</span><time>{formatDateTime(locale, current?.createdAt)}</time><ArrowRight size={16}/></a>;
      })}
      {!visible.length && <p className="empty-list">{t(deployments.length ? "noMatch" : "emptyDeployments")}</p>}
    </main>;
  }

  const currentRoute = publishedRoute(deployment);
  const current = currentRoute?.currentRelease;
  return (
    <main className="deployment-page">
      <header className="deployment-heading">
        <div className="deployment-title">
          <a className="inline-link deployment-breadcrumb" href="#/deployments"><ArrowLeft size={15}/>{t("backToDeployments")}</a>
          <div><h1>{deployment.name}</h1><span className={`deployment-state ${current ? "published" : ""}`}>{t(current ? "published" : "unpublished")}</span></div>
        </div>
        <div className="management-actions">{current && <button className="secondary-action" onClick={() => setIntegrationOpen(true)}><Copy size={16}/>{t("integration")}</button>}<button className="primary-action" type="button" onClick={() => { setInitialVersionId(""); setReleaseOpen(true); }}><GitBranch size={17} />{t("releaseVersion")}</button></div>
      </header>
      <nav className="deployment-tabs" aria-label={t("deployments")}>{["overview", "releases", "traffic", "webhooks", "releasePolicy"].map((key) => <button key={key} type="button" className={tab === key ? "active" : ""} onClick={() => setTab(key)}>{t(key)}</button>)}</nav>
      <ErrorNotice error={error}/>

      {tab === "overview" ? <>
        <section className="deployment-current">
          <h2>{t("currentPublishedVersion")}</h2>
          {current ? <div><div><a className="inline-link" href={`#/agents?agent=${encodeURIComponent(current.agentVersionId)}`}>{versionLabel(agents, current.agentVersionId)}</a><p>{t("publishedAt")} · {formatDateTime(locale, current.createdAt)}</p></div></div> : <p>{t("publishBeforeIntegration")}</p>}
        </section>
        <section className="policy-section">
          <h2>{t("deploymentPolicy")}</h2>
          <div><span><Gauge size={17} /><small>{t("requestLimit")}</small><strong>{deployment.policy.requestsPerMinute} RPM</strong></span><span><Users size={17} /><small>{t("concurrencyLimit")}</small><strong>{deployment.policy.maxConcurrentResponses}</strong></span><span><Clock size={17} /><small>{t("timeout")}</small><strong>{Math.round(deployment.policy.executionTimeoutMs / 1000)}s</strong></span></div>
          <p className="form-hint">{t("fixedDeploymentPolicy")}</p>
        </section>
      </> : tab === "webhooks" ? <WebhookSettings key={deployment.id} deployment={deployment} t={t} /> : tab === "traffic" ? <ResponseCenter key={deployment.id} tenantId={deployment.tenantId} deployments={[deployment]} agents={agents} locale={locale} t={t} scopedDeploymentId={deployment.id} /> : tab === "releases" ? <><ReleaseTaskHistory key={`tasks-${deployment.id}`} deployment={deployment} datasets={datasets} agents={agents} locale={locale} t={t} onReleased={onRefresh} /><ReleaseHistory key={`history-${deployment.id}`} deployment={deployment} agents={agents} locale={locale} t={t} alias={currentRoute?.name || "production"} /></> : <ReleasePolicyEditor key={deployment.id} deployment={deployment} datasets={datasets} t={t} />}
      {integrationOpen && <EndpointIntegration deployment={deployment} deployments={deployments} locale={locale} t={t} onClose={() => setIntegrationOpen(false)} />}
      {releaseOpen && <ReleaseDialog deployment={deployment} datasets={datasets} agents={agents} initialVersionId={initialVersionId} locale={locale} t={t} onClose={() => setReleaseOpen(false)} onReleased={onRefresh} />}
    </main>
  );
}
