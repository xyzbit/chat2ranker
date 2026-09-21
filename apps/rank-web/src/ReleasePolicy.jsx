import { useEffect, useState } from "react";
import { CheckCircle, WarningCircle } from "@phosphor-icons/react";
import { rankApi } from "./api.js";
import { SelectField } from "./SelectField.jsx";
import { ErrorNotice } from "./ManagementUI.jsx";
import { formatDateTime } from "./i18n.js";

/** Loads release evidence and rejects stale preflight results after selection changes. */
export function useReleaseReview(deploymentId, alias, agentVersionId) {
  const [evaluations, setEvaluations] = useState({ version: "", items: [] });
  const [selection, setSelection] = useState({ version: "", id: "" });
  const [result, setResult] = useState(null);
  const [listError, setListError] = useState("");
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    let stale = false;
    setListError("");
    if (agentVersionId) rankApi.listAgentEvaluations(agentVersionId).then(items => {
      if (!stale) setEvaluations({ version: agentVersionId, items });
    }).catch(error => { if (!stale) setListError(error.message); });
    return () => { stale = true; };
  }, [agentVersionId, revision]);
  const loaded = evaluations.version === agentVersionId;
  const items = loaded ? evaluations.items : [];
  const evidenceRunId = selection.version === agentVersionId ? selection.id : items.find(item => item.status === "complete")?.id || "";
  const key = JSON.stringify([deploymentId, alias, agentVersionId, evidenceRunId, revision]);
  useEffect(() => {
    let stale = false;
    if (loaded && deploymentId && agentVersionId) rankApi.preflightRelease(deploymentId, alias, { agentVersionId, evidenceRunId }).then(preflight => {
      if (!stale) setResult({ key, preflight });
    }).catch(error => { if (!stale) setResult({ key, error: error.message }); });
    return () => { stale = true; };
  }, [key, loaded, deploymentId, alias, agentVersionId, evidenceRunId]);
  const current = result?.key === key ? result : null;
  return { loaded, items, evidenceRunId, preflight: current?.preflight, error: listError || current?.error,
    selectEvidence: id => setSelection({ version: agentVersionId, id }),
    refresh: () => { setResult(null); setRevision(value => value + 1); } };
}

/** Shows the persisted numeric policy checks without treating a completed Run as approval. */
export function ReleaseChecks({ evaluation, t }) {
  return <ul className="release-checks">{evaluation.checks.map(check => <li key={check.code} data-passed={check.passed}>
    {check.passed ? <CheckCircle size={16} /> : <WarningCircle size={16} />}
    <span>{t(`releaseCheck_${check.code}`)}</span>
    {["pass_rate", "valid_trial_rate"].includes(check.code) && <strong>{check.actual.toFixed(1)}% / {check.required}%</strong>}
    {check.code === "evidence_age" && <strong>{check.actual.toFixed(1)}h / {check.required}h</strong>}
  </li>)}</ul>;
}

export function ReleaseReview({ review, t, locale }) {
  return <section className="release-review">
    <label><span>{t("evaluationEvidence")}</span>{review.loaded && <SelectField aria-label={t("evaluationEvidence")} value={review.evidenceRunId} onValueChange={review.selectEvidence} options={[
      { value: "", label: t("noReleaseEvidence") },
      ...review.items.map(item => ({ value: item.id, label: `${item.datasetName} v${item.datasetVersion} · ${formatDateTime(locale, item.createdAt)}` })),
    ]} />}</label>
    <ErrorNotice error={review.error} />
    {!review.preflight && !review.error && <p role="status">{t("releaseChecking")}</p>}
    {review.error && <button type="button" onClick={review.refresh}>{t("releaseRecheck")}</button>}
    {review.preflight && <><p className="release-check-result" role="status">{t(review.preflight.evaluation.passed ? "releaseGatePassed" : "releaseGateBlocked")}</p><ReleaseChecks evaluation={review.preflight.evaluation} t={t} /></>}
  </section>;
}

/** Edits versioned publication gates independently of immutable execution limits. */
export function ReleasePolicyEditor({ deployment, datasets, t, embedded = false }) {
  const [policy, setPolicy] = useState(null);
  const [draft, setDraft] = useState(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);
  useEffect(() => {
    let stale = false;
    rankApi.getReleasePolicy(deployment.id).then(value => {
      if (!stale) { setPolicy(value); setDraft(value.revision ? value : { datasetVersionId: "", minPassRate: 100, minValidTrialRate: 100, maxEvidenceAgeHours: 24 }); }
    }).catch(error => { if (!stale) setError(error.message); });
    return () => { stale = true; };
  }, [deployment.id]);
  async function save(event) {
    event.preventDefault(); setBusy(true); setError(""); setSaved(false);
    try {
      const value = await rankApi.saveReleasePolicy(deployment.id, { expectedRevision: policy.revision, datasetVersionId: "", minPassRate: Number(draft.minPassRate), minValidTrialRate: Number(draft.minValidTrialRate), maxEvidenceAgeHours: Number(draft.maxEvidenceAgeHours) });
      setPolicy(value); setDraft(value); setSaved(true);
    } catch (error) { setError(error.status === 409 ? t("releasePolicyConflict") : error.message); } finally { setBusy(false); }
  }
  return <section className="management-section release-policy-editor">{!embedded && <h2>{t("releasePolicy")}</h2>}<ErrorNotice error={error} />
    {draft && <form className="management-form" onSubmit={save} onChange={() => setSaved(false)}>
      <div className="form-grid">
        {[["minPassRate", "releaseMinPassRate", 0, 100], ["minValidTrialRate", "releaseMinCoverage", 1, 100], ["maxEvidenceAgeHours", "releaseMaxAge", 1, undefined]].map(([key, label, min, max]) => <label key={key}>{t(label)}<input type="number" required min={min} max={max} step="1" value={draft[key]} onChange={event => setDraft(value => ({ ...value, [key]: event.target.value }))} /></label>)}
      </div>
      <div className="management-actions"><button className="primary-action" disabled={busy}>{t(busy ? "saving" : "save")}</button><span role="status">{saved ? t("saved") : policy.revision ? t("releasePolicyRevision", { revision: policy.revision }) : t("releasePolicyDefaults")}</span></div>
    </form>}
  </section>;
}
