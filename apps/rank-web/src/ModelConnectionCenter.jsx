import { SelectField } from "./SelectField.jsx";
import { useId, useLayoutEffect, useMemo, useRef, useState } from "react";
import { ArrowClockwise, ArrowLeft, ArrowRight, CaretDown, CheckCircle, CircleNotch, Key, MagnifyingGlass, Plus, Robot, Trash, WarningCircle } from "@phosphor-icons/react";
import { isManualModel, modelOptions, protocolOptions, providerOptions } from "./model-connections.js";
import { menuConfig, menuStyle } from "./ui-config.js";

const emptyPrice = { input: "", output: "", cacheRead: "", cacheWrite: "" };

function providerTemplate(catalog, id) {
  return catalog.find((item) => item.id === id);
}

function priceFor(catalog, provider, model) {
  return providerTemplate(catalog, provider)?.models.find((item) => item.id === model);
}

function draftFor(connection, catalog) {
  const template = providerTemplate(catalog, connection?.provider) || catalog[0];
  const model = connection?.defaultModel || template?.models[0]?.id || "";
  const price = connection?.prices?.[model];
  return {
    id: connection?.id || "",
    name: connection?.name || template?.name || "",
    provider: connection?.provider || template?.id || "custom",
    protocol: connection?.protocol || template?.protocol || "openai-chat-completions",
    baseUrl: connection?.baseUrl || template?.baseUrl || "",
    apiKey: "",
    defaultModel: model,
    models: connection?.models || [],
    prices: connection?.prices || {},
    price: price ? Object.fromEntries(Object.entries(price).map(([key, value]) => [key, String(value)])) : emptyPrice,
    overridePrice: Boolean(price),
    hasCredential: Boolean(connection?.hasCredential),
    status: connection?.status || "missing_credential",
    statusMessage: connection?.statusMessage || "",
  };
}

function priceInput(price) {
  return Object.fromEntries(Object.entries(price).map(([key, value]) => [key, value === "" ? 0 : Number(value)]));
}

function formatPrice(value) {
  return Number(value).toLocaleString("en-US", { maximumFractionDigits: 4 });
}

function ModelPicker({ value, options, onChange }) {
  const [open, setOpen] = useState(false);
  const [manual, setManual] = useState(() => !options.length || isManualModel(value, options));
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const trigger = useRef(null);
  const menu = useRef(null);
  const listId = useId();
  const visible = options.filter((item) => `${item.name} ${item.id}`.toLowerCase().includes(query.trim().toLowerCase()));
  const selected = options.find((item) => item.id === value);

  useLayoutEffect(() => {
    if (!open) return;
    const node = menu.current;
    const place = () => {
      const rect = trigger.current.getBoundingClientRect();
      const { gap, viewportPadding, maxHeight, minWidth } = menuConfig;
      const width = Math.min(Math.max(rect.width, minWidth), innerWidth - viewportPadding * 2);
      const below = innerHeight - rect.bottom - viewportPadding;
      const above = rect.top - viewportPadding;
      const height = Math.min(maxHeight, Math.max(below, above));
      Object.assign(node.style, {
        width: `${width}px`,
        left: `${Math.max(viewportPadding, Math.min(rect.left, innerWidth - width - viewportPadding))}px`,
        top: below >= height ? `${rect.bottom + gap}px` : "auto",
        bottom: below >= height ? "auto" : `${innerHeight - rect.top + gap}px`,
        maxHeight: `${height}px`,
      });
    };
    const toggle = (event) => { if (event.newState === "closed") setOpen(false); };
    node.addEventListener("toggle", toggle);
    node.showPopover();
    place();
    node.querySelector("input").focus();
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    return () => {
      node.removeEventListener("toggle", toggle);
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
    };
  }, [open]);

  function choose(item) {
    onChange(item.id);
    setOpen(false);
    setQuery("");
    trigger.current.focus();
  }

  function handleKey(event) {
    if (event.key === "Escape") { event.preventDefault(); event.stopPropagation(); setOpen(false); trigger.current.focus(); }
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const next = Math.max(0, Math.min(visible.length - 1, active + (event.key === "ArrowDown" ? 1 : -1)));
      setActive(next);
      document.getElementById(`${listId}-${next}`)?.scrollIntoView({ block: "nearest" });
    }
    if (event.key === "Enter" && visible[active]) { event.preventDefault(); choose(visible[active]); }
  }

  if (manual) return <div className="manual-model">
    <input required autoFocus value={value} onChange={(event) => onChange(event.target.value)} placeholder="输入模型 ID" />
    {options.length > 0 && <button type="button" onClick={() => { onChange(options[0].id); setManual(false); }}>返回模型列表</button>}
    <small>保存时会用该模型发起最小请求验证。</small>
  </div>;

  return <div className="model-combobox" onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget)) setOpen(false); }}>
    <button ref={trigger} type="button" className="model-combobox-trigger" aria-label="选择模型" aria-haspopup="listbox" aria-controls={open ? listId : undefined} aria-expanded={open} onClick={() => { setActive(0); setOpen((current) => !current); }}>
      <span><strong>{selected?.name || value}</strong><small>{selected?.id || value}</small></span><CaretDown size={14} />
    </button>
    {open && <div ref={menu} popover="auto" className="model-combobox-menu" style={menuStyle}>
      <label><MagnifyingGlass size={14} /><input role="combobox" aria-label="搜索模型" aria-expanded="true" aria-controls={listId} aria-activedescendant={visible[active] ? `${listId}-${active}` : undefined} value={query} onKeyDown={handleKey} onChange={(event) => { setQuery(event.target.value); setActive(0); }} placeholder="搜索模型" /></label>
      <div id={listId} role="listbox" aria-label="模型">{visible.map((item, index) => <button id={`${listId}-${index}`} tabIndex={-1} className={index === active ? "keyboard-active" : ""} type="button" role="option" aria-selected={item.id === value} key={item.id} onClick={() => choose(item)}>
        <span><strong>{item.name}</strong><small>{item.id}</small></span><em>{item.source === "official" ? "官方目录" : "连接发现"}</em>
      </button>)}{!visible.length && <p>没有匹配模型</p>}</div>
      <button type="button" className="manual-model-action" onClick={() => { setManual(true); setOpen(false); }}><Plus size={14} />使用其他模型 ID</button>
    </div>}
  </div>;
}

export function ModelConnectionCenter({ connections, catalog, systemModels = [], onBind, onSave, onSync, onDelete, onUse }) {
  const [draft, setDraft] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const template = useMemo(() => draft && providerTemplate(catalog, draft.provider), [catalog, draft]);
  const inherited = draft && priceFor(catalog, draft.provider, draft.defaultModel);
  const options = modelOptions(template?.models, draft?.models);

  function update(field, value) {
    setDraft((current) => ({ ...current, [field]: value }));
  }

  function selectProvider(provider) {
    const next = providerTemplate(catalog, provider);
    setDraft((current) => ({ ...current, provider, name: current.id ? current.name : next?.name || current.name, protocol: next?.protocol || current.protocol, baseUrl: next?.baseUrl || current.baseUrl, defaultModel: next?.models[0]?.id || "", price: emptyPrice, overridePrice: false }));
  }

  function selectModel(model) {
    const override = draft.prices[model];
    setDraft((current) => ({ ...current, defaultModel: model, overridePrice: Boolean(override), price: override ? Object.fromEntries(Object.entries(override).map(([key, value]) => [key, String(value)])) : emptyPrice }));
  }

  async function submit(event) {
    event.preventDefault();
    setBusy(true); setError("");
    try {
      const prices = { ...draft.prices };
      if (draft.overridePrice && draft.defaultModel) prices[draft.defaultModel] = priceInput(draft.price);
      else delete prices[draft.defaultModel];
      const saved = await onSave(draft.id, { name: draft.name, provider: draft.provider, protocol: draft.protocol, baseUrl: draft.baseUrl, apiKey: draft.apiKey, defaultModel: draft.defaultModel, prices });
      setDraft(null);
      if (saved.status === "verified") onUse?.(saved);
    } catch (saveError) { setError(saveError.message); }
    finally { setBusy(false); }
  }

  async function remove() {
    setBusy(true); setError("");
    try { await onDelete(draft.id); setDraft(null); }
    catch (deleteError) { setError(deleteError.message); }
    finally { setBusy(false); }
  }

  async function sync() {
    setBusy(true); setError("");
    try { setDraft(draftFor(await onSync(draft.id), catalog)); }
    catch { setError(`同步失败，已保留上次 ${draft.models.length} 个模型。请检查 Provider 服务或连接配置后重试。`); }
    finally { setBusy(false); }
  }

  if (!draft) return (
    <section className="connection-center">
      <header><div><small>LLM CONNECTIONS</small><h3>模型连接</h3><p>Agent 只需选择已验证的连接和模型。</p></div><button type="button" onClick={() => setDraft(draftFor(null, catalog))}><Plus size={15} />添加</button></header>
      <div className="system-model-slots">{[["control", "对话模型"], ["judge", "评审模型"]].map(([role, label]) => {
        const binding = systemModels.find((item) => item.role === role);
        return <label key={role}><span><strong>{label}</strong><small>{role === "control" ? "Rank 对话" : "LLM Judge"}</small></span><SelectField value={binding?.connectionId || ""} onValueChange={nextValue => onBind(role, nextValue)} aria-label={label} options={[{ value: "", label: "未配置", disabled: true }, ...connections.filter(item => item.status === "verified").map(item => ({ value: item.id, label: <>{item.name} · {item.defaultModel}</> }))]} /></label>;
      })}</div>
      <div className="connection-list">
        {connections.map((connection) => <button type="button" key={connection.id} onClick={() => setDraft(draftFor(connection, catalog))}>
          <span className={`connection-state ${connection.status}`}><Robot size={17} /></span>
          <span><strong>{connection.name}</strong><small>{connection.status === "verified" ? `已连接 · ${connection.models.length || 1} 个模型` : connection.status === "missing_credential" ? "待添加 API Key" : connection.status === "failed" ? "验证失败" : "待验证"}</small></span>
          <em>{connection.defaultModel || "未选择模型"}</em><ArrowRight size={14} />
        </button>)}
        {!connections.length && <div className="connection-empty"><Key size={22} /><strong>还没有模型连接</strong><span>添加一个 Provider，验证后即可在 Agent 中使用。</span></div>}
      </div>
    </section>
  );

  return (
    <form className="connection-form" onSubmit={submit}>
      <button type="button" className="connection-back" onClick={() => { setDraft(null); setError(""); }}><ArrowLeft size={14} />返回模型连接</button>
      <header><div><small>{draft.id ? "MODEL CONNECTION" : "NEW CONNECTION"}</small><h3>{draft.id ? draft.name : "添加模型连接"}</h3></div>{draft.id && <div className="connection-header-actions"><span className={`connection-status ${draft.status === "verified" ? "ready" : "missing"}`}>{draft.status === "verified" ? `已同步 ${draft.models.length} 个模型` : draft.status === "failed" ? "同步失败" : draft.hasCredential ? "待同步" : "缺少凭据"}</span>{draft.hasCredential && <button type="button" onClick={sync} disabled={busy}><ArrowClockwise className={busy ? "spin" : ""} size={14} />同步模型</button>}</div>}</header>
      <div className="connection-fields">
        <label><span>Provider</span><SelectField value={draft.provider} onValueChange={selectProvider} aria-label="Provider" options={providerOptions(catalog, "自定义 Provider")} /></label>
        <label><span>名称</span><input required value={draft.name} onChange={(event) => update("name", event.target.value)} placeholder="团队 DeepSeek" /></label>
        <label className="wide"><span>Base URL</span><input required value={draft.baseUrl} onChange={(event) => update("baseUrl", event.target.value)} placeholder="https://api.example.com/v1" /></label>
        <label><span>API Key</span><input type="password" autoComplete="new-password" value={draft.apiKey} onChange={(event) => update("apiKey", event.target.value)} placeholder={draft.hasCredential ? "留空保留当前凭据" : "安全保存到 executiond"} /></label>
        <div className="model-field"><span>模型</span><ModelPicker key={`${draft.provider}:${draft.models.join(",")}`} value={draft.defaultModel} options={options} onChange={selectModel} /></div>
      </div>

      <div className={`effective-price ${draft.overridePrice ? "override" : inherited ? "inherited" : "missing"}`}>
        {draft.overridePrice ? <><CheckCircle size={16} /><span><strong>使用本地价格</strong><small>该连接的价格覆盖模型目录默认值。</small></span></> : inherited ? <><CheckCircle size={16} /><span><strong>继承 {template.name} 官方默认价格</strong><small>输入 ${formatPrice(inherited.price.input)} · 输出 ${formatPrice(inherited.price.output)}{inherited.price.cacheRead ? ` · 缓存读取 $${formatPrice(inherited.price.cacheRead)}` : ""} / 1M Token</small>{inherited.priceNote && <small>{inherited.priceNote}</small>}{(inherited.sourceUrl || template.sourceUrl) && <a href={inherited.sourceUrl || template.sourceUrl} target="_blank" rel="noreferrer">查看官方价格 · 更新于 {inherited.updatedAt}</a>}</span></> : <><WarningCircle size={16} /><span><strong>缺少计价配置</strong><small>不影响运行；结果不会展示成本。</small></span></>}
      </div>

      <details className="connection-advanced">
        <summary>高级设置</summary>
        <div className="advanced-fields">
          <label><span>协议</span><SelectField value={draft.protocol} onValueChange={nextValue => update("protocol", nextValue)} aria-label="协议" options={protocolOptions} /></label>
          <label className="price-toggle"><input type="checkbox" checked={draft.overridePrice} onChange={(event) => update("overridePrice", event.target.checked)} /><span>为 {draft.defaultModel || "当前模型"} 设置本地价格</span></label>
          {draft.overridePrice && <div className="price-fields">{[["input", "输入"], ["output", "输出"], ["cacheRead", "缓存读取"], ["cacheWrite", "缓存写入"]].map(([key, label]) => <label key={key}><span>{label} · $/1M</span><input type="number" min="0" step="any" value={draft.price[key] ?? ""} onChange={(event) => update("price", { ...draft.price, [key]: event.target.value })} placeholder="0" /></label>)}</div>}
        </div>
      </details>

      {error && <div className="connection-error"><WarningCircle size={15} />{error}</div>}
      <footer>{draft.id && <button type="button" className="delete-connection" onClick={remove} disabled={busy}><Trash size={14} />删除</button>}<span /><button type="button" onClick={() => setDraft(null)}>取消</button><button className="primary-action" type="submit" disabled={busy || !draft.name || !draft.baseUrl || !draft.defaultModel}>{busy ? <CircleNotch size={16} className="spin" /> : <CheckCircle size={16} />}{draft.apiKey || draft.hasCredential ? "验证并保存" : "保存配置"}</button></footer>
    </form>
  );
}
