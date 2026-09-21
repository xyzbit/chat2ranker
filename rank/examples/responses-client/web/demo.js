import { marked } from "/marked.js";
import DOMPurify from "/purify.js";

const $ = id => document.getElementById(id);
function markdown(node, value) {
  node.innerHTML = DOMPurify.sanitize(marked.parse(value), { USE_PROFILES: { html: true }, FORBID_TAGS: ["img", "video", "audio", "style", "input", "button", "form"] });
  for (const a of node.querySelectorAll("a")) { a.target = "_blank"; a.rel = "noreferrer"; }
}
let state;
let busy = false;
const emptyChat = $("chat-empty").cloneNode(true);

function showError(error) { $("error").textContent = error || ""; $("error").hidden = !error; }
async function api(path, body) {
  const response = await fetch(path, body === undefined ? {} : { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
  if (!response.ok) { const value = await response.json(); throw new Error(value.error || `HTTP ${response.status}`); }
  return response;
}
function setBusy(value, chat = false) {
  busy = value;
  $("generate").disabled = value;
  $("new-chat").disabled = value;
  $("message").disabled = value || state?.broken;
  $("send").disabled = value || state?.broken || !$("message").value.trim();
  $("stop").hidden = !value || !chat;
  $("stop").disabled = false;
  $("chat-status").textContent = state?.broken ? "本轮未完成，请新建对话" : value ? "正在处理…" : "";
}
function selectTab(name) {
  for (const tab of ["news", "chat"]) { $(tab + "-tab").setAttribute("aria-selected", String(tab === name)); $(tab + "-panel").hidden = tab !== name; }
  $("heading").textContent = name === "news" ? "每日热点" : "聊天助手";
  location.hash = name;
}
for (const tab of ["news", "chat"]) $(tab + "-tab").onclick = () => selectTab(tab);
document.querySelector(".tabs").setAttribute("role", "tablist");
document.querySelector(".tabs").onkeydown = event => {
  if (!["ArrowLeft", "ArrowRight"].includes(event.key)) return;
  event.preventDefault();
  const next = $("news-panel").hidden ? "news" : "chat";
  selectTab(next); $(next + "-tab").focus();
};
function metadata(container, value) {
  container.replaceChildren();
  if (!value.id) return;
  const version = value.deployment?.agent_version_id;
  const details = document.createElement("details"), summary = document.createElement("summary"), ids = document.createElement("p");
  summary.textContent = value.elapsedMs ? `${(value.elapsedMs / 1000).toFixed(1)}s · 请求详情` : "请求详情";
  ids.textContent = `Response: ${value.id}${version ? "\nAgent: " + version : ""}`;
  details.append(summary, ids); container.append(details);
  const link = document.createElement("a"); link.href = state.platformURL + "/#/responses?responseId=" + encodeURIComponent(value.id); link.target = "_blank"; link.rel = "noreferrer"; link.textContent = "调用记录 ↗"; container.append(link);
}
function renderNews(result) {
  $("news-result").hidden = !result;
  $("news-empty").hidden = Boolean(result);
  if (!result) return;
  markdown($("brief"), result.text);
  $("news-time").textContent = "采集于 " + new Date(result.collectedAt).toLocaleString("zh-CN");
  metadata($("news-meta"), result);
  $("sources").replaceChildren();
  for (const source of result.sources) {
    const li = document.createElement("li"), content = document.createElement("div"), a = document.createElement("a"), small = document.createElement("small");
    a.href = source.url; a.textContent = source.title; a.target = "_blank"; a.rel = "noreferrer";
    small.textContent = `${source.points} 积分 · ${source.comments} 条评论 · 提交于 ${new Date(source.submittedAt).toLocaleString("zh-CN")}`;
    content.append(a, small); li.append(content); $("sources").append(li);
  }
}
function message(value) {
  const article = document.createElement("article"); article.className = "message " + value.role;
  const label = document.createElement("div"); label.className = "message-label"; label.textContent = value.role === "user" ? "你" : "助手";
  const prose = document.createElement("div"); prose.className = "prose"; if (value.role === "assistant") markdown(prose, value.text); else prose.textContent = value.text;
  const meta = document.createElement("div"); meta.className = "request-meta"; if (value.id) metadata(meta, value);
  article.append(label, prose, meta);
  if (value.error) { const error = document.createElement("p"); error.className = "message-error"; error.textContent = value.error; article.append(error); }
  $("messages").append(article); return { prose, meta };
}
function renderMessages() {
  $("messages").replaceChildren();
  if (!state.messages.length) { $("messages").append(emptyChat.cloneNode(true)); return; }
  state.messages.forEach(message);
  $("messages").scrollTop = $("messages").scrollHeight;
}
$("generate").onclick = async () => {
  showError(); setBusy(true); $("news-progress").hidden = false; $("news-empty").hidden = true; $("news-result").hidden = true;
  try { state.news = await (await api("/api/news", {})).json(); }
  catch (error) { showError(error.message); }
  finally { $("news-progress").hidden = true; renderNews(state.news); setBusy(false); }
};
$("message").oninput = () => { $("send").disabled = busy || state?.broken || !$("message").value.trim(); };
$("messages").onclick = event => { if (event.target.id !== "suggestion") return; $("message").value = event.target.textContent; $("message").oninput(); $("message").focus(); };
$("new-chat").onclick = async () => {
  try { await api("/api/reset", {}); state.messages = []; state.broken = false; renderMessages(); setBusy(false); showError(); $("message").focus(); }
  catch (error) { showError(error.message); }
};
$("stop").onclick = async () => {
  $("stop").disabled = true;
  try { await api("/api/cancel", {}); $("chat-status").textContent = "正在停止…"; }
  catch (error) { showError(error.message); $("stop").disabled = false; }
};
$("composer").onsubmit = async event => {
  event.preventDefault(); const input = $("message").value.trim(); if (busy || !input || state.broken) return;
  showError(); $("chat-empty")?.remove(); setBusy(true, true); $("message").value = "";
  message({ role: "user", text: input }); const view = message({ role: "assistant", text: "" });
  $("messages").scrollTop = $("messages").scrollHeight;
  let complete = false;
  try {
    const response = await api("/api/chat", { input });
    const reader = response.body.pipeThrough(new TextDecoderStream()).getReader();
    let pending = "";
    while (true) {
      const { done, value } = await reader.read(); if (done) break; pending += value;
      const lines = pending.split("\n"); pending = lines.pop();
      for (const line of lines) {
        if (!line) continue;
        const item = JSON.parse(line);
        const atBottom = $("messages").scrollHeight - $("messages").scrollTop - $("messages").clientHeight < 80;
        if (item.type === "delta") view.prose.textContent += item.text;
        if (item.type === "completed") { complete = true; markdown(view.prose, item.result.text); metadata(view.meta, item.result); }
        if (item.type === "error") { metadata(view.meta, { id: item.responseId }); throw new Error(item.status === "cancelled" ? "本轮已停止。可以新建对话继续。" : item.error); }
        if (atBottom) $("messages").scrollTop = $("messages").scrollHeight;
      }
    }
    if (!complete) throw new Error("连接中断，尚未确认完成。请刷新页面查看已保存的状态。");
  } catch (error) { state.broken = true; showError(error.message); }
  finally { setBusy(false); if (!state.broken) $("message").focus(); }
};

selectTab(location.hash === "#chat" ? "chat" : "news");
try {
  state = await (await api("/api/state")).json();
  document.querySelector(".topbar > a:last-child").href = state.platformURL + "/#/docs?topic=examples&endpoint=" + encodeURIComponent(state.model);
  if (["news", "chat"].includes(state.kind)) { selectTab(state.kind); document.querySelector(".tabs").hidden = true; }
  $("endpoint").textContent = "端点 · " + state.model;
  renderNews(state.news); renderMessages(); setBusy(state.busy);
  if (state.busy) {
    const poll = setInterval(async () => {
      try { state = await (await api("/api/state")).json(); renderMessages(); renderNews(state.news); if (!state.busy) { clearInterval(poll); setBusy(false); } }
      catch (error) { clearInterval(poll); showError(error.message); }
    }, 2000);
  }
} catch (error) { showError(error.message); }
