import { FormEvent, useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api, CaseDetail as Detail } from "../api";
import { RiskSeal, SLACountdown, Stars, StatusPill, statusLabel } from "../components";

const REPLY_STATUS: Record<string, string> = {
  draft: "草稿",
  pending_approval: "待審核",
  approved: "已核准",
  sending: "送出中",
  sent: "已送出",
  rejected: "已退回",
  failed: "送出失敗",
};

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
  const [replyText, setReplyText] = useState("");
  const [sending, setSending] = useState(false);

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

  async function submitReply(e: FormEvent) {
    e.preventDefault();
    if (!replyText.trim()) return;
    setSending(true);
    setErr("");
    try {
      await api.createReply(id, replyText.trim());
      setReplyText("");
      load();
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "送出失敗");
    } finally {
      setSending(false);
    }
  }

  if (!d) return <div className="page"><div className="loading">{err || "載入中…"}</div></div>;

  const score = d.sentiment_score ?? 0;
  const negative = score < 0;
  const width = Math.min(50, Math.abs(score) * 50);

  return (
    <div className="page">
      <Link className="back" to="/inbox">← 返回即時案件</Link>

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
            <h3>回覆留言</h3>
            {d.replies.length > 0 && (
              <div className="reply-list">
                {d.replies.map((rp) => (
                  <div className={`reply-item ${rp.status}`} key={rp.id}>
                    <div className="reply-top">
                      <span className={`reply-st ${rp.status}`}>{REPLY_STATUS[rp.status] ?? rp.status}</span>
                      <span className="reply-by">{rp.created_by.replace("svc:", "")}</span>
                      {rp.reply_url && <a href={rp.reply_url} target="_blank" rel="noreferrer">已發佈 ↗</a>}
                    </div>
                    <div className="reply-body">{rp.content}</div>
                    {rp.error && <div className="reply-err">{rp.error}</div>}
                  </div>
                ))}
              </div>
            )}
            {d.can_reply ? (
              <form className="reply-form" onSubmit={submitReply}>
                <textarea
                  value={replyText}
                  onChange={(e) => setReplyText(e.target.value)}
                  placeholder={
                    d.risk_level === "high"
                      ? "撰寫回覆…（高風險案件送出後需公關/法務審核）"
                      : "撰寫回覆…"
                  }
                  rows={3}
                />
                <div className="reply-actions">
                  {d.risk_level === "high" && <span className="reply-hint">高風險：送出後進審核佇列</span>}
                  <button className="btn-action send" disabled={sending || !replyText.trim()}>
                    {sending ? "送出中…" : d.risk_level === "high" ? "送審" : "送出回覆"}
                  </button>
                </div>
              </form>
            ) : (
              <div className="byline">此來源不支援直接回覆（唯讀）</div>
            )}
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
              {d.keywords.map((k) => <span key={k} className="tag">{k}</span>)}
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
