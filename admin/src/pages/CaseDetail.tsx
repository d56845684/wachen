import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api, CaseDetail as Detail } from "../api";
import { RiskSeal, SLACountdown, Stars, StatusPill, statusLabel } from "../components";

// 與後端 validTransitions 對齊的動作表
const ACTIONS: Record<string, [string, string][]> = {
  open: [["in_progress", "開始處理"], ["resolved", "標記解決"]],
  in_progress: [["resolved", "標記解決"], ["open", "退回待處理"]],
  resolved: [["closed", "結案"], ["in_progress", "重新處理"]],
  closed: [],
};

export default function CaseDetailPage() {
  const { id = "" } = useParams();
  const [d, setD] = useState<Detail | null>(null);
  const [err, setErr] = useState("");

  const load = useCallback(() => {
    api.caseDetail(id).then(setD).catch((e) => setErr(String(e)));
  }, [id]);
  useEffect(load, [load]);

  async function act(status: string) {
    setErr("");
    try {
      await api.updateStatus(id, status);
      load();
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "操作失敗");
    }
  }

  if (!d) return <div className="page"><div className="loading">{err || "載入中…"}</div></div>;

  const score = d.sentiment_score ?? 0;
  const negative = score < 0;
  const width = Math.min(50, Math.abs(score) * 50);

  return (
    <div className="page">
      <Link className="back" to="/">← 返回收件匣</Link>

      <div className="detail-head">
        <RiskSeal risk={d.risk_level} />
        <div>
          <h2>{d.store_name || "未對映門市"}</h2>
          <div className="case-meta">
            <span>{d.source_name}</span>
            <StatusPill status={d.status} />
            <SLACountdown
              dueAt={d.sla_due_at}
              active={d.status === "open" || d.status === "in_progress"}
            />
            {d.reopened_count > 0 && <span className="tag reopen">回開 ×{d.reopened_count}</span>}
          </div>
        </div>
        <div className="actions">
          {ACTIONS[d.status]?.map(([to, label]) => (
            <button key={to} className="btn-action" onClick={() => act(to)}>
              {label}
            </button>
          ))}
        </div>
      </div>
      {err && <div className="login-err">{err}</div>}

      <div className="grid2">
        <div>
          <section className="panel">
            <h3>顧客留言</h3>
            <blockquote className="review">{d.review_content || "（純星等，無文字）"}</blockquote>
            <div className="byline">
              {d.author_name || "匿名"} · <Stars rating={d.rating} />
              {d.posted_at && ` · ${new Date(d.posted_at).toLocaleString("zh-TW")}`}
              {" · "}
              <a href={d.source_url} target="_blank" rel="noreferrer">原始留言 ↗</a>
            </div>
          </section>

          <section className="panel">
            <h3>通知紀錄</h3>
            {d.notifications.length === 0 && <div className="byline">尚無通知</div>}
            {d.notifications.map((n, i) => (
              <div className="notif" key={i}>
                <span className={`st ${n.status}`}>{n.status.toUpperCase()}</span>
                <span className="who">{n.recipient.replace("role:", "")}</span>
                <span>{n.subject}</span>
              </div>
            ))}
          </section>
        </div>

        <div>
          <section className="panel">
            <h3>AI 分析（{statusLabel(d.status)}中的依據）</h3>
            <div className="byline">情緒 {d.sentiment} · 分數 <span className="score-num">{score.toFixed(2)}</span></div>
            <div className="score-bar">
              <span className="mid" />
              <span
                className="fill"
                style={{
                  left: negative ? `${50 - width}%` : "50%",
                  width: `${width}%`,
                  background: negative ? "var(--vermilion)" : "var(--jade)",
                }}
              />
            </div>
            <div className="kv" style={{ margin: "12px 0" }}>
              {d.categories.map((c) => <span key={c} className="tag">{c}</span>)}
              {d.keywords.map((k) => <span key={k} className="tag">🔑 {k}</span>)}
            </div>
            {d.risk_reasons.length > 0 && (
              <ul className="reasons">
                {d.risk_reasons.map((r, i) => <li key={i}>{r}</li>)}
              </ul>
            )}
            <div className="trace">
              model={d.model_name} · prompt={d.prompt_version} · case={d.id.slice(0, 8)}
            </div>
          </section>

          <section className="panel">
            <h3>指派</h3>
            <div className="kv">
              {d.assignments.map((a) => <span key={a} className="tag">{a}</span>)}
            </div>
          </section>
        </div>
      </div>
    </div>
  );
}
