import { useEffect, useId, useRef, useState } from "react";
import { ArrowClockwise, ArrowRight, CircleNotch, WarningCircle } from "@phosphor-icons/react";
import { AssistantAvatar } from "./AssistantAvatar.jsx";
import { SelectField } from "./SelectField.jsx";
import { ModelField } from "./ModelField.jsx";
import { protocolOptions, providerOptions } from "./model-connections.js";
import { rankApi } from "./api.js";

/** Configure Rank's initial dialogue model from a local source or an explicit connection. */
export function SystemModelSetup({ catalog, onComplete, onSkip, t }) {
  const [tab, setTab] = useState("manual");
  const touched = useRef(false);
  const [candidates, setCandidates] = useState([]);
  const [candidateId, setCandidateId] = useState("");
  const [localModel, setLocalModel] = useState("");
  const [scanning, setScanning] = useState(true);
  const [scanFailed, setScanFailed] = useState(false);
  const [provider, setProvider] = useState(catalog[0]?.id || "custom");
  const template = catalog.find(item => item.id === provider);
  const [model, setModel] = useState(catalog[0]?.models[0]?.id || "");
  const [baseUrl, setBaseUrl] = useState(catalog[0]?.baseUrl || "");
  const [protocol, setProtocol] = useState(catalog[0]?.protocol || "openai-chat-completions");
  const [apiKey, setAPIKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [manualId, setManualId] = useState("");
  const inputId = useId();
  const candidate = candidates.find(item => item.id === candidateId);

  useEffect(() => {
    let active = true;
    rankApi.discoverLocalModels().then(result => {
      if (!active) return;
      setCandidates(result.candidates);
      setCandidateId(result.candidates[0]?.id || "");
      setLocalModel(result.candidates[0]?.model || "");
      if (!touched.current) setTab(result.candidates.length ? "local" : "manual");
    }, () => { if (active) setScanFailed(true); }).finally(() => { if (active) setScanning(false); });
    return () => { active = false; };
  }, []);

  async function rediscover() {
    setScanning(true); setScanFailed(false);
    try {
      const result = await rankApi.discoverLocalModels();
      const next = result.candidates.find(item => item.id === candidateId) || result.candidates[0];
      setCandidates(result.candidates);
      if (next?.id !== candidateId) { setCandidateId(next?.id || ""); setLocalModel(next?.model || ""); }
    } catch { setScanFailed(true); }
    finally { setScanning(false); }
  }

  function chooseTab(value) {
    touched.current = true;
    setTab(value); setError("");
  }

  function chooseProvider(value) {
    touched.current = true;
    const next = catalog.find(item => item.id === value);
    setProvider(value); setModel(next?.models[0]?.id || "");
    setBaseUrl(next?.baseUrl || ""); setProtocol(next?.protocol || "openai-chat-completions");
  }

  async function submit(event) {
    event.preventDefault(); setBusy(true); setError("");
    const selectedModel = (tab === "local" ? localModel : model).trim();
    try {
      let connection;
      if (tab === "local") {
        connection = await rankApi.importLocalModel(candidate.id, selectedModel);
      } else {
        const input = { name: template?.name || t("customProvider"), provider, protocol, baseUrl, apiKey, defaultModel: selectedModel };
        connection = manualId ? await rankApi.updateModelConnection(manualId, input) : await rankApi.createModelConnection(input);
        setManualId(connection.id);
      }
      if (connection.status !== "verified") connection = await rankApi.verifyModelConnection(connection.id);
      await rankApi.setSystemModel("control", { connectionId: connection.id, model: selectedModel });
      await rankApi.setSystemModel("judge", { connectionId: connection.id, model: selectedModel });
      await onComplete();
    } catch (e) { setError(e.message); }
    finally { setBusy(false); }
  }

  const ready = tab === "local" ? candidate && localModel.trim() && !scanning : apiKey.trim() && model.trim() && baseUrl.trim();
  const suggestions = tab === "local" ? [candidate?.model].filter(Boolean) : (template?.models || []).map(item => item.id);

  return <main className="system-setup"><div className="system-setup-content">
    <header className="setup-heading"><AssistantAvatar /><div><h1>Rank</h1><p>{t("setupDialogueModel")}</p></div></header>
    <div role="tablist" aria-label={t("setupConnectionMethod")} className="setup-tabs">
      {[{ id: "local", label: "localConnections" }, { id: "manual", label: "connectionSetup" }].map(item => <button key={item.id} type="button" role="tab" id={`${inputId}-${item.id}`} aria-controls={`${inputId}-panel`} aria-selected={tab === item.id} tabIndex={tab === item.id ? 0 : -1} disabled={busy} onClick={() => chooseTab(item.id)} onKeyDown={event => {
        if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) {
          event.preventDefault();
          const next = event.key === "Home" ? "local" : event.key === "End" ? "manual" : tab === "local" ? "manual" : "local";
          chooseTab(next); document.getElementById(`${inputId}-${next}`).focus();
        }
      }}>{t(item.label)}{item.id === "local" && scanning && <CircleNotch className="spin" size={14} />}</button>)}
    </div>
    <form onSubmit={submit} onFocusCapture={() => { touched.current = true; }} onChange={() => { touched.current = true; }}>
      <fieldset disabled={busy}>
        <div role="tabpanel" id={`${inputId}-panel`} aria-labelledby={`${inputId}-${tab}`} className="setup-fields">
          {tab === "local" ? <>
            {candidates.length ? <div className="setup-local-select"><label><span>{t("connection")}</span><SelectField value={candidateId} onValueChange={id => { const next = candidates.find(item => item.id === id); setCandidateId(id); setLocalModel(next?.model || ""); setError(""); }} aria-label={t("connection")} options={candidates.map(item => ({ value: item.id, label: item.name }))} /></label><button type="button" className="setup-rediscover" aria-label={t("rediscoverConnections")} title={t("rediscoverConnections")} disabled={scanning || busy} onClick={rediscover}><ArrowClockwise size={17} /></button></div>
              : <div className="setup-local-empty" role="status"><p>{t(scanning ? "discoveringConnections" : scanFailed ? "setupScanFailed" : "noLocalConnections")}</p>{!scanning && <button type="button" className="setup-switch" onClick={rediscover}><ArrowClockwise size={15} />{t("rediscoverConnections")}</button>}</div>}
          </> : <>
            <label><span>Provider</span><SelectField value={provider} onValueChange={chooseProvider} aria-label="Provider" options={providerOptions(catalog, t("customProvider"))} /></label>
            {provider === "custom" && <><label><span>Base URL</span><input type="url" required value={baseUrl} onChange={event => setBaseUrl(event.target.value)} placeholder="https://api.example.com/v1" /></label><label><span>{t("setupProtocol")}</span><SelectField value={protocol} onValueChange={setProtocol} aria-label={t("setupProtocol")} options={protocolOptions} /></label></>}
          </>}
          {(tab === "manual" || candidate) && <label><span>{t("model")}</span><ModelField label={t("model")} disabled={busy} value={tab === "local" ? localModel : model} onChange={tab === "local" ? setLocalModel : setModel} placeholder={t("modelNamePlaceholder")} options={suggestions} /></label>}
          {tab === "manual" && <label><span>API Key</span><input type="password" autoComplete="new-password" required value={apiKey} onChange={event => setAPIKey(event.target.value)} /></label>}
        </div>
        {error && <div className="setup-error" role="alert"><WarningCircle size={16} />{error}</div>}
        <button className="primary-action" type="submit" disabled={busy || !ready}>{busy && <CircleNotch className="spin" size={17} />}{t(busy ? "setupConnecting" : "startConversation")}{!busy && <ArrowRight size={17} />}</button>
      </fieldset>
    </form>
    <button className="setup-switch setup-later" type="button" disabled={busy} onClick={onSkip}>{t("setupLater")}</button>
  </div></main>;
}
