import { SelectField } from "./SelectField.jsx";
import { useEffect, useMemo, useState } from "react";
import { Modal } from "./Modal.jsx";
import {
  ArrowRight,
  BellRinging,
  CheckCircle,
  CircleNotch,
  Clock,
  Copy,
  Cube,
  Gauge,
  GitBranch,
  Plus,
  ShieldCheck,
  Users,
  WarningCircle,
  Wrench,
  X,
} from "@phosphor-icons/react";
import { rankApi } from "./api.js";
import { formatDateTime } from "./i18n.js";
import { ResponseCenter } from "./ManagementPages.jsx";
import { ErrorNotice, StatePill } from "./ManagementUI.jsx";
import { DeploymentForm, DeploymentPolicy, ReleaseHistory, versionLabel } from "./DeploymentDetails.jsx";

const aliasOrder = ["latest", "staging", "production"];

function aliasStatus(alias, t) {
  if (!alias?.currentRelease) return ["empty", t("unconfigured")];
  return alias.currentRelease.policyResult === "passed" ? ["evaluated", t("evaluated")] : ["healthy", t("published")];
}

function ReleaseDialog({ deployment, agents, locale, t, onClose, onReleased }) {
  const versions = useMemo(() => agents
    .filter((item) => item.familyId === deployment.agentFamilyId)
    .flatMap((item) => item.versions || [item]), [agents, deployment.agentFamilyId]);
  const latest = deployment.aliases.find((item) => item.name === "latest")?.currentRelease?.agentVersionId;
  const [alias, setAlias] = useState("staging");
  const [agentVersionId, setAgentVersionId] = useState(latest || versions[0]?.id || "");
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const target = deployment.aliases.find((item) => item.name === alias);

  async function submit(event) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await rankApi.promoteDeployment(deployment.id, alias, {
        agentVersionId,
        expectedReleaseRevisionId: target?.currentReleaseRevisionId || "",
        policyResult: "manual_override",
        actor: "local-operator",
        reason,
      });
      await onReleased();
      onClose();
    } catch (releaseError) {
      setError(releaseError.status === 409 ? t("releaseConflict") : releaseError.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal label={t("releaseVersion")} className="deployment-modal-backdrop" onClose={onClose}>
      <form className="release-dialog" aria-label={t("releaseVersion")} onSubmit={submit} onMouseDown={(event) => event.stopPropagation()}>
        <header><div><small>{t("manualOverride")}</small><h2>{t("releaseTo", { alias })}</h2></div><button type="button" onClick={onClose} aria-label={t("cancel")}><X size={20} /></button></header>
        <div className="release-dialog-body">
          <label><span>{t("route")}</span><SelectField value={alias} onValueChange={setAlias} aria-label={t("route")} options={[{ value: "staging", label: "staging" }, { value: "production", label: "production" }]} /></label>
          <label><span>{t("targetVersion")}</span><SelectField value={agentVersionId} onValueChange={setAgentVersionId} aria-label={t("targetVersion")} options={[...versions.map(version => ({ value: version.id, label: versionLabel(agents, version.id) }))]} /></label>
          <div className="release-transition"><span>{versionLabel(agents, target?.currentRelease?.agentVersionId)}</span><ArrowRight size={20} /><strong>{versionLabel(agents, agentVersionId)}</strong></div>
          <label><span>{t("releaseReason")}</span><textarea rows="3" required value={reason} onChange={(event) => setReason(event.target.value)} placeholder={t("releaseReasonPlaceholder")} /></label>
          <small className="release-cas"><GitBranch size={14} />{target?.currentReleaseRevisionId || "—"}</small>
          {error && <p className="release-error"><WarningCircle size={15} />{error}</p>}
        </div>
        <footer><button type="button" onClick={onClose}>{t("cancel")}</button><button className="primary-action" type="submit" disabled={busy || !agentVersionId || !reason.trim()}>{busy ? <CircleNotch className="spin" size={17} /> : <CheckCircle size={17} />}{t("confirmRelease")}</button></footer>
      </form>
    </Modal>
  );
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

export function DeploymentCenter({ deployments, agents, tenantId, locale, t, onRefresh }) {
  const [selectedId, setSelectedId] = useState(deployments[0]?.id || "");
  const [tab, setTab] = useState("overview");
  const [releaseOpen, setReleaseOpen] = useState(false);
  const [webhookOpen, setWebhookOpen] = useState(false);
  const [webhooks, setWebhooks] = useState([]);
  const [webhookBusyId, setWebhookBusyId] = useState("");
  const [error,setError] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  useEffect(() => {
    if (!deployments.some((item) => item.id === selectedId)) setSelectedId(deployments[0]?.id || "");
  }, [deployments, selectedId]);
  const deployment = deployments.find((item) => item.id === selectedId);

  async function refreshWebhooks() {
    if (!deployment) return;
    setWebhooks(await rankApi.listWebhookEndpoints(deployment.tenantId));
  }

  useEffect(() => {
    if (tab === "access" && deployment) refreshWebhooks().catch((e) => setError(e.message));
  }, [tab, deployment?.id]);

  async function toggleWebhook(endpoint) {
    setWebhookBusyId(endpoint.id);
    try {
      setError("");
      await rankApi.setWebhookEndpointEnabled(endpoint.id, !endpoint.enabled);
      await refreshWebhooks();
    } catch (e) {
      setError(e.message);
    } finally {
      setWebhookBusyId("");
    }
  }

  if (createOpen) {
    return <main className="management-page"><DeploymentForm tenantId={tenantId} agents={agents} t={t} onCancel={() => setCreateOpen(false)} onCreated={async (v) => { await onRefresh(); setSelectedId(v.id); setTab("overview"); setCreateOpen(false); }} /></main>;
  }
  if (!deployment) {
    return <main className="deployment-empty"><Cube size={30} /><h1>{t("emptyDeployments")}</h1><p>{t("emptyDeploymentsHint")}</p><button className="primary-action" type="button" onClick={() => setCreateOpen(true)}><Plus size={17} />{t("createDeployment")}</button></main>;
  }

  const aliases = aliasOrder.map((name) => deployment.aliases.find((item) => item.name === name) || { name });
  const publicAlias = aliases.find((item) => item.name === "production" && item.currentRelease) || aliases.find((item) => item.name === "latest" && item.currentRelease);
  const publicReference = publicAlias ? `${deployment.name}@${publicAlias.name}` : "";
  return (
    <main className="deployment-page">
      <header className="deployment-heading">
        <div className="deployment-title">
          {deployments.length > 1 && <SelectField className="deployment-switcher" icon={Cube} aria-label={t("deployments")}
            value={selectedId} onValueChange={setSelectedId} options={deployments.map(item => ({ value: item.id, label: item.name }))} />}
          <span className="deployment-breadcrumb">{t("deployments")} <i>/</i> {deployment.name}</span>
          <div><h1>{deployment.name}</h1><span className="health-badge"><i />{t("configured")}</span></div>
          <p><span>{t("currentReference")}</span> {publicReference || t("unconfigured")} {publicReference && <button type="button" aria-label={t("copyReference")} onClick={()=>navigator.clipboard.writeText(publicReference).catch((e)=>setError(e.message))}><Copy size={14} /></button>}</p>
        </div>
        <div className="management-actions"><button className="secondary-action" onClick={() => setCreateOpen(true)}><Plus size={17} />{t("createDeployment")}</button><button className="primary-action" type="button" onClick={() => setReleaseOpen(true)}><GitBranch size={17} />{t("releaseVersion")}</button></div>
      </header>
      <nav className="deployment-tabs" aria-label={t("deployments")}>{["overview", "releases", "traffic", "access", "settings"].map((key) => <button key={key} type="button" className={tab === key ? "active" : ""} onClick={() => setTab(key)}>{t(key)}</button>)}</nav>
      <ErrorNotice error={error}/>

      {tab === "overview" ? <>
        <section className="route-section">
          <h2>{t("currentRoutes")}</h2>
          <div className="route-table" role="table">
            <div className="route-row route-head" role="row"><span>{t("route")}</span><span>{t("agentVersion")}</span><span>{t("releaseRevision")}</span><span>{t("publishedAt")}</span><span>{t("status")}</span></div>
            {aliases.map((alias) => {
              const [statusClass, statusLabel] = aliasStatus(alias, t);
              return <div className="route-row" role="row" key={alias.name}>
                <strong>{t(alias.name)}</strong>
                <span>{versionLabel(agents, alias.currentRelease?.agentVersionId)}</span>
                <code>{alias.currentRelease?.id || "—"}</code>
                <time>{formatDateTime(locale, alias.currentRelease?.createdAt)}</time>
                <span className={`route-status ${statusClass}`}><i />{statusLabel}</span>
              </div>;
            })}
          </div>
        </section>
        <button className="secondary-action" onClick={() => setTab("traffic")}><Gauge size={17} />{t("viewRequests")}</button>
        <section className="policy-section">
          <h2>{t("deploymentPolicy")}</h2>
          <div><span><Gauge size={17} /><small>{t("requestLimit")}</small><strong>{deployment.policy.requestsPerMinute} RPM</strong></span><span><Users size={17} /><small>{t("concurrencyLimit")}</small><strong>{deployment.policy.maxConcurrentResponses}</strong></span><span><Clock size={17} /><small>{t("timeout")}</small><strong>{Math.round(deployment.policy.executionTimeoutMs / 1000)}s</strong></span><span><GitBranch size={17} /><small>{t("callerInstructions")}</small><strong>{deployment.policy.allowInstructions ? "On" : t("off")}</strong></span><span><Wrench size={17} /><small>{t("callerTools")}</small><strong>{deployment.policy.allowCallerTools ? "On" : t("off")}</strong></span></div>
        </section>
      </> : tab === "access" ? <WebhookAccess key={deployment.id} deployment={deployment} endpoints={webhooks} busyId={webhookBusyId} t={t} onCreate={() => setWebhookOpen(true)} onToggle={toggleWebhook} /> : tab === "traffic" ? <ResponseCenter key={deployment.id} tenantId={deployment.tenantId} deployments={[deployment]} agents={agents} locale={locale} t={t} scopedDeploymentId={deployment.id} /> : tab === "releases" ? <ReleaseHistory key={deployment.id} deployment={deployment} agents={agents} locale={locale} t={t} /> : <DeploymentPolicy deployment={deployment} t={t} />}
      {releaseOpen && <ReleaseDialog deployment={deployment} agents={agents} locale={locale} t={t} onClose={() => setReleaseOpen(false)} onReleased={onRefresh} />}
      {webhookOpen && <WebhookDialog deployment={deployment} t={t} onClose={() => setWebhookOpen(false)} onCreated={refreshWebhooks} />}
    </main>
  );
}
