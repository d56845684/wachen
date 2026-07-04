import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, CaseSummary } from "../api";
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

export default function Inbox() {
  const nav = useNavigate();
  const [risk, setRisk] = useState("");
  const [status, setStatus] = useState("");
  const [cases, setCases] = useState<CaseSummary[] | null>(null);

  useEffect(() => {
    let live = true;
    api.listCases(risk, status).then((r) => live && setCases(r.cases));
    const t = setInterval(
      () => api.listCases(risk, status).then((r) => live && setCases(r.cases)),
      30_000, // 收件匣半即時：30s 輪詢（PoC；正式版換 SSE/WebSocket）
    );
    return () => {
      live = false;
      clearInterval(t);
    };
  }, [risk, status]);

  return (
    <div className="page">
      <div className="filters">
        {RISKS.map(([v, label]) => (
          <button key={v} className={`chip${risk === v ? " on" : ""}`} onClick={() => setRisk(v)}>
            {label}
          </button>
        ))}
        <div className="sep" />
        {STATUSES.map(([v, label]) => (
          <button key={v} className={`chip${status === v ? " on" : ""}`} onClick={() => setStatus(v)}>
            {label}
          </button>
        ))}
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
