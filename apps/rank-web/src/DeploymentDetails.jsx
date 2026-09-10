import { SelectField } from "./SelectField.jsx";
import { useEffect, useState } from "react";
import { ArrowRight } from "@phosphor-icons/react";
import { rankApi } from "./api.js";
import { formatDateTime } from "./i18n.js";
import { ErrorNotice, StatePill, Pagination } from "./ManagementUI.jsx";

export function versionLabel(agents, id) {
  const version = agents.flatMap((item) => item.versions || [item]).find((item) => item.id === id);
  return version ? `${version.name || version.handle} v${version.version}` : id || "—";
}

/** Creates a separate serving target; it never changes an existing target's policy. */
export function DeploymentForm({ tenantId, agents, t, onCancel, onCreated }) {
  const [name, setName] = useState("");
  const [family, setFamily] = useState(agents[0]?.familyId || "");
  const [policy, setPolicy] = useState({ requestsPerMinute: 60, maxConcurrentResponses: 4, executionTimeoutMs: 120000, allowInstructions: false, allowCallerTools: false });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function submit(event) {
    event.preventDefault(); setBusy(true); setError("");
    try {
      const created = await rankApi.createDeployment({ tenantId, name, agentFamilyId: family, policy });
      await onCreated(created);
    } catch (e) { setError(e.message); } finally { setBusy(false); }
  }
  return <form className="management-form deployment-create-form" onSubmit={submit}>
    <h2>{t("createDeployment")}</h2><ErrorNotice error={error} />
    <div className="form-grid">
      <label>{t("name")}<input required autoFocus maxLength={63} pattern={"[a-z][a-z0-9\\-]*"} placeholder="customer-support" value={name} onChange={(e) => setName(e.target.value)} /><small>{t("deploymentNameHint")}</small></label>
      <label>{t("agentLibrary")}<SelectField required value={family} onValueChange={setFamily} aria-label={t("agentLibrary")} options={[{ value: "", label: t("selectAgent"), disabled: true }, ...agents.map(a => ({ value: a.familyId, label: a.name }))]} /></label>
      {[["requestsPerMinute", "requestLimit", 1], ["maxConcurrentResponses", "concurrencyLimit", 1], ["executionTimeoutMs", "timeoutSeconds", 1000]].map(([key, label, scale]) => <label key={key}>{t(label)}<input type="number" required min={1} step={1} value={policy[key] / scale} onChange={(e) => setPolicy((v) => ({ ...v, [key]: Number(e.target.value) * scale }))} /></label>)}
    </div>
    {[["allowInstructions", "callerInstructions"], ["allowCallerTools", "callerTools"]].map(([key, label]) => <label className="check-label" key={key}><input type="checkbox" checked={policy[key]} onChange={(e) => setPolicy((v) => ({ ...v, [key]: e.target.checked }))} />{t(label)}</label>)}
    <p className="form-hint">{t("deploymentCreateHint")}</p>
    <div className="management-actions"><button type="button" disabled={busy} onClick={onCancel}>{t("cancel")}</button><button className="primary-action" disabled={busy || !family}>{t(busy ? "saving" : "createDeployment")}</button></div>
  </form>;
}

export function DeploymentPolicy({ deployment, t }) {
  const { policy } = deployment;
  return <section className="management-section deployment-policy-details"><h2>{t("deploymentPolicy")}</h2>
    <p>{t("fixedDeploymentPolicy")}</p>
    <dl className="detail-facts">{[["requestLimit", `${policy.requestsPerMinute} RPM`], ["concurrencyLimit", policy.maxConcurrentResponses], ["timeoutSeconds", policy.executionTimeoutMs / 1000], ["callerInstructions", t(policy.allowInstructions ? "on" : "off")], ["callerTools", t(policy.allowCallerTools ? "on" : "off")], ["authentication", "Bearer API Key"], ["policyDigest", deployment.policyDigest]].map(([label, value]) => <div key={label}><dt>{t(label)}</dt><dd>{value}</dd></div>)}</dl>
  </section>;
}

/** Alias-local revision pagination retains the exact before/after Agent versions. */
export function ReleaseHistory({ deployment, agents, locale, t }) {
  const [alias, setAlias] = useState("production");
  const [cursors, setCursors] = useState([""]);
  const [page, setPage] = useState(null);
  const [error, setError] = useState("");
  const cursor = cursors.at(-1);
  useEffect(() => {
    let stale = false; setPage(null); setError("");
    rankApi.listReleaseHistory(deployment.id, alias, cursor).then((v) => { if (!stale) setPage(v); }).catch((e) => { if (!stale) setError(e.message); });
    return () => { stale = true; };
  }, [deployment, alias, cursor]);
  return <section className="release-history management-page">
    <div className="management-filters"><label>{t("route")}<SelectField value={alias} onValueChange={nextValue => { setAlias(nextValue); setCursors([""]); }} aria-label={t("route")} options={[...["production", "staging", "latest"].map(v => ({ value: v, label: v }))]} /></label></div>
    <ErrorNotice error={error} />{!page && !error && <p role="status">{t("loading")}</p>}{page?.items.length === 0 && <p>{t("noReleases")}</p>}
    {page?.items.map((v) => <article className="release-history-row" key={v.id}>
      <header><strong>{alias} · r{v.revision}</strong><time>{formatDateTime(locale, v.createdAt)}</time><StatePill value={v.policyResult} t={t} /></header>
      <div className="release-transition"><span>{versionLabel(agents, v.previousAgentVersionId)}</span><ArrowRight size={18} /><strong>{versionLabel(agents, v.agentVersionId)}</strong></div>
      <p>{v.reason === "agent_version_published" ? t("agentVersionPublished") : v.reason}</p><small>{t("operator")}: {v.actor}</small>
      {v.evidenceRunId && <p>{t("evaluationEvidence")}: {v.evidenceRunId}</p>}
      <details><summary>{t("releaseRevision")}</summary><code>{v.id}</code></details>
    </article>)}
    <Pagination pageNumber={cursors.length} canPrevious={cursors.length > 1} canNext={Boolean(page?.nextCursor)} onPrevious={() => setCursors((v) => v.slice(0, -1))} onNext={() => setCursors((v) => [...v, page.nextCursor])} t={t} />
  </section>;
}
