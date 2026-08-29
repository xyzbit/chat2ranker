function parseCsvRows(text) {
  const rows = [];
  let row = [];
  let field = "";
  let quoted = false;

  for (let index = 0; index < text.length; index += 1) {
    const character = text[index];
    if (character === '"') {
      if (quoted && text[index + 1] === '"') {
        field += '"';
        index += 1;
      } else {
        quoted = !quoted;
      }
    } else if (character === "," && !quoted) {
      row.push(field.trim());
      field = "";
    } else if ((character === "\n" || character === "\r") && !quoted) {
      if (character === "\r" && text[index + 1] === "\n") index += 1;
      row.push(field.trim());
      if (row.some(Boolean)) rows.push(row);
      row = [];
      field = "";
    } else {
      field += character;
    }
  }

  if (quoted) throw new Error("CSV 中存在未闭合的引号");
  row.push(field.trim());
  if (row.some(Boolean)) rows.push(row);
  return rows;
}

function normalizeRow(row, index) {
  if (typeof row === "string" || typeof row === "number" || typeof row === "boolean") {
    return { title: `用例 ${index + 1}`, input: String(row), expected: { summary: "任务成功完成" } };
  }
  if (!row || typeof row !== "object" || Array.isArray(row)) throw new Error(`第 ${index + 1} 条用例格式无效`);
  const input = row.input ?? row.prompt ?? row.task;
  if (input == null || !String(input).trim()) throw new Error(`第 ${index + 1} 条用例缺少 input、prompt 或 task`);
  let expected = row.expected ?? row.assertion ?? { summary: "任务成功完成" };
  if (typeof expected === "string" && expected.trim().startsWith("{")) {
    try { expected = JSON.parse(expected); } catch { /* Treat non-JSON assertions as summaries. */ }
  }
  if (!expected || typeof expected !== "object" || Array.isArray(expected)) expected = { summary: String(expected || "任务成功完成") };
  return {
    id: row.id == null ? undefined : String(row.id),
    title: String(row.title ?? row.name ?? `用例 ${index + 1}`),
    input: String(input),
    expected,
  };
}

export function parseImportedCases(fileName, text) {
  const extension = fileName.split(".").pop()?.toLowerCase();
  let rows;
  if (extension === "json") {
    const parsed = JSON.parse(text);
    rows = Array.isArray(parsed) ? parsed : parsed?.cases;
  } else if (extension === "jsonl") {
    rows = text.split(/\r?\n/).map((line) => line.trim()).filter(Boolean).map((line, index) => {
      try {
        return JSON.parse(line);
      } catch {
        throw new Error(`JSONL 第 ${index + 1} 行不是有效 JSON`);
      }
    });
  } else if (extension === "csv") {
    const [headers, ...values] = parseCsvRows(text);
    if (!headers?.length) rows = [];
    else rows = values.map((row) => Object.fromEntries(headers.map((header, index) => [header, row[index] ?? ""])));
  } else if (extension === "txt") {
    rows = text.split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
  } else {
    throw new Error("仅支持 JSON、JSONL、CSV 和 TXT 文件");
  }

  if (!Array.isArray(rows) || rows.length === 0) throw new Error("文件中没有识别到用例");
  return rows.map(normalizeRow);
}
