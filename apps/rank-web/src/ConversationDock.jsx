import { useEffect, useRef, useState } from "react";
import { ArrowsOutSimple, X } from "@phosphor-icons/react";

/** Shared conversation container; Rank and trial children keep independent state. */
export function ConversationDock({ open, mode, canTrial, onMode, onClose, onExpand, modalActive, t, children }) {
  const root = useRef(null);
  const callbacks = useRef({ onClose });
  callbacks.current = { onClose };
  const [compact, setCompact] = useState(() => matchMedia("(max-width: 1100px)").matches);
  useEffect(() => {
    const media = matchMedia("(max-width: 1100px)");
    const update = () => setCompact(media.matches);
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);
  useEffect(() => {
    if (!open || modalActive) return;
    const previous = document.activeElement;
    root.current?.querySelector(mode === "rank" ? "#task-input" : ".trial-composer textarea")?.focus();
    const background = compact ? [...document.querySelectorAll(".workspace-context, .product-sidebar, .product-topbar, .mobile-navigation")] : [];
    const prior = background.map(node => node.inert);
    background.forEach(node => { node.inert = true; });
    function keydown(event) {
      if (event.defaultPrevented || document.querySelector("dialog[open]")) return;
      if (event.key === "Escape") { event.preventDefault(); callbacks.current.onClose(); }
      if (event.key !== "Tab" || !compact) return;
      const controls = [...root.current.querySelectorAll("button:not(:disabled), textarea:not(:disabled), input:not(:disabled), summary, a[href]")].filter(node => node.getClientRects().length);
      const first = controls[0], last = controls.at(-1);
      if (event.shiftKey && (document.activeElement === first || !root.current.contains(document.activeElement))) { event.preventDefault(); last?.focus(); }
      else if (!event.shiftKey && (document.activeElement === last || !root.current.contains(document.activeElement))) { event.preventDefault(); first?.focus(); }
    }
    document.addEventListener("keydown", keydown);
    return () => {
      background.forEach((node, index) => { node.inert = prior[index]; });
      document.removeEventListener("keydown", keydown);
      if (previous?.isConnected) previous.focus();
    };
  }, [open, mode, compact, modalActive]);
  return <aside ref={root} className="conversation-dock" hidden={!open} role={compact ? "dialog" : "complementary"} aria-modal={compact || undefined} aria-label={t("conversationMode")}>
    <header className="dock-heading">
      <div role="tablist" aria-label={t("conversationMode")} onKeyDown={event => {
        if (!canTrial || !["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
        event.preventDefault();
        onMode(event.key === "Home" ? "rank" : event.key === "End" ? "trial" : mode === "rank" ? "trial" : "rank");
        requestAnimationFrame(() => root.current?.querySelector('[role="tab"][aria-selected="true"]')?.focus());
      }}>
        <button id="dock-rank-tab" role="tab" aria-controls="dock-rank" tabIndex={mode === "rank" ? 0 : -1} aria-selected={mode === "rank"} onClick={() => onMode("rank")}>{t("assistant")}</button>
        {canTrial && <button id="dock-trial-tab" role="tab" aria-controls="dock-trial" tabIndex={mode === "trial" ? 0 : -1} aria-selected={mode === "trial"} onClick={() => onMode("trial")}>{t("trialMode")}</button>}
      </div>
      {mode === "rank" && <button className="dock-icon" title={t("fullConversation")} aria-label={t("fullConversation")} onClick={onExpand}><ArrowsOutSimple size={18} /></button>}
      <button className="dock-icon" title={t("close")} aria-label={t("close")} onClick={onClose}><X size={19} /></button>
    </header>
    {children}
  </aside>;
}
