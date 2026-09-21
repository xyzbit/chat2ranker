import { useId, useLayoutEffect, useRef, useState } from "react";
import { CaretDown, Check } from "@phosphor-icons/react";
import { menuConfig } from "./ui-config.js";

/** Editable model ID with filtered suggestions; typing never requires selecting an option. */
export function ModelField({ value, options = [], onChange, label = "模型", placeholder = "选择或输入模型名称", disabled = false, required = true }) {
  const id = useId();
  const input = useRef(null);
  const container = useRef(null);
  const menu = useRef(null);
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const [active, setActive] = useState(-1);
  const models = [...new Map(options.map(option => {
    const item = typeof option === "string" ? { id: option, name: option } : option;
    return [item.id, item];
  })).values()].filter(item => item.id);
  const visible = models.filter(item => `${item.id} ${item.name || ""}`.toLowerCase().includes(filter.toLowerCase()));

  useLayoutEffect(() => {
    if (!open) return;
    const node = menu.current;
    const place = () => {
      const rect = container.current.getBoundingClientRect();
      const { viewportPadding, gap, maxHeight } = menuConfig;
      const below = innerHeight - rect.bottom - viewportPadding;
      const above = rect.top - viewportPadding;
      const height = Math.min(maxHeight, Math.max(below, above));
      Object.assign(node.style, { width: `${Math.min(rect.width, innerWidth - viewportPadding * 2)}px`, left: `${Math.max(viewportPadding, rect.left)}px`, top: below >= height ? `${rect.bottom + gap}px` : "auto", bottom: below >= height ? "auto" : `${innerHeight - rect.top + gap}px`, maxHeight: `${height}px` });
    };
    const toggle = event => { if (event.newState === "closed") setOpen(false); };
    node.addEventListener("toggle", toggle);
    node.showPopover();
    place();
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    return () => {
      node.removeEventListener("toggle", toggle);
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
    };
  }, [open]);

  useLayoutEffect(() => {
    if (active >= 0) document.getElementById(`${id}-option-${active}`)?.scrollIntoView({ block: "nearest" });
  }, [active, id]);

  function choose(model) {
    onChange(model); setOpen(false); setActive(-1); input.current.focus();
  }

  function keyDown(event) {
    if (event.key === "Escape" && open) {
      event.preventDefault(); event.stopPropagation(); setOpen(false);
    } else if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      if (!open) { setFilter(""); setOpen(true); setActive(-1); }
      else setActive(current => Math.max(0, Math.min(visible.length - 1, current + (event.key === "ArrowDown" ? 1 : -1))));
    } else if (event.key === "Enter" && open) {
      event.preventDefault();
      if (visible[active]) choose(visible[active].id);
      else setOpen(false);
    } else if (event.key === "Tab") setOpen(false);
  }

  return <div ref={container} className="model-field-control" onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget)) setOpen(false); }}>
    <input ref={input} role="combobox" aria-label={label} aria-autocomplete="list" aria-expanded={open} aria-controls={open ? id : undefined} aria-activedescendant={open && visible[active] ? `${id}-option-${active}` : undefined} value={value} placeholder={placeholder} disabled={disabled} required={required} autoComplete="off" spellCheck={false} onKeyDown={keyDown} onChange={event => { onChange(event.target.value); setFilter(event.target.value); setActive(-1); setOpen(true); }} />
    <button type="button" className="model-field-toggle" tabIndex={-1} aria-label={label} aria-haspopup="listbox" aria-expanded={open} disabled={disabled} onMouseDown={event => event.preventDefault()} onClick={() => { input.current.focus(); setFilter(""); setActive(-1); setOpen(current => !current); }}><CaretDown size={14} /></button>
    {open && <div ref={menu} popover="auto" className="model-field-menu" id={id} role="listbox" aria-label={label}>
      {visible.map((item, index) => <button key={item.id} id={`${id}-option-${index}`} type="button" role="option" tabIndex={-1} aria-selected={item.id === value} className={active === index ? "keyboard-active" : ""} onMouseDown={event => event.preventDefault()} onClick={() => choose(item.id)}><span>{item.id}</span>{item.id === value && <Check size={14} />}</button>)}
      {value.trim() && !models.some(item => item.id === value.trim()) && <button type="button" role="option" aria-selected="false" tabIndex={-1} onMouseDown={event => event.preventDefault()} onClick={() => choose(value.trim())}><span>{value.trim()}</span><Check size={14} /></button>}
    </div>}
  </div>;
}
