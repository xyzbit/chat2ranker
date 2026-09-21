import { useEffect, useState } from "react";
import { BookOpen, Check, Copy, Plus, X } from "@phosphor-icons/react";
import { Modal } from "./Modal.jsx";
import { ErrorNotice } from "./ManagementUI.jsx";
import { SelectField } from "./SelectField.jsx";
import { CredentialForm } from "./CredentialForm.jsx";
import { ExampleDownloads } from "./ExampleDownloads.jsx";
import { rankApi } from "./api.js";
import { endpointExamples, endpointAgentInstructions } from "./endpoint-guide.js";
import servingGuide from "../../../rank/docs/serving-api.md?url";
import servingGuideZh from "../../../rank/docs/serving-api.zh.md?url";

/** Endpoint integration keeps new or pasted key plaintext only in the open drawer. */
export function EndpointIntegration({ deployment, deployments, locale, t, onClose }) {
  const [keys, setKeys] = useState(null);
  const [selectedId, setSelectedId] = useState("");
  const [secret, setSecret] = useState("");
  const [createdId, setCreatedId] = useState("");
  const [creating, setCreating] = useState(false);
  const [mode, setMode] = useState("curl");
  const [copied, setCopied] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    let stale = false;
    rankApi.listCredentials(deployment.tenantId).then(items => {
      if (stale) return;
      const eligible = items.filter(item => !item.revokedAt && item.deploymentIds.includes(deployment.id) && item.scopes.includes("responses:create"));
      setKeys(eligible); setSelectedId(eligible[0]?.id || "");
    }).catch(e => { if (!stale) setError(e.message); });
    return () => { stale = true; };
  }, [deployment.id, deployment.tenantId]);
  const selected = keys?.find(item => item.id === selectedId);
  const usable = Boolean(selected && secret.trim().startsWith(selected.keyPrefix));
  const example = endpointExamples(deployment.name, location.origin);
  async function copy(value, key) {
    try { await navigator.clipboard.writeText(value); setCopied(key); setError(""); }
    catch (e) { setError(e.message); }
  }
  return <Modal label={t("integration")} className="integration-backdrop" onClose={onClose}>
    <aside className="integration-drawer">
      <header><div><h2>{t("integration")}</h2><small>{deployment.name}</small></div><button onClick={() => copy(endpointAgentInstructions(deployment.name, location.origin, locale, new URL(locale === "en" ? servingGuide : servingGuideZh, location.origin).href), "agent")}>{copied === "agent" ? <Check size={16}/> : <Copy size={16}/>} {t(copied === "agent" ? "copied" : "copyToAgent")}</button><button aria-label={t("close")} onClick={onClose}><X size={19}/></button></header>
      <div className="integration-body">
        <ErrorNotice error={error}/>
        <section className="integration-key-step">
          <h3><span className="step-number">1</span>{t("integrationGetKey")}</h3>
          {!keys && !error && <p role="status">{t("loading")}</p>}
          {keys && !creating && <>
            {keys.length > 0 ? <><SelectField aria-label="API Key" value={selectedId} onValueChange={value => { setSelectedId(value); setSecret(""); setCreatedId(""); setCopied(""); }} options={keys.map(item => ({ value: item.id, label: `${item.name} · ${item.keyPrefix}…` }))}/>
              {createdId === selectedId ? <p role="status">{t("keyReady")}</p> : <label className="integration-secret">{t("enterExistingKey")}<input type="password" aria-label={t("enterExistingKey")} autoComplete="off" value={secret} placeholder="rnk_live_..." onChange={event => { setSecret(event.target.value); setCopied(""); }}/><small>{t("existingKeyHint")}</small></label>}
            </> : <p>{t("noEligibleKeys")}</p>}
            <button className="inline-link" onClick={() => setCreating(true)}><Plus size={16}/>{t("createKey")}</button>
          </>}
          {creating && <CredentialForm tenantId={deployment.tenantId} deployments={deployments} t={t} onCancel={() => setCreating(false)} onCreated={async value => {
            const eligible = value.deploymentIds.includes(deployment.id) && value.scopes.includes("responses:create");
            if (eligible) { setKeys(items => [...(items || []), value]); setSelectedId(value.id); setCreatedId(value.id); }
            setSecret(value.apiKey); setCreating(false); setCopied("");
            if (!eligible) { setSelectedId(""); setCreatedId(value.id); setError(t("keyNotEligible")); }
          }}/>}
          {createdId && secret && <div className="new-key-copy"><button onClick={() => copy(secret, "key")}><Copy size={16}/>{t(copied === "key" ? "copied" : "copyKey")}</button><small>{t("keyOnceHint")}</small></div>}
        </section>
        <section>
          <h3><span className="step-number">2</span>{t("quickIntegration")}</h3>
          <dl className="detail-facts"><div><dt>Base URL</dt><dd><code>{example.baseURL}</code></dd></div><div><dt>Model</dt><dd><code>{deployment.name}</code></dd></div></dl>
          <div className="integration-example-heading"><div className="integration-modes">{["curl", "javascript"].map(value => <button key={value} aria-pressed={mode === value} onClick={() => { setMode(value); setCopied(""); }}>{value === "curl" ? "cURL" : "JavaScript"}</button>)}</div></div>
          <pre className="integration-code"><code>{example[mode]}</code></pre>
          <button className="primary-action" disabled={!usable} onClick={() => copy(endpointExamples(deployment.name, location.origin, secret.trim())[mode + "Command"], "example")}>{copied === "example" ? <Check size={16}/> : <Copy size={16}/>} {t(copied === "example" ? "copied" : "copyExecutable")}</button>
          <p>{t(usable ? "executableKeyHint" : "chooseKeyFirst")}</p>
        </section>
        <section><h3>{locale === "en" ? "Complete examples" : "完整案例"}</h3><ExampleDownloads endpoint={deployment.name} locale={locale}/></section>
        <a className="inline-link" href={`#/docs?topic=quickstart&endpoint=${encodeURIComponent(deployment.name)}`} target="_blank" rel="noreferrer"><BookOpen size={16}/>{t("fullDocumentation")}</a>
      </div>
    </aside>
  </Modal>;
}
