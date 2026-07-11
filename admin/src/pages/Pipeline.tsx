import { useEffect, useState } from "react";
import { api, PipelineStats } from "../api";
import { RiskSeal } from "../components";

const RISK_LABEL: Record<string, string> = { high: "高風險", medium: "中風險", low: "低風險" };
const SENTIMENT_LABEL: Record<string, string> = {
  negative: "負面", neutral: "中性", positive: "正面",
};

export default function Pipeline() {
  const [p, setP] = useState<PipelineStats | null>(null);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    let live = true;
    const fetch = () => api.pipeline().then((r) => live && setP(r));
    fetch();
    const t = setInterval(() => {
      setTick((n) => n + 1);
      fetch();
    }, 5000); // AI 進度：5s 輪詢，比收件匣密（要看即時處理）
    return () => {
      live = false;
      clearInterval(t);
    };
  }, []);

  if (!p) return <div className="page"><div className="loading">載入中…</div></div>;

  const f = p.funnel;
  // 漏斗每階段，附「積壓」提示
  const stages = [
    { key: "raw", label: "原始擷取", value: f.raw_reviews, note: "raw_reviews" },
    { key: "review", label: "已正規化", value: f.reviews, note: "reviews" },
    { key: "await_ai", label: "待 AI 分析", value: f.awaiting_analysis, note: "status=new", warn: f.awaiting_analysis > 0 },
    { key: "analyzed", label: "已分析", value: f.analyzed, note: "is_current" },
    { key: "await_route", label: "待分流", value: f.awaiting_routing, note: "analyzed 未建案", warn: f.awaiting_routing > 0 },
    { key: "cased", label: "已建案", value: f.cased, note: "cases" },
  ];
  const maxV = Math.max(...stages.map((s) => s.value), 1);
  const totalRisk = p.risk.reduce((a, r) => a + r.count, 0) || 1;

  return (
    <div className="page">
      <div className="pl-head">
        <h2>AI 處理進度</h2>
        <span className="pl-live">● 每 5 秒更新<span className="pl-dot" key={tick} /></span>
      </div>

      {/* 漏斗 */}
      <section className="panel">
        <h3>處理管線漏斗</h3>
        <div className="funnel">
          {stages.map((s) => (
            <div className="fn-row" key={s.key}>
              <span className="fn-label">{s.label}</span>
              <div className="fn-bar-wrap">
                <div
                  className={`fn-bar${s.warn ? " warn" : ""}`}
                  style={{ width: `${Math.max(2, (s.value / maxV) * 100)}%` }}
                />
              </div>
              <span className={`fn-val${s.warn ? " warn" : ""}`}>{s.value}</span>
              <span className="fn-note">{s.note}</span>
            </div>
          ))}
        </div>
      </section>

      <div className="grid2">
        {/* AI 指標 */}
        <section className="panel">
          <h3>AI 引擎</h3>
          <div className="stat-grid">
            <Stat label="供應商" value={p.ai.models.join(" / ") || "—"} />
            <Stat label="現行分析數" value={p.ai.total_analyses} />
            <Stat label="平均延遲" value={`${p.ai.avg_latency_ms} ms`} />
            <Stat label="最大延遲" value={`${p.ai.max_latency_ms} ms`} />
            <Stat label="近 5 分鐘" value={p.ai.last_5min} />
            <Stat label="近 1 小時" value={p.ai.last_hour} />
            <Stat label="降級 heuristic" value={p.ai.fallback_count} warn={p.ai.fallback_count > 0} />
            <Stat label="隔離毒藥" value={p.ai.quarantine_count} warn={p.ai.quarantine_count > 0} />
          </div>
        </section>

        {/* 風險 + 情緒分布 */}
        <section className="panel">
          <h3>風險分布（現行分析）</h3>
          <div className="risk-dist">
            {p.risk.map((r) => (
              <div className="rd-row" key={r.value}>
                <RiskSeal risk={r.value} small />
                <span className="rd-label">{RISK_LABEL[r.value] ?? r.value}</span>
                <div className="rd-bar-wrap">
                  <div className={`rd-bar ${r.value}`} style={{ width: `${(r.count / totalRisk) * 100}%` }} />
                </div>
                <span className="rd-val">{r.count}</span>
              </div>
            ))}
          </div>
          <div className="kv" style={{ marginTop: 14 }}>
            {p.sentiment.map((s) => (
              <span key={s.value} className="tag">
                {SENTIMENT_LABEL[s.value] ?? s.value} {s.count}
              </span>
            ))}
          </div>
        </section>
      </div>

      {/* 最近分析 */}
      <section className="panel">
        <h3>最近分析（15 筆）</h3>
        {p.recent.length === 0 && <div className="byline">尚無分析</div>}
        <div className="recent">
          {p.recent.map((r) => (
            <div className="rec-row" key={r.review_id + r.created_at}>
              <RiskSeal risk={r.risk_level} small />
              <div className="rec-main">
                <div className="rec-meta">
                  <strong>{r.store_name || "未對映門市"}</strong>
                  <span>{r.source_name}</span>
                  <span className="rec-model">{r.model_name}{r.fallback && " 降級"}</span>
                  {r.latency_ms != null && <span className="rec-lat">{r.latency_ms}ms</span>}
                  <span className="rec-time">{new Date(r.created_at).toLocaleTimeString("zh-TW")}</span>
                </div>
                <div className="rec-summary">{r.summary || "（無摘要）"}</div>
              </div>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}

function Stat({ label, value, warn }: { label: string; value: string | number; warn?: boolean }) {
  return (
    <div className="stat">
      <div className={`stat-val${warn ? " warn" : ""}`}>{value}</div>
      <div className="stat-label">{label}</div>
    </div>
  );
}
