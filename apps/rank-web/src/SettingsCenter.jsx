import { AccountSettings } from "./Authentication.jsx";
import { SelectField } from "./SelectField.jsx";
import { useEffect, useId, useState } from "react";
import { Plus } from "@phosphor-icons/react";
import { ErrorNotice, PageHeading, StatePill, InlineConfirmation } from "./ManagementUI.jsx";
import { languageOptions } from "./i18n.js";
import { protocolOptions, providerOptions } from "./model-connections.js";
import { LocalModelDiscovery } from "./LocalModelDiscovery.jsx";
import { ModelField } from "./ModelField.jsx";

/** System bindings and provider secrets are edited through explicit forms. */
export function SettingsCenter({
  connections,
  catalog,
  systemModels,
  onSave,
  onBind,
  onDelete,
  onImportLocal,
  onUseConnection,
  locale,
  onLocale,
  t,
  companionSettings,
  section = "all",
  initialConnectionId = "",
}) {
  const [draft, setDraft] = useState(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [removing, setRemoving] = useState(null);
  const [bindings, setBindings] = useState({});
  const [connectionTab, setConnectionTab] = useState(section === "local_connections" ? "local" : "saved");
  const tabId = useId();
  function begin(value) {
    const template =
      catalog.find((p) => p.id === value?.provider) || catalog[0];
    setError("");
    setSaved(false);
    setDraft(
      value
        ? { ...value, apiKey: "" }
        : {
            name: template?.name || "",
            provider: template?.id || "custom",
            protocol: template?.protocol || "openai-chat-completions",
            baseUrl: template?.baseUrl || "",
            defaultModel: template?.models?.[0]?.id || "",
            apiKey: "",
          },
    );
  }
  function update(key, value) {
    setDraft((d) => ({ ...d, [key]: value }));
  }
  useEffect(() => {
    if (!initialConnectionId) return;
    const connection = connections.find(item => item.id === initialConnectionId);
    if (connection) begin(connection);
    else setError(t("noMatch"));
  }, [initialConnectionId]);
  async function run(action) {
    setBusy(true);
    setError("");
    setSaved(false);
    try {
      await action();
      setSaved(true);
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }
  async function save(e) {
    e.preventDefault();
    await run(async () => {
      await onSave(draft.id, {
        name: draft.name,
        provider: draft.provider,
        protocol: draft.protocol,
        baseUrl: draft.baseUrl,
        defaultModel: draft.defaultModel,
        apiKey: draft.apiKey,
        prices: draft.prices || {},
      });
      setDraft(null);
    });
  }
  return (
    <main className="management-page settings-page">
      {section === "all" && <PageHeading title={t("settings")} />}
      {["all", "preferences"].includes(section) && <AccountSettings locale={locale} />}
      <ErrorNotice error={error} />
      {saved && (
        <p className="saved-notice" role="status">
          {t("saved")}
        </p>
      )}
      <section className="management-section settings-language" hidden={!["all", "preferences"].includes(section)}>
        <h2>{t("language")}</h2>
        <SelectField
          aria-label={t("language")}
          value={locale}
          onValueChange={onLocale}
          options={languageOptions}
        />
      </section>
      <section className="management-section settings-connections" hidden={!["all", "model_connections", "local_connections"].includes(section)}>
        <div className="detail-title">
          {section === "all" && <h2>{t("connection")}</h2>}
          {connectionTab === "saved" && <button disabled={busy} onClick={() => begin(null)}>
            <Plus size={16} />
            {t("addConnection")}
          </button>}
        </div>
        <div className="settings-connection-tabs" role="tablist" aria-label={t("connection")}>
          {[{ id: "saved", label: "modelConnections" }, { id: "local", label: "localConnections" }].map(item => <button type="button" role="tab" key={item.id} id={`${tabId}-${item.id}`} aria-selected={connectionTab === item.id} aria-controls={`${tabId}-${item.id}-panel`} tabIndex={connectionTab === item.id ? 0 : -1} disabled={busy} onClick={() => setConnectionTab(item.id)} onKeyDown={event => {
            if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) {
              event.preventDefault();
              const next = event.key === "Home" ? "saved" : event.key === "End" ? "local" : connectionTab === "saved" ? "local" : "saved";
              setConnectionTab(next); document.getElementById(`${tabId}-${next}`).focus();
            }
          }}>{t(item.label)}</button>)}
        </div>
        <div role="tabpanel" id={`${tabId}-local-panel`} aria-labelledby={`${tabId}-local`} hidden={connectionTab !== "local"}>
          <LocalModelDiscovery onSelect={onImportLocal} disabled={busy} t={t} />
        </div>
        <div role="tabpanel" id={`${tabId}-saved-panel`} aria-labelledby={`${tabId}-saved`} hidden={connectionTab !== "saved"}>
        {!connections.length && <p className="form-hint">{t("noConnections")}</p>}
        {connections.map((c) => (
          <div className="settings-connection-row" key={c.id}>
            <button
              className="settings-connection"
              disabled={busy}
              onClick={() => begin(c)}
            >
              <span>
                <strong>{c.name}</strong>
                <small>
                  {c.provider} · {c.defaultModel}
                </small>
              </span>
              <StatePill value={c.status} t={t} />
            </button>
            {onUseConnection && c.status === "verified" && (
              <button disabled={busy} onClick={() => onUseConnection(c)}>
                {t("useConnectionInDraft")}
              </button>
            )}
          </div>
        ))}
        {draft && (
          <form className="management-form" onSubmit={save}>
            <h3>{t(draft.id ? "editConnection" : "addConnection")}</h3>
            <div className="form-grid">
              <label>
                {t("connectionName")}
                <input
                  required
                  value={draft.name}
                  onChange={(e) => update("name", e.target.value)}
                />
              </label>
              <label>
                Provider
                <SelectField
                  value={draft.provider}
                  onValueChange={(nextValue) => {
                    const p = catalog.find((c) => c.id === nextValue);
                    setDraft((d) => ({
                      ...d,
                      provider: nextValue,
                      protocol: p?.protocol || d.protocol,
                      baseUrl: p?.baseUrl || "",
                      defaultModel: p?.models?.[0]?.id || "",
                    }));
                  }}
                  aria-label="Provider"
                  options={providerOptions(
                    catalog,
                    locale === "en" ? "Custom" : "自定义 Provider",
                  )}
                />
              </label>
              <label>
                Base URL
                <input
                  type="url"
                  required
                  value={draft.baseUrl}
                  onChange={(e) => update("baseUrl", e.target.value)}
                />
              </label>
              <label>
                {t("model")}
                <ModelField
                  label={t("model")}
                  placeholder={t("modelNamePlaceholder")}
                  disabled={busy}
                  value={draft.defaultModel}
                  onChange={(value) => update("defaultModel", value)}
                  options={[...(catalog.find(p => p.id === draft.provider)?.models || []), ...(draft.models || [])]}
                />
              </label>
              <label>
                API Key
                <input
                  type="password"
                  autoComplete="new-password"
                  required={!draft.hasCredential}
                  value={draft.apiKey}
                  onChange={(e) => update("apiKey", e.target.value)}
                  placeholder={t(
                    draft.hasCredential ? "keepSecret" : "secretPlaceholder",
                  )}
                />
              </label>
              <label>
                {locale === "en" ? "Protocol" : "协议"}
                <SelectField
                  value={draft.protocol}
                  onValueChange={(nextValue) => update("protocol", nextValue)}
                  aria-label={locale === "en" ? "Protocol" : "协议"}
                  options={protocolOptions}
                />
              </label>
            </div>
            <div className="management-actions">
              {draft.id && (
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => setRemoving(draft)}
                >
                  {t("removeConnection")}
                </button>
              )}
              <button
                type="button"
                disabled={busy}
                onClick={() => setDraft(null)}
              >
                {t("cancel")}
              </button>
              <button className="primary-action" disabled={busy}>
                {t(busy ? "loading" : "verifySave")}
              </button>
            </div>
          </form>
        )}
        {removing && (
          <InlineConfirmation
            headingLevel="h3"
            title={`${t("removeConnection")} · ${removing.name}`}
            hint={t("removeConnectionHint")}
            confirmLabel={t("confirmDeleteConnection")}
            busy={busy}
            t={t}
            onCancel={() => setRemoving(null)}
            onConfirm={() =>
                  run(async () => {
                    await onDelete(removing.id);
                    setRemoving(null);
                    setDraft(null);
                  })
            }
          />
        )}
        </div>
      </section>
      <section className="management-section settings-models" hidden={!["all", "system_models"].includes(section)}>
        {section === "all" && <h2>{t("systemModels")}</h2>}
        {["control", "judge"].map((role) => {
          const current = systemModels.find((b) => b.role === role);
          const value = bindings[role] || {
            connectionId: current?.connectionId || "",
            model: current?.model || "",
          };
          const connection = connections.find(item => item.id === value.connectionId);
          return (
            <form
              key={role}
              className="binding-form"
              onSubmit={(e) => {
                e.preventDefault();
                void run(() => onBind(role, value.connectionId, value.model));
              }}
            >
              <h3>{t(role === "control" ? "controlModel" : "judgeModel")}</h3>
              <label>
                {t("connection")}
                <SelectField
                  required
                  value={value.connectionId}
                  onValueChange={(nextValue) =>
                    setBindings((b) => ({
                      ...b,
                      [role]: {
                        connectionId: nextValue,
                        model:
                          connections.find((c) => c.id === nextValue)
                            ?.defaultModel || "",
                      },
                    }))
                  }
                  aria-label={t("connection")}
                  options={[
                    { value: "", label: t("notConfigured"), disabled: true },
                    ...connections
                      .filter((c) => c.status === "verified")
                      .map((c) => ({ value: c.id, label: c.name })),
                  ]}
                />
              </label>
              <label>
                {t("model")}
                <ModelField
                  label={t("model")}
                  placeholder={t("modelNamePlaceholder")}
                  disabled={busy}
                  value={value.model}
                  options={[...(catalog.find(item => item.id === connection?.provider)?.models || []), ...(connection?.models || []), connection?.defaultModel || ""]}
                  onChange={(model) =>
                    setBindings((b) => ({
                      ...b,
                      [role]: { ...value, model },
                    }))
                  }
                />
              </label>
              <button disabled={busy || !value.connectionId}>
                {t("save")}
              </button>
            </form>
          );
        })}
      </section>
      <section className="management-section settings-companion" hidden={!["all", "preferences"].includes(section)}>
        <h2>{t("companion")}</h2>
        {companionSettings}
      </section>
    </main>
  );
}
