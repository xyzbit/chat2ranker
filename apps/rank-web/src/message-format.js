const TEXT_TYPES = new Set(["summary", "paragraph", "note"]);

function normalizeBlock(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  if (TEXT_TYPES.has(value.type) && typeof value.text === "string") {
    return { type: value.type, text: value.text.trim(), ...(value.type === "note" ? { tone: ["info", "success", "warning"].includes(value.tone) ? value.tone : "info" } : {}) };
  }
  if (value.type === "list" && Array.isArray(value.items)) {
    const items = value.items.filter((item) => typeof item === "string" && item.trim()).map((item) => item.trim());
    return items.length ? { type: "list", items, ordered: value.ordered === true } : null;
  }
  if (value.type === "facts" && Array.isArray(value.items)) {
    const items = value.items.filter((item) => item && typeof item.label === "string" && typeof item.value === "string").map((item) => ({ label: item.label.trim(), value: item.value.trim() }));
    return items.length ? { type: "facts", items } : null;
  }
  if (value.type === "code" && typeof value.code === "string") return { type: "code", code: value.code, language: typeof value.language === "string" ? value.language : "" };
  if (value.type === "error" && typeof value.text === "string") return { type: "error", text: value.text.trim(), detail: typeof value.detail === "string" ? value.detail.trim() : "" };
  return null;
}

function legacyTextBlocks(text) {
  const lines = text.split("\n");
  const blocks = [];
  let paragraph = [];
  const flush = () => { if (paragraph.length) blocks.push({ type: "paragraph", text: paragraph.join("\n").trim() }); paragraph = []; };
  const cells = (line) => line.trim().replace(/^\||\|$/g, "").split("|").map((cell) => cell.trim());
  for (let index = 0; index < lines.length;) {
    const line = lines[index].trim();
    if (!line) { flush(); index += 1; continue; }
    if (index + 1 < lines.length && line.includes("|") && cells(lines[index + 1]).length > 1 && cells(lines[index + 1]).every((cell) => /^:?-{3,}:?$/.test(cell))) {
      flush();
      const headers = cells(line);
      const rows = [];
      index += 2;
      while (index < lines.length && lines[index].includes("|")) rows.push(cells(lines[index++]));
      blocks.push({ type: "table", headers, rows });
      continue;
    }
    const list = line.match(/^([-*]|\d+[.)])\s+(.+)$/);
    if (list) {
      flush();
      const ordered = /^\d/.test(list[1]);
      const items = [];
      while (index < lines.length) {
        const item = lines[index].trim().match(ordered ? /^\d+[.)]\s+(.+)$/ : /^[-*]\s+(.+)$/);
        if (!item) break;
        items.push(item[1]);
        index += 1;
      }
      blocks.push({ type: "list", ordered, items });
      continue;
    }
    if (/^#{1,3}\s+/.test(line)) { flush(); blocks.push({ type: "summary", text: line.replace(/^#{1,3}\s+/, "") }); index += 1; continue; }
    paragraph.push(line);
    index += 1;
  }
  flush();
  return blocks;
}

function legacyBlocks(text) {
  return text.split(/(```[\s\S]*?```)/g).flatMap((section) => {
    const match = section.match(/^```([^\n]*)\n?([\s\S]*?)```$/);
    return match ? [{ type: "code", language: match[1].trim(), code: match[2].replace(/\n$/, "") }] : legacyTextBlocks(section);
  });
}

export function parseMessageContent(content, { streaming = false } = {}) {
  const source = String(content || "").trim();
  if (!source) return { format: "empty", blocks: [] };
  const candidate = source.replace(/^```(?:jsonl)?\s*\n/i, "").replace(/\n```$/, "");
  const lines = candidate.split("\n").map((line) => line.trim()).filter(Boolean);
  const blocks = [];
  for (let index = 0; index < lines.length; index += 1) {
    try {
      const block = normalizeBlock(JSON.parse(lines[index]));
      if (!block) throw new Error("unsupported block");
      blocks.push(block);
    } catch {
      if (streaming && index === lines.length - 1 && candidate.trimStart().startsWith("{")) return { format: "rank-jsonl", blocks };
      return { format: "legacy-markdown", blocks: legacyBlocks(source) };
    }
  }
  return { format: "rank-jsonl", blocks };
}

export function messageContentToMarkdown(content) {
  const parsed = parseMessageContent(content);
  if (parsed.format === "legacy-markdown") return String(content || "").trim();
  return parsed.blocks.map((block) => {
    if (block.type === "summary") return `**${block.text}**`;
    if (block.type === "paragraph") return block.text;
    if (block.type === "note") return `> ${block.text}`;
    if (block.type === "list") return block.items.map((item, index) => `${block.ordered ? `${index + 1}.` : "-"} ${item}`).join("\n");
    if (block.type === "facts") return block.items.map((item) => `- **${item.label}：** ${item.value}`).join("\n");
    if (block.type === "error") return `> ${block.text}${block.detail ? `\n>\n> 技术信息：${block.detail}` : ""}`;
    return `\`\`\`${block.language}\n${block.code}\n\`\`\``;
  }).join("\n\n");
}
