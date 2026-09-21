import { useState } from "react";
import { ArrowLeft } from "@phosphor-icons/react";
import { SelectField } from "./SelectField.jsx";

/** Addressable comparison of two immutable versions in one Agent family. */
export function AgentVersionComparison({ selected, family, t }) {
  const versions = family?.versions || [selected];
  const [leftID, setLeftID] = useState(versions.find(item => item.id !== selected.id)?.id || selected.id);
  const [rightID, setRightID] = useState(selected.id);
  return <main className="management-page agent-comparison-page">
    <a className="inline-link" href={`#/agents?agent=${encodeURIComponent(selected.id)}`}><ArrowLeft size={15} />{selected.name}</a>
    <header className="management-heading"><h1>{t("versionCompare")}</h1></header>
    <div className="version-comparison">
      {[[leftID, setLeftID, "compareBase"], [rightID, setRightID, "compareTarget"]].map(([id, onValueChange, label]) => {
        const version = versions.find(item => item.id === id);
        const other = versions.find(item => item.id === (label === "compareBase" ? rightID : leftID));
        return <section key={label}>
          <label>{t(label)}<SelectField value={id} onValueChange={onValueChange} aria-label={t(label)} options={versions.map(item => ({value: item.id, label: `v${item.version}`}))} /></label>
          <dl className="detail-facts">{["runnerType", "model", "description", "tools", "skills"].map(key => <div key={key} className={JSON.stringify(version[key]) !== JSON.stringify(other[key]) ? "comparison-changed" : ""}><dt>{t(({runnerType: "runner", skills: "Skills"})[key] || key)}</dt><dd>{Array.isArray(version[key]) ? version[key].join(", ") || t("emptyValue") : version[key] || t("emptyValue")}</dd></div>)}</dl>
          <h2>{t("systemPrompt")}</h2><pre className={`readable-output ${version.systemPrompt !== other.systemPrompt ? "comparison-changed" : ""}`}>{version.systemPrompt || t("emptyValue")}</pre>
        </section>;
      })}
    </div>
  </main>;
}
