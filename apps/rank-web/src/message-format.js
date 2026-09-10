const TEXT_TYPES = new Set(["summary", "paragraph", "note"]);

function repairJSON(line) {
  let output = "";
  let string = false;
  for (let index = 0; index < line.length; index += 1) {
    const character = line[index];
    if (character === "\\" && string) {
      const next = line[index + 1];
      if ('"\\/bfnrt'.includes(next) || next === "u") { output += character + next; index += 1; }
      else output += "\\\\";
      continue;
    }
    if (character === '"') {
      if (!string) string = true;
      else {
        const next = line.slice(index + 1).match(/^\s*(.)/)?.[1];
        if (!next || ":,}]".includes(next)) string = false;
        else output += "\\";
      }
    }
    output += character;
  }
  const repaired = output.replace(/,\s*([}\]])/g, "$1");
  return repaired.startsWith("{") && repaired.endsWith("}]") ? repaired.slice(0, -1) : repaired;
}

function parseJSON(line) {
  const candidates = [line];
  if (line.startsWith('{\\"') || line.startsWith('[\\"')) candidates.push(line.replace(/\\"/g, '"'));
  for (const candidate of candidates) {
    for (const value of [candidate, repairJSON(candidate)]) {
      try {
        const parsed = JSON.parse(value);
        return typeof parsed === "string" && /^[{[]/.test(parsed.trim()) ? JSON.parse(parsed) : parsed;
      } catch { /* try the next conservative repair */ }
    }
  }
  throw new SyntaxError("invalid JSON");
}

function splitJSONValues(source) {
  const values = [];
  let start = -1;
  let depth = 0;
  let inString = false;
  let escaped = false;
  for (let index = 0; index < source.length; index += 1) {
    const character = source[index];
    if (start < 0) {
      if (/\s/.test(character)) continue;
      if (character !== "{" && character !== "[") return null;
      start = index;
      depth = 1;
      continue;
    }
    if (inString) {
      if (escaped) escaped = false;
      else if (character === "\\") escaped = true;
      else if (character === '"') inString = false;
      continue;
    }
    if (character === '"') inString = true;
    else if (character === "{" || character === "[") depth += 1;
    else if (character === "}" || character === "]") {
      depth -= 1;
      if (depth === 0) {
        values.push(source.slice(start, index + 1));
        start = -1;
      }
    }
  }
  return { values, incomplete: start >= 0 };
}

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

function rankLineBlocks(line, streaming) {
  const split = splitJSONValues(line);
  if (!split || (split.incomplete && !streaming)) return null;
  try {
    const blocks = split.values.map((value) => normalizeBlock(parseJSON(value)));
    if (blocks.some((block) => !block)) return null;
    return blocks.length || (streaming && /^\{\s*"type"\s*:/.test(line)) ? blocks : null;
  } catch { return null; /* Non-protocol JSON remains ordinary message content. */ }
}

function legacyTextBlocks(text, streaming) {
  const lines = text.split("\n");
  const blocks = [];
  let structured = false;
  let paragraph = [];
  const flush = () => { if (paragraph.length) blocks.push({ type: "paragraph", text: paragraph.join("\n").trim() }); paragraph = []; };
  const cells = (line) => line.trim().replace(/^\||\|$/g, "").split("|").map((cell) => cell.trim());
  for (let index = 0; index < lines.length;) {
    const line = lines[index].trim();
    if (!line) { flush(); index += 1; continue; }
    const rankBlocks = rankLineBlocks(line, streaming);
    if (rankBlocks) { flush(); blocks.push(...rankBlocks); structured = true; index += 1; continue; }
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
  return { blocks, structured };
}

function legacyBlocks(text, streaming) {
  const blocks = [];
  let structured = false;
  for (const section of text.split(/(```[\s\S]*?```)/g)) {
    const match = section.match(/^```([^\n]*)\n?([\s\S]*?)```$/);
    if (match) blocks.push({ type: "code", language: match[1].trim(), code: match[2].replace(/\n$/, "") });
    else {
      const parsed = legacyTextBlocks(section, streaming);
      blocks.push(...parsed.blocks);
      structured ||= parsed.structured;
    }
  }
  return { format: structured ? "mixed" : "legacy-markdown", blocks };
}

export function parseMessageContent(content, { streaming = false } = {}) {
  const source = String(content || "").trim();
  if (!source) return { format: "empty", blocks: [] };
  const candidate = source.replace(/^```(?:jsonl)?\s*\n/i, "").replace(/\n```$/, "");
  const splitValues = splitJSONValues(candidate);
  const useSplitValues = splitValues && (!splitValues.incomplete || streaming) && splitValues.values.length;
  const lines = useSplitValues
    ? splitValues.values
    : candidate.split("\n").map((line) => line.trim()).filter(Boolean);
  const blocks = [];
  let rankMessage = false;
  for (let index = 0; index < lines.length; index += 1) {
    try {
      const value = parseJSON(lines[index]);
      rankMessage ||= ["summary", "paragraph", "note", "list", "facts", "code", "error"].includes(value?.type);
      const block = normalizeBlock(value);
      if (!block) throw new Error("unsupported block");
      blocks.push(block);
    } catch {
      if (streaming && index === lines.length - 1 && candidate.trimStart().startsWith("{")) return { format: "rank-jsonl", blocks };
      if (rankMessage) {
        blocks.push({ type: "error", formatError: true, text: "部分回复格式异常，请重试。", detail: "" });
        continue;
      }
      return legacyBlocks(source, streaming);
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
