import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api, PendingApproval } from "../api";
import { RiskSeal } from "../components";

export default function Approvals() {
  const [list, setList] = useState<PendingApproval[] | null>(null);
  const [err, setErr] = useState("");

  const load = useCallback(() => {
    api.approvals().then((r) => setList(r.replies)).catch((e) => setErr(String(e)));
  }, []);
  useEffect(() => {
    load();
    const t = setInterval(load, 15_000);
    return () => clearInterval(t);
  }, [load]);

  async function decide(id: string, action: "approve" | "reject") {
    setErr("");
    try {
      if (action === "approve") await api.approveReply(id);
      else await api.rejectReply(id, "審核退回");
      load();
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "操作失敗");
    }
  }

  return (
    <div className="page">
      <div className="pl-head">
        <h2>回覆審核佇列</h2>
        {list && <span className="fb-count">{list.length} 件待審</span>}
      </div>
      {err && <div className="login-err">{err}</div>}

      {list === null ? (
        <div className="loading">載入中…</div>
      ) : list.length === 0 ? (
        <div className="empty">沒有待審回覆 — 高風險案件的回覆會在此把關</div>
      ) : (
        <div className="case-list">
          {list.map((p) => (
            <article className={`case-card ${p.risk_level}`} key={p.id}>
              <RiskSeal risk={p.risk_level} />
              <div className="case-main">
                <div className="case-meta">
                  <strong>{p.store_name}</strong>
                  <Link to={`/cases/${p.case_id}`}>查看案件 →</Link>
                </div>
                <div className="byline" style={{ marginBottom: 8 }}>{p.summary}</div>
                <blockquote className="review" style={{ fontSize: 15 }}>{p.content}</blockquote>
              </div>
              <div className="case-side">
                <button className="btn-action send" onClick={() => decide(p.id, "approve")}>核准送出</button>
                <button className="btn-action reject" onClick={() => decide(p.id, "reject")}>退回</button>
              </div>
            </article>
          ))}
        </div>
      )}
    </div>
  );
}
