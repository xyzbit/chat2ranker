import { useEffect, useRef, useState } from "react";
import { ArrowUp, ArrowsClockwise, Check, ClockCounterClockwise, Copy, Plus, Robot, Stop, X } from "@phosphor-icons/react";
import { rankApi } from "./api.js";
import { ErrorNotice } from "./ManagementUI.jsx";
import { Modal } from "./Modal.jsx";
import { ScrollRegion } from "./ScrollRegion.jsx";
import "./trials.css";

const active = item => item && ["queued", "in_progress"].includes(item.status);

/** Durable trial panel; hiding it preserves drafts and ongoing execution. */
export function TrialCenter({ agents, launch, visible, onLocation, t, renderContent }) {
  const [configurationOpen, setConfigurationOpen] = useState(false);
  const [id, setID] = useState(launch.conversationId || "");
  const [detail, setDetail] = useState(null);
  const [history, setHistory] = useState([]);
  const [more, setMore] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const agentID = launch.agentVersionId || "";
  const deployment = launch.deploymentReference || "";
  const [input, setInput] = useState(launch?.draft || "");
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(Boolean(id || (agentID && !deployment && !launch.draft)));
  const [error, setError] = useState("");
  const [reconnecting, setReconnecting] = useState(false);
  const [copied, setCopied] = useState("");
  const [revision, setRevision] = useState(0);
  const requestKey = useRef(null);
  const sending = useRef(false);
  const generation = useRef(0);
  const scroll = useRef(null);
  const follow = useRef(true);
  const composer = useRef(null);
  const locationCallback = useRef(onLocation);
  locationCallback.current = onLocation;
  const versions = agents.flatMap(agent => agent.versions || [agent]);
  const selected = detail?.agent || versions.find(agent => agent.id === agentID);
  const familyID = selected?.familyId || "";
  const last = detail?.messages.at(-1);
  const running = active(last);
  const label = agent => agent ? `${agent.name} · v${agent.version}` : t("trialChoose");


  useEffect(() => {
    let stale = false;
    if (!launch.conversationId && agentID && !deployment && !launch.draft) {
      rankApi.listTrials({ agentVersionId: agentID, directOnly: "true" }).then(items => {
        if (!stale) { setID(items[0]?.id || ""); setLoading(Boolean(items.length)); }
      }).catch(e => { if (!stale) { setError(e.message); setLoading(false); } });
    }
    return () => { stale = true; generation.current++; };
  }, []);

  useEffect(() => {
    if (!familyID) return;
    let stale = false;
    rankApi.listTrials({ agentFamilyId: familyID }).then(items => { if (!stale) { setHistory(items); setMore(items.length === 30); } }).catch(e => { if (!stale) setError(e.message); });
    return () => { stale = true; };
  }, [revision, familyID]);

  useEffect(() => {
    if (visible) {
      locationCallback.current(selected?.id || agentID, id, detail ? (detail.conversation.deploymentId ? last?.requestedReference : "") : deployment);
      composer.current?.focus();
    }
  }, [visible, selected?.id, id, detail?.conversation.id]);

  useEffect(() => {
    if (!id) return;
    let stale = false;
    setLoading(true);
    rankApi.getTrial(id).then(value => { if (!stale) { setDetail(value); setLoading(false); } }).catch(e => { if (!stale) { setError(e.message); setLoading(false); } });
    return () => { stale = true; };
  }, [id, revision]);

  useEffect(() => {
    if (!running) return;
    let closed = false;
    const events = new EventSource(rankApi.trialEventsURL(last.id));
    events.onopen = () => setReconnecting(false);
    events.onerror = () => { if (!closed) setReconnecting(true); };
    events.addEventListener("trial.progress", event => {
      const data = JSON.parse(event.data);
      setDetail(value => value && ({ ...value, messages: value.messages.map(item => item.id === last.id ? { ...item, outputText: data.text, status: "in_progress" } : item) }));
    });
    const finish = () => { closed = true; events.close(); setReconnecting(false); setRevision(value => value + 1); };
    for (const type of ["response.completed", "response.failed", "response.cancelled", "response.requires_action"]) events.addEventListener(type, finish);
    return () => { closed = true; events.close(); setReconnecting(false); };
  }, [last?.id, running]);

  useEffect(() => {
    if (follow.current && scroll.current) scroll.current.scrollTop = scroll.current.scrollHeight;
  }, [detail, busy]);

  function newConversation() {
    generation.current++;
    historyReplace("");
    setDetail(null); setInput(""); setError(""); setLoading(false); setHistoryOpen(false);
    requestKey.current = null; follow.current = true;
    composer.current?.focus();
  }

  function historyReplace(next) {
    setID(next);
  }

  async function send(text = input) {
    if (sending.current || running || loading || !text.trim() || (!id && !agentID && !deployment)) return;
    sending.current = true; setBusy(true); setError(""); follow.current = true;
    const currentGeneration = generation.current;
    const payload = id
      ? { conversationId: id, previousResponseId: last?.id, input: text.trim() }
      : { ...(deployment ? { deploymentReference: deployment } : { agentVersionId: agentID }), input: text.trim() };
    const signature = JSON.stringify(payload);
    if (requestKey.current?.signature !== signature) requestKey.current = { signature, key: crypto.randomUUID() };
    try {
      const response = await rankApi.sendTrial(payload, requestKey.current.key);
      if (generation.current !== currentGeneration) return;
      requestKey.current = null;
      setInput(""); historyReplace(response.conversationId);
      setRevision(value => value + 1);
    } catch (e) { if (generation.current === currentGeneration) setError(e.message); }
    finally { sending.current = false; setBusy(false); }
  }

  async function stop() {
    setBusy(true); setError("");
    try { await rankApi.cancelTrial(last.id); setRevision(value => value + 1); }
    catch (e) { setError(e.message); }
    finally { setBusy(false); }
  }

  async function older() {
    try {
      const items = await rankApi.listTrials({ agentFamilyId: familyID, before: history.at(-1)?.id || "" });
      setHistory(value => [...value, ...items]); setMore(items.length === 30);
    } catch (e) { setError(e.message); }
  }

  return <section className="trial-page" aria-label={t("trials")}>
    <header className="trial-heading">
      <div>{selected && <button className="trial-agent-label" onClick={() => setConfigurationOpen(true)} title={t("trialConfiguration")}><Robot size={17} />{label(selected)}</button>}{deployment && !id && <span className="trial-deployment">{deployment}</span>}{detail?.conversation.deploymentId && <span className="trial-deployment">{last?.requestedReference}</span>}</div>
      <div className="trial-actions">
        <button aria-label={t("trialHistory")} title={t("trialHistory")} disabled={!familyID} aria-expanded={historyOpen} onClick={() => setHistoryOpen(value => !value)}><ClockCounterClockwise size={19} /></button>
        <button aria-label={t("newTrial")} title={t("newTrial")} disabled={busy || loading} onClick={newConversation}><Plus size={20} /></button>
      </div>
    </header>
    {historyOpen && <section className="trial-history" aria-label={t("trialHistory")}>
      <header><strong>{t("trialHistory")}</strong><button aria-label={t("close")} onClick={() => setHistoryOpen(false)}><X size={17}/></button></header>
      {!history.length && <p>{t("trialNoHistory")}</p>}
      {history.map(item => <button className={item.id === id ? "selected" : ""} key={item.id} onClick={() => { generation.current++; setDetail(null); setLoading(true); setInput(""); setError(""); historyReplace(item.id); setHistoryOpen(false); follow.current = true; }}>
        <span>{item.title}</span><small>{label(versions.find(agent => agent.id === item.agentVersionId))}</small>
      </button>)}
      {more && <button onClick={older}>{t("trialMore")}</button>}
    </section>}
    <ScrollRegion className="trial-scroll" scrollRef={scroll} aria-label={t("trialMode")} onScroll={event => { const node = event.currentTarget; follow.current = node.scrollHeight - node.scrollTop - node.clientHeight < 100; }}>
      <div className="trial-transcript">
        {!id && !loading && <section className="trial-start"><Robot size={32} /><h2>{selected ? label(selected) : deployment || t("trialChoose")}</h2>
        </section>}
        {loading && !detail && <p role="status">{t("loading")}</p>}
        {detail?.messages.map(item => <section className="trial-turn" key={item.id}>
          <article className="trial-user"><div>{item.input}</div></article>
          <article className="trial-assistant">
            <span className="trial-speaker"><Robot size={17} />{label(detail.agent)}</span>
            {item.outputText && renderContent(item.outputText)}
            {active(item) && <span role="status" className="trial-status">{t(reconnecting ? "trialConnecting" : "trialWorking")}<span className="trial-cursor" /></span>}
            {item.failure && <div className="trial-failure"><p>{item.status === "cancelled" ? t("trialStopped") : item.failure.message}</p>{item.id === last?.id && <button disabled={busy || running} onClick={() => send(item.input)}><ArrowsClockwise size={15}/>{t("retry")}</button>}</div>}
            {!active(item) && <div className="trial-message-actions">
              {item.outputText && <button aria-label={t(copied === item.id ? "copied" : "copy")} title={t("copy")} onClick={async () => { try { await navigator.clipboard.writeText(item.outputText); setCopied(item.id); } catch (e) { setError(e.message); } }}>{copied === item.id ? <Check size={15}/> : <Copy size={15}/>}</button>}
              <details><summary>{t("trialDetails")}</summary><dl>
                <div><dt>{t("status")}</dt><dd>{t(item.status)}</dd></div>
                <div><dt>{t("model")}</dt><dd>{detail.agent.model}</dd></div>
                <div><dt>Tokens</dt><dd>{item.usage.totalTokens}</dd></div>
                <div><dt>{t("cost")}</dt><dd>{item.costKnown ? `$${Number(item.cost).toFixed(6)}` : t("missingPrice")}</dd></div>
                <div><dt>{t("latency")}</dt><dd>{item.completedAt && item.startedAt ? `${((Date.parse(item.completedAt) - Date.parse(item.startedAt)) / 1000).toFixed(1)}s` : "—"}</dd></div>
              </dl></details>
            </div>}
          </article>
        </section>)}
      </div>
    </ScrollRegion>
    {configurationOpen && selected && <Modal label={t("trialConfiguration")} onClose={() => setConfigurationOpen(false)}>
      <section className="trial-configuration" onMouseDown={event => event.stopPropagation()}>
        <header><h2>{label(selected)}</h2><button aria-label={t("close")} onClick={() => setConfigurationOpen(false)}><X size={19}/></button></header>
        <dl className="detail-facts">{[["runner", selected.runnerType], ["model", selected.model], ["tools", selected.tools?.join(", ") || "—"], ["Skills", selected.skills?.join(", ") || "—"]].map(([key, value]) => <div key={key}><dt>{t(key)}</dt><dd>{value}</dd></div>)}</dl>
        <h3>{t("systemPrompt")}</h3><pre className="readable-output">{selected.systemPrompt || t("emptyValue")}</pre>
      </section>
    </Modal>}
    <div className="trial-composer-wrap">
      <ErrorNotice error={error} />
      <form className="trial-composer" onSubmit={event => { event.preventDefault(); void send(); }}>
        <textarea ref={composer} rows={2} aria-label={t("trialInput")} placeholder={t("trialInput")} value={input} disabled={loading || (!id && !agentID && !deployment)} onChange={event => setInput(event.target.value)} onKeyDown={event => { if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); void send(); } }} />
        {running ? <button type="button" aria-label={t("stop")} title={t("stop")} disabled={busy} onClick={stop}><Stop size={19} weight="fill"/></button> : <button type="submit" aria-label={t("sendMessage")} title={t("sendMessage")} disabled={busy || loading || !input.trim() || (!id && !agentID && !deployment)}><ArrowUp size={20}/></button>}
      </form>
    </div>
  </section>;
}
