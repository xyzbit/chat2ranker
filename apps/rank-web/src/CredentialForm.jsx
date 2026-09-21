import { useState } from "react";
import { rankApi } from "./api.js";
import { ErrorNotice } from "./ManagementUI.jsx";

/** Public operations offered by the serving API. */
export const servingScopes = ["responses:create", "responses:read", "responses:cancel", "responses:approve"];

/** Local timestamp used as the editable default API key name. */
export function defaultKeyName(now = new Date()) {
  const pad = value => String(value).padStart(2, "0");
  return `apikey-${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}-${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`;
}

/** Creates keys with an explicit snapshot of selected endpoint and operation permissions. */
export function CredentialForm({ tenantId, deployments, t, onCreated, onCancel }) {
  const [name, setName] = useState(defaultKeyName);
  const [ids, setIds] = useState(() => deployments.map(item => item.id));
  const [scopes, setScopes] = useState(servingScopes);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  function toggle(set, value) { set(items => items.includes(value) ? items.filter(item => item !== value) : [...items, value]); }
  async function submit(event) {
    event.preventDefault(); setBusy(true); setError("");
    try { await onCreated(await rankApi.createCredential({ tenantId, name: name.trim(), scopes, deploymentIds: ids })); }
    catch (e) { setError(e.message); }
    finally { setBusy(false); }
  }
  return <form className="management-form credential-form" onSubmit={submit}>
    <h2>{t("createKey")}</h2><ErrorNotice error={error} />
    <label>{t("name")}<input autoFocus required maxLength={120} value={name} disabled={busy} onChange={event => setName(event.target.value)} /></label>
    <h3>{t("permissions")}</h3>
    <div className="credential-permissions">
      {[
        { title: "deployments", values: deployments.map(item => ({ value: item.id, label: item.name })), selected: ids, set: setIds },
        { title: "scopes", values: servingScopes.map(value => ({ value, label: t(value) })), selected: scopes, set: setScopes },
      ].map(group => <fieldset key={group.title}>
        <legend>{t(group.title)}</legend>
        <label className="check-label permission-all"><input type="checkbox" disabled={busy || !group.values.length} checked={group.values.length > 0 && group.selected.length === group.values.length} ref={node => { if (node) node.indeterminate = group.selected.length > 0 && group.selected.length < group.values.length; }} onChange={event => group.set(event.target.checked ? group.values.map(item => item.value) : [])} />{t("selectAll")}</label>
        <div className="permission-options">{group.values.map(item => <label className="check-label" key={item.value}><input type="checkbox" disabled={busy} checked={group.selected.includes(item.value)} onChange={() => toggle(group.set, item.value)} />{item.label}</label>)}</div>
      </fieldset>)}
    </div>
    <p className="form-hint">{t("keyPermissionHint")}</p>
    <div className="management-actions"><button type="button" disabled={busy} onClick={onCancel}>{t("cancel")}</button><button className="primary-action" disabled={busy || !name.trim() || !ids.length || !scopes.length}>{t(busy ? "saving" : "createKey")}</button></div>
  </form>;
}
