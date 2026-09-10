import { SelectField } from "./SelectField.jsx";
import { useState } from "react";
import { Plus } from "@phosphor-icons/react";
import { ErrorNotice, PageHeading, StatePill, InlineConfirmation } from "./ManagementUI.jsx";
import { languageOptions } from "./i18n.js";
import { protocolOptions, providerOptions } from "./model-connections.js";

/** System bindings and provider secrets are edited through explicit forms. */
export function SettingsCenter({
  connections,
  catalog,
  systemModels,
  onSave,
  onBind,
  onDelete,
  onUseConnection,
  locale,
  onLocale,
  t,
  companionSettings,
}) {
  const [draft, setDraft] = useState(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  const [removing, setRemoving] = useState(null);
  const [bindings, setBindings] = useState({});
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
    <main className="management-page">
      <PageHeading title={t("settings")} hint={t("settingsHint")} />
      <ErrorNotice error={error} />
      {saved && (
        <p className="saved-notice" role="status">
          {t("saved")}
        </p>
      )}
      <section className="management-section">
        <h2>{t("language")}</h2>
        <SelectField
          aria-label={t("language")}
          value={locale}
          onValueChange={onLocale}
          options={languageOptions}
        />
      </section>
      <section className="management-section">
        <div className="detail-title">
          <h2>{t("modelConnections")}</h2>
          <button disabled={busy} onClick={() => begin(null)}>
            <Plus size={16} />
            {t("addConnection")}
          </button>
        </div>
        {!connections.length && <p>{t("noConnections")}</p>}
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
                <input
                  required
                  list="settings-models"
                  value={draft.defaultModel}
                  onChange={(e) => update("defaultModel", e.target.value)}
                />
                <datalist id="settings-models">
                  {[
                    ...new Set([
                      ...(
                        catalog.find((p) => p.id === draft.provider)?.models ||
                        []
                      ).map((m) => m.id),
                      ...(draft.models || []),
                    ]),
                  ].map((m) => (
                    <option key={m} value={m} />
                  ))}
                </datalist>
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
      </section>
      <section className="management-section">
        <p>{t("systemModelsHint")}</p>
        {["control", "judge"].map((role) => {
          const current = systemModels.find((b) => b.role === role);
          const value = bindings[role] || {
            connectionId: current?.connectionId || "",
            model: current?.model || "",
          };
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
                <input
                  required
                  value={value.model}
                  onChange={(e) =>
                    setBindings((b) => ({
                      ...b,
                      [role]: { ...value, model: e.target.value },
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
      <section className="management-section">
        <h2>{t("companion")}</h2>
        <p>{t("companionHint")}</p>
        {companionSettings}
      </section>
    </main>
  );
}
