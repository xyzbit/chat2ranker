import { useMemo, useState } from "react";
import { ScrollRegion } from "./ScrollRegion.jsx";
import {
  ChatsCircle,
  ClockCounterClockwise,
  Cube,
  Gear,
  Key,
  MagnifyingGlass,
  NotePencil,
  Stack,
  Translate,
  UserCircle,
} from "@phosphor-icons/react";

const pageNavigation = [
  ["agents", UserCircle, "agents"],
  ["deployments", Cube, "deployments"],
  ["responses", ChatsCircle, "responses"],
  ["credentials", Key, "credentials"],
];

const mobileNavigation = [
  ["new", NotePencil, "newConversation"],
  ...pageNavigation,
  ["settings", Gear, "settings"],
];

function conversationTitle(item, t) {
  return !item?.title || item.title === "未命名实验" ? t("untitledConversation") : item.title;
}

function LanguageControl({ onChange, t }) {
  return <button className="locale-control" type="button" onClick={onChange} aria-label={t("switchLanguage")} title={t("switchLanguage")}><Translate size={19} aria-hidden="true" /></button>;
}

export function WorkspaceShell({
  route,
  onNavigate,
  locale,
  onLocaleChange,
  experiments,
  activeExperimentId,
  onOpenConversation,
  onNewConversation,
  onOpenHistory,
  t,
  children,
}) {
  const [query, setQuery] = useState("");
  const [searchOpen, setSearchOpen] = useState(false);
  const filteredExperiments = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase(locale === "en" ? "en-US" : "zh-CN");
    if (!normalized) return experiments;
    return experiments.filter((item) => conversationTitle(item, t)
      .toLocaleLowerCase(locale === "en" ? "en-US" : "zh-CN")
      .includes(normalized));
  }, [experiments, locale, query, t]);

  function startConversation() {
    setQuery("");
    setSearchOpen(false);
    onNavigate("agent");
    void onNewConversation();
  }

  function openConversation(id) {
    onNavigate("agent");
    void onOpenConversation(id);
  }

  return (
    <div className="product-shell">
      <aside className="product-sidebar" aria-label={t("primaryNavigation")}>
        <header className="sidebar-header">
          <button className="product-brand" type="button" onClick={() => onNavigate("agent")} aria-label="AgentKiln">AgentKiln</button>
          <button
            className={searchOpen ? "sidebar-search-toggle active" : "sidebar-search-toggle"}
            type="button"
            onClick={() => { setSearchOpen((value) => !value); setQuery(""); }}
            aria-label={t("searchConversations")}
            aria-expanded={searchOpen}
            title={t("searchConversations")}
          >
            <MagnifyingGlass size={19} />
          </button>
        </header>

        <nav className="sidebar-navigation" aria-label={t("pages")}>
          <button className="new-conversation-action" type="button" onClick={startConversation}>
            <NotePencil size={19} />
            <span>{t("newConversation")}</span>
          </button>
          {pageNavigation.map(([key, Icon, label]) => (
            <button
              key={key}
              type="button"
              className={route === key ? "active" : ""}
              aria-current={route === key ? "page" : undefined}
              onClick={() => onNavigate(key)}
              title={t(label)}
            >
              <Icon size={19} weight={route === key ? "fill" : "regular"} />
              <span>{t(label)}</span>
            </button>
          ))}
        </nav>

        <section className="recent-conversations" aria-label={t("recentConversations")}>
          <div className="recent-heading"><h2>{t("recent")}</h2></div>
          {searchOpen && <label className="conversation-search">
            <MagnifyingGlass size={15} />
            <span className="sr-only">{t("searchConversations")}</span>
            <input autoFocus type="search" value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t("searchConversations")} />
          </label>}
          <ScrollRegion className="conversation-list" aria-label={t("recentConversations")}>
            {filteredExperiments.map((item) => (
              <button
                key={item.id}
                type="button"
                className={route === "agent" && item.id === activeExperimentId ? "active" : ""}
                aria-current={route === "agent" && item.id === activeExperimentId ? "page" : undefined}
                onClick={() => openConversation(item.id)}
                title={conversationTitle(item, t)}
              >
                <strong>{conversationTitle(item, t)}</strong>
              </button>
            ))}
            {!filteredExperiments.length && <p>{query ? t("noConversationsMatch") : t("noConversations")}</p>}
          </ScrollRegion>
        </section>

        <footer className="sidebar-footer">
          <button className={route === "settings" ? "sidebar-settings active" : "sidebar-settings"} type="button" onClick={() => onNavigate("settings")}>
            <Gear size={19} weight={route === "settings" ? "fill" : "regular"} />
            <span>{t("settings")}</span>
          </button>
          <div className="sidebar-account">
            <span className="product-avatar" aria-label="Z">Z</span>
            <span><strong>Local</strong><small><Stack size={12} />{t("localEnvironment")}</small></span>
            <LanguageControl onChange={onLocaleChange} t={t} />
          </div>
        </footer>
      </aside>

      <header className="product-topbar">
        <button className="product-brand" type="button" onClick={() => onNavigate("agent")} aria-label="AgentKiln">AgentKiln</button>
        <div className="product-topbar-actions">
          <button className="mobile-history-action" type="button" onClick={onOpenHistory} aria-label={t("recentConversations")} title={t("recentConversations")}><ClockCounterClockwise size={20} /></button>
          <LanguageControl onChange={onLocaleChange} t={t} />
          <span className="product-avatar" aria-label="Z">Z</span>
        </div>
      </header>

      <nav className="mobile-navigation" aria-label={t("primaryNavigation")}>
        {mobileNavigation.map(([key, Icon, label]) => (
          <button
            key={key}
            type="button"
            className={key !== "new" && route === key ? "active" : ""}
            aria-current={key !== "new" && route === key ? "page" : undefined}
            onClick={() => key === "new" ? startConversation() : onNavigate(key)}
            aria-label={t(label)}
            title={t(label)}
          >
            <Icon size={21} weight={key !== "new" && route === key ? "fill" : "regular"} />
          </button>
        ))}
      </nav>

      <div className="product-content">{children}</div>
    </div>
  );
}

export function PlaceholderPage({ title, t, onBack }) {
  return (
    <main className="placeholder-page">
      <span className="placeholder-icon"><Stack size={24} /></span>
      <h1>{title}</h1>
      <p>{t("pageComingSoon")}</p>
      <button type="button" onClick={onBack}>{t("backToConversation")}</button>
    </main>
  );
}
