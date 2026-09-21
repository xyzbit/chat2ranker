import { useState } from "react";
import { ArrowClockwise, CircleNotch, Plus } from "@phosphor-icons/react";
import { rankApi } from "./api.js";

/** Optional local candidates; discovery never changes a saved model binding. */
export function LocalModelDiscovery({ onSelect, t, disabled = false }) {
  const [result, setResult] = useState(null);
  const [scanning, setScanning] = useState(false);
  const [importing, setImporting] = useState("");
  const [error, setError] = useState("");

  async function discover() {
    setScanning(true);
    setError("");
    try { setResult(await rankApi.discoverLocalModels()); }
    catch { setError(t("localDiscoveryFailed")); }
    finally { setScanning(false); }
  }

  async function select(candidate) {
    setImporting(candidate.id);
    setError("");
    try {
      const connection = await onSelect(candidate);
      setResult(current => ({ ...current, candidates: current.candidates.map(item => item.id === candidate.id ? { ...item, connectionId: connection.id } : item) }));
    } catch (e) { setError(e.message); }
    finally { setImporting(""); }
  }

  return <div className="local-model-discovery">
    <div className="detail-title">
      <button type="button" disabled={disabled || scanning || Boolean(importing)} onClick={discover}>
        {scanning ? <CircleNotch size={16} className="spin" /> : <ArrowClockwise size={16} />}
        {t(scanning ? "discoveringConnections" : "rediscoverConnections")}
      </button>
    </div>
    {error && <p role="alert" className="setup-error">{error}</p>}
    {result && <div aria-live="polite">
      {!result.candidates.length && <p>{t("noLocalConnections")}</p>}
      {result.issues.map((issue, index) => <p key={`${issue.source}-${index}`} className="local-discovery-note">{issue.source}: {t("localSourceUnavailable")}</p>)}
      {result.candidates.map(candidate => <div className="settings-connection-row local-candidate" key={candidate.id}>
        <span><strong>{candidate.name}</strong><small>{candidate.model}</small><small>{candidate.baseUrl}</small></span>
        <button type="button" disabled={disabled || scanning || Boolean(importing) || Boolean(candidate.connectionId)} onClick={() => select(candidate)}>
          {importing === candidate.id ? <CircleNotch className="spin" size={16} /> : <Plus size={16} />}
          {t(candidate.connectionId ? "connectionImported" : "importConnection")}
        </button>
      </div>)}
    </div>}
  </div>;
}
