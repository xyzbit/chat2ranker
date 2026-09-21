import { useState } from "react";
import { DownloadSimple } from "@phosphor-icons/react";

/** Downloads endpoint-specific examples without adding key plaintext to the archive. */
export function ExampleDownloads({ endpoint, locale }) {
  const [pending, setPending] = useState("");
  const [error, setError] = useState("");
  const en = locale === "en";
  async function download(kind) {
    setPending(kind); setError("");
    try {
      const { exampleBundle } = await import("./example-bundle.js");
      const bundle = exampleBundle(kind, endpoint, location.origin);
      const url = URL.createObjectURL(new Blob([bundle.bytes], { type: "application/zip" }));
      const a = document.createElement("a"); a.href = url; a.download = bundle.name; a.click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (e) { setError(e.message); }
    finally { setPending(""); }
  }
  return <div className="example-downloads">
    <div className="management-actions">{["news", "chat"].map(kind => <button className="secondary-action" key={kind} disabled={Boolean(pending)} onClick={() => download(kind)}><DownloadSimple size={16}/>{pending === kind ? (en ? "Preparing..." : "正在打包…") : kind === "news" ? (en ? "Download headlines demo" : "下载每日热点") : (en ? "Download chat demo" : "下载聊天助手")}</button>)}</div>
    <p>{en ? `Node.js 22.19+. Endpoint: ${endpoint}. Extract the ZIP and follow README. Add your key to the local .env file.` : `Node.js 22.19+。端点：${endpoint}。解压后按 README 启动，在本地 .env 填写密钥。`}</p>
    {error && <p role="alert">{error}</p>}
  </div>;
}
