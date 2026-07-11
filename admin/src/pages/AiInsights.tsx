/** AI 洞察中心 — 對應 PAGES.ai */
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { DB, useApp } from "../lib/db";
import { AlertRow, PageHeader, SectionT } from "../components/ui";

export default function AiInsights() {
  useApp();
  const nav = useNavigate();
  const I = DB.insights;
  const [q, setQ] = useState("");
  const [answers, setAnswers] = useState<[string, string][]>(I.qa);

  const ask = () => {
    const query = q.trim();
    if (!query) return;
    const hit = I.qa.find((x) => query.includes(x[0].slice(0, 4)) || x[0].includes(query.slice(0, 4)));
    const ans = hit ? hit[1] : "（POC 示範）此問題正式版將由 LLM 即時查詢資料後回答。目前可試問清單中的預設問題。";
    setAnswers((prev) => [[query, ans], ...prev]);
    setQ("");
  };

  return (
    <>
      <PageHeader title="AI 洞察中心" sub="讓 AI 主動找出人員未必注意到的趨勢 · 異常偵測 · 根因分析 · 改善建議" />
      <SectionT>異常偵測</SectionT>
      <div className="alist">
        {I.anomalies.map((a, i) => (
          <AlertRow
            key={i}
            level={a.sev ?? a.level}
            title={a.t ?? a.title ?? ""}
            body={a.d ?? a.body ?? ""}
            actions={
              <>
                <button className="btn sm" onClick={() => nav("/causes")}>查看分析</button>
                <button className="btn sm" onClick={() => nav("/tasks")}>建立改善任務</button>
              </>
            }
          />
        ))}
      </div>
      <SectionT>根因分析</SectionT>
      <div className="card"><p style={{ margin: 0, fontSize: 14, lineHeight: 1.8 }}>{I.rootcause}</p></div>
      <SectionT>改善建議</SectionT>
      <div className="card">
        <ul className="reasons" style={{ fontSize: 14 }}>{I.suggestions.map((s) => <li key={s}>{s}</li>)}</ul>
        <div style={{ display: "flex", gap: 8, marginTop: 12 }}>
          <button className="btn pri sm" onClick={() => nav("/tasks")}>一鍵建立改善任務</button>
        </div>
      </div>
      <SectionT>AI 問答</SectionT>
      <div className="card">
        <div style={{ display: "flex", gap: 8, marginBottom: 12 }}>
          <input
            style={{ flex: 1, font: "inherit", padding: "9px 12px", borderRadius: 10, border: "1px solid var(--border)", background: "var(--surface-2)", color: "var(--ink)" }}
            placeholder="問問看：本週哪三家門市最需要注意？"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") ask(); }}
            list="ai-suggests"
          />
          <datalist id="ai-suggests">{I.qa.map(([question]) => <option key={question} value={question} />)}</datalist>
          <button className="btn pri" onClick={ask}>詢問</button>
        </div>
        <div className="alist">
          {answers.map(([question, answer], i) => (
            <AlertRow key={`${question}-${i}`} level="good" title={question} body={answer} />
          ))}
        </div>
        <div className="note" style={{ marginTop: 10 }}>POC 示範：以上為預先計算之回答；正式版將接入 LLM 即時查詢資料。</div>
      </div>
    </>
  );
}
