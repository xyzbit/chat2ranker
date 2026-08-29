const labels = {
  runnerType: "Runner",
  model: "模型",
  modelConnectionId: "模型连接",
  description: "版本说明",
  systemPrompt: "System Prompt",
  tools: "Tools",
  skills: "Skills",
};

function valueText(key, value, connections) {
  if (key === "modelConnectionId") return connections.find((item) => item.id === value)?.name || "Runner 默认连接";
  if (Array.isArray(value)) return value.length ? value.join("、") : "无";
  return String(value || "无");
}

export function agentConversationPrompt(form, base, connections = []) {
  if (base) {
    const changed = Object.keys(labels).filter((key) => JSON.stringify(form[key] ?? "") !== JSON.stringify(base[key] ?? ""));
    if (!changed.length) return `请检查 ${base.handle} v${base.version} 的配置，只询问我想修改的必要项，暂时不要创建新版本。`;
    return `请基于 ${base.handle} v${base.version} 创建并选中新版本，只修改：${changed.map((key) => `${labels[key]}为${valueText(key, form[key], connections)}`).join("；")}。未提及配置保持不变。`;
  }
  const missing = [["name", "名称"], ["handle", "Handle"]].filter(([key]) => !String(form[key] || "").trim()).map(([, label]) => label);
  if (missing.length) return `我想创建一个 Agent，目前只缺${missing.join("和")}。请只询问这些必要信息；其他配置暂定 Runner 为 ${valueText("runnerType", form.runnerType, connections)}。`;
  const details = [`名称为${form.name}`, `Handle 为${form.handle}`, `Runner 为${form.runnerType}`];
  for (const key of Object.keys(labels).filter((key) => key !== "runnerType" && form[key] && (!Array.isArray(form[key]) || form[key].length))) details.push(`${labels[key]}为${valueText(key, form[key], connections)}`);
  return `请创建并选中 Agent：${details.join("；")}。信息已完整，请直接创建。`;
}

export function experimentTitleFromMessage(text) {
  const value = String(text).replace(/@[\w/.-]+/g, "").replace(/\s+/g, " ").trim();
  const namedAgent = value.match(/名称(?:为|是|：|:)\s*([^，,；;。]+)/)?.[1]?.trim();
  return (namedAgent ? `${namedAgent} 实验` : value.split(/[。！？!?\n]/)[0]).slice(0, 24) || "未命名实验";
}
