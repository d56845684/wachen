import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, CaseFilters, CaseSummary, Facet } from "../api";
import { RiskSeal, SLACountdown, Stars, StatusPill } from "../components";

const RISKS = [
  ["", "全部風險"],
  ["high", "高風險"],
  ["medium", "中風險"],
  ["low", "低風險"],
] as const;
const STATUSES = [
  ["", "全部狀態"],
  ["open", "待處理"],
  ["in_progress", "處理中"],
  ["resolved", "已解決"],
  ["closed", "已結案"],
] as const;

const SORTS = [
  ["sla", "SLA 急迫優先"],
  ["newest", "評論最新優先"],
  ["oldest", "評論最舊優先"],
] as const;

const EMPTY: CaseFilters = { risk: "", status: "", store: "", source: "", sort: "sla" };

export default function Inbox() {
  const nav = useNavigate();
  const [filters, setFilters] = useState<CaseFilters>(EMPTY);
  const [cases, setCases] = useState<CaseSummary[] | null>(null);
  const [stores, setStores] = useState<Facet[]>([]);
  const [sources, setSources] = useState<Facet[]>([]);

  // facets 只在載入時抓一次（門市/來源清單相對穩定）
  useEffect(() => {
    api.facets().then((f) => {
      setStores(f.stores);
      setSources(f.sources);
    });
  }, []);

  useEffect(() => {
    let live = true;
    const fetch = () => api.listCases(filters).then((r) => live && setCases(r.cases));
    fetch();
    const t = setInterval(fetch, 30_000); // 半即時：30s 輪詢（PoC）
    return () => {
      live = false;
      clearInterval(t);
    };
  }, [filters]);

  const set = (patch: Partial<CaseFilters>) => setFilters((f) => ({ ...f, ...patch }));
  const active =
    filters.risk || filters.status || filters.store || filters.source || filters.sort !== "sla";

  return (
    <div className="page">
      <div className="filterbar">
        <div className="fb-group">
          <span className="fb-label">風險</span>
          <div className="chips">
            {RISKS.map(([v, label]) => (
              <button key={v} className={`chip${filters.risk === v ? " on" : ""}`} onClick={() => set({ risk: v })}>
                {label}
              </button>
            ))}
          </div>
        </div>

        <div className="fb-group">
          <span className="fb-label">狀態</span>
          <div className="chips">
            {STATUSES.map(([v, label]) => (
              <button key={v} className={`chip${filters.status === v ? " on" : ""}`} onClick={() => set({ status: v })}>
                {label}
              </button>
            ))}
          </div>
        </div>

        <div className="fb-row">
          <label className="fb-field">
            <span className="fb-label">門市</span>
            <select className="select" value={filters.store} onChange={(e) => set({ store: e.target.value })}>
              <option value="">全部門市</option>
              {stores.map((s) => (
                <option key={s.value} value={s.value}>
                  {s.label}（{s.count}）
                </option>
              ))}
            </select>
          </label>
          <label className="fb-field">
            <span className="fb-label">來源</span>
            <select className="select" value={filters.source} onChange={(e) => set({ source: e.target.value })}>
              <option value="">全部來源</option>
              {sources.map((s) => (
                <option key={s.value} value={s.value}>
                  {s.label}（{s.count}）
                </option>
              ))}
            </select>
          </label>
          <label className="fb-field">
            <span className="fb-label">排序</span>
            <select className="select" value={filters.sort} onChange={(e) => set({ sort: e.target.value })}>
              {SORTS.map(([v, label]) => (
                <option key={v} value={v}>{label}</option>
              ))}
            </select>
          </label>
          <div className="fb-spacer" />
          {active && (
            <button className="chip clear" onClick={() => setFilters(EMPTY)}>
              清除篩選 ✕
            </button>
          )}
          {cases && <span className="fb-count">{cases.length} 件</span>}
        </div>
      </div>

      {cases === null ? (
        <div className="loading">載入中…</div>
      ) : cases.length === 0 ? (
        <div className="empty">目前沒有符合條件的案件 — 哨站無事</div>
      ) : (
        <div className="case-list">
          {cases.map((c, i) => (
            <article
              key={c.id}
              className={`case-card ${c.risk_level}`}
              style={{ animationDelay: `${Math.min(i, 8) * 40}ms` }}
              onClick={() => nav(`/cases/${c.id}`)}
            >
              <RiskSeal risk={c.risk_level} />
              <div className="case-main">
                <div className="case-meta">
                  <strong>{c.store_name || "未對映門市"}</strong>
                  <span>{c.source_name}</span>
                  <Stars rating={c.rating} />
                  {c.posted_at && <span>{new Date(c.posted_at).toLocaleDateString("zh-TW")}</span>}
                </div>
                <div className="case-summary">{c.summary}</div>
                <div className="tags">
                  {c.categories.map((cat) => (
                    <span key={cat} className="tag">{cat}</span>
                  ))}
                  {c.reopened_count > 0 && (
                    <span className="tag reopen">回開 ×{c.reopened_count}</span>
                  )}
                  <a
                    className="tag"
                    href={c.source_url}
                    target="_blank"
                    rel="noreferrer"
                    onClick={(e) => e.stopPropagation()}
                  >
                    原始留言 ↗
                  </a>
                </div>
              </div>
              <div className="case-side">
                <StatusPill status={c.status} />
                <SLACountdown
                  dueAt={c.sla_due_at}
                  active={c.status === "open" || c.status === "in_progress"}
                />
              </div>
            </article>
          ))}
        </div>
      )}
    </div>
  );
}
