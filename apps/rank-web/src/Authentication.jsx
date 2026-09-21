import { createContext, useContext, useEffect, useRef, useState } from "react";
import { initialLocale } from "./i18n.js";
import "./authentication.css";

const AccountContext = createContext(null);
const copy = {
  zh: { title: "登录 Rank", subtitle: "管理 Agent，评测并发布。", username: "账号", password: "密码", login: "登录", busy: "正在登录…", checking: "正在连接 Rank…", unavailable: "暂时无法连接服务，请稍后重试。", invalid: "账号或密码不正确，请重试。", expired: "登录已过期，请重新登录。", retry: "重试", account: "当前账号", logout: "退出登录", leaving: "正在退出…" },
  en: { title: "Sign in to Rank", subtitle: "Manage, evaluate and publish your Agents.", username: "Username", password: "Password", login: "Sign in", busy: "Signing in…", checking: "Connecting to Rank…", unavailable: "Unable to connect. Please try again shortly.", invalid: "Incorrect username or password. Please try again.", expired: "Your session expired. Please sign in again.", retry: "Try again", account: "Account", logout: "Sign out", leaving: "Signing out…" },
};

async function authRequest(path, options = {}) {
  const response = await fetch(path, { credentials: "same-origin", cache: "no-store", signal: AbortSignal.timeout(10000), ...options });
  if (!response.ok) {
    const error = new Error("Authentication request failed");
    error.status = response.status;
    throw error;
  }
  return response.status === 204 ? null : response.json();
}

/** Business views mount only after identity is verified; secrets never enter browser storage. */
export function Authentication({ children }) {
  const passwordInput = useRef(null);
  const [account, setAccount] = useState(null);
  const [checking, setChecking] = useState(true);
  const [error, setError] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const text = copy[initialLocale()] || copy.zh;

  useEffect(() => {
    const controller = new AbortController();
    setChecking(true);
    authRequest("/api/identity", { signal: AbortSignal.any([controller.signal, AbortSignal.timeout(10000)]) })
      .then(value => { if (!controller.signal.aborted) { setAccount(value); setError(""); } })
      .catch(failure => { if (!controller.signal.aborted) setError(failure.status === 401 ? "" : "unavailable"); })
      .finally(() => { if (!controller.signal.aborted) setChecking(false); });
    return () => controller.abort();
  }, [attempt]);

  useEffect(() => {
    if (!account || account.role === "local-development") return;
    let active = true;
    const expire = () => { if (active) { setAccount(null); setPassword(""); setError("expired"); } };
    const verify = () => {
      authRequest("/api/identity").catch(failure => { if (failure.status === 401) expire(); });
    };
    const visibility = () => { if (document.visibilityState === "visible") verify(); };
    window.addEventListener("rank:unauthorized", expire);
    window.addEventListener("focus", verify);
    document.addEventListener("visibilitychange", visibility);
    const interval = setInterval(verify, 60000);
    return () => {
      active = false;
      clearInterval(interval);
      window.removeEventListener("rank:unauthorized", expire);
      window.removeEventListener("focus", verify);
      document.removeEventListener("visibilitychange", visibility);
    };
  }, [account]);

  async function login(event) {
    event.preventDefault();
    setBusy(true); setError("");
    try {
      const value = await authRequest("/api/auth/login", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ username: username.trim(), password }) });
      setPassword(""); setAccount(value);
    } catch (failure) {
      setPassword(""); setError(failure.status === 401 ? "invalid" : "unavailable");
      passwordInput.current?.focus();
    } finally { setBusy(false); }
  }

  async function logout() {
    await authRequest("/api/auth/logout", { method: "POST", headers: { "content-type": "application/json" }, body: "{}" });
    setAccount(null); setPassword(""); setError("");
  }

  if (account) return <AccountContext.Provider value={{ account, logout }}>{children}</AccountContext.Provider>;
  return <main className="rank-auth">
    <div className="rank-auth-content">
      <a className="rank-auth-brand" href="#/agent" aria-label="Rank">rank<span aria-hidden="true">.</span></a>
      {checking ? <p role="status">{text.checking}</p> : <>
        <h1>{text.title}</h1>
        <p className="rank-auth-subtitle">{text.subtitle}</p>
        <form onSubmit={login} aria-label={text.title}>
          <label htmlFor="rank-username">{text.username}</label>
          <input id="rank-username" name="username" autoComplete="username" autoCapitalize="none" spellCheck={false} required maxLength={128} autoFocus value={username} onChange={event => setUsername(event.target.value)} readOnly={busy} />
          <label htmlFor="rank-password">{text.password}</label>
          <input ref={passwordInput} id="rank-password" name="password" type="password" autoComplete="current-password" required maxLength={512} value={password} onChange={event => setPassword(event.target.value)} readOnly={busy} aria-describedby={error ? "rank-login-error" : undefined} />
          {error && <p id="rank-login-error" className="rank-auth-error" role="alert">{text[error]}</p>}
          <button className="rank-auth-submit" disabled={busy || !username.trim() || !password}>{busy ? text.busy : text.login}</button>
          {error === "unavailable" && <button className="rank-auth-retry" type="button" onClick={() => setAttempt(value => value + 1)}>{text.retry}</button>}
        </form>
      </>}
    </div>
  </main>;
}

/** Authenticated accounts can explicitly invalidate this browser's server session. */
export function AccountSettings({ locale }) {
  const session = useContext(AccountContext);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const text = copy[locale] || copy.zh;
  if (!session || session.account.role === "local-development") return null;
  return <section className="management-section rank-account">
    <div><h2>{text.account}</h2><p>{session.account.subject}</p></div>
    <button type="button" className="secondary-action" disabled={busy} onClick={async () => {
      setBusy(true); setFailed(false);
      try { await session.logout(); } catch { setFailed(true); } finally { setBusy(false); }
    }}>{busy ? text.leaving : text.logout}</button>
    {failed && <p role="alert">{text.unavailable}</p>}
  </section>;
}
