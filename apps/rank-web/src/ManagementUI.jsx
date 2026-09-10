import { ArrowLeft, ArrowRight } from "@phosphor-icons/react";

/** Shared management-page presentation; data and actions remain owned by callers. */
export function PageHeading({ title, hint, children }) {
  return <header className="management-heading"><div><h1>{title}</h1>{hint && <p>{hint}</p>}</div><div className="management-actions">{children}</div></header>;
}

export function ErrorNotice({ error }) {
  return error ? <p className="management-error" role="alert">{error}</p> : null;
}

export function StatePill({ value, t }) {
  return <span className={`state-pill state-${value}`}>{t(value)}</span>;
}

export function EmptyState({ title, hint }) {
  return <div className="management-empty"><h2>{title}</h2>{hint && <p>{hint}</p>}</div>;
}

/** Cursor navigation renders caller-owned availability without fetching or caching pages. */
export function Pagination({ pageNumber, canPrevious, canNext, onPrevious, onNext, t }) {
  return <div className="pagination">
    <button type="button" disabled={!canPrevious} onClick={onPrevious} aria-label={t("previousPage")} title={t("previousPage")}><ArrowLeft size={17} /></button>
    <span>{pageNumber}</span>
    <button type="button" disabled={!canNext} onClick={onNext} aria-label={t("nextPage")} title={t("nextPage")}><ArrowRight size={17} /></button>
  </div>;
}

/** Inline confirmation retains the surrounding page and delegates mutation to its caller. */
export function InlineConfirmation({ title, hint, confirmLabel, busy, onCancel, onConfirm, t, headingLevel = "h2" }) {
  const Heading = headingLevel;
  return <section className="management-confirm">
    <Heading>{title}</Heading>
    <p>{hint}</p>
    <div className="management-actions">
      <button type="button" onClick={onCancel}>{t("cancel")}</button>
      <button type="button" className="danger-action" disabled={busy} onClick={onConfirm}>{confirmLabel}</button>
    </div>
  </section>;
}
