/** 規則設定 — 對應 PAGES.rules（需總部權限） */
import { DB, useApp } from "../lib/db";
import { getRole } from "../lib/roles";
import { PageHeader, RiskBadge, SectionT } from "../components/ui";

export default function Rules() {
  useApp();
  const role = getRole();
  if (!role.rules) {
    return <div className="empty">此頁需要「總部管理員／超級管理員」權限。目前角色為 {role.title}。</div>;
  }
  const R = DB.rules;
  return (
    <>
      <PageHeader title="規則設定" sub="AI 分類規則 · 案件派工規則 · SLA 與預警規則" />
      <SectionT>案件派工中心 · 分派規則</SectionT>
      <div className="tbl-wrap">
        <table style={{ minWidth: 0 }}>
          <thead>
            <tr><th>觸發條件</th><th>分派對象</th><th className="num">SLA</th><th>通知</th><th>升級</th><th>啟用</th></tr>
          </thead>
          <tbody>
            {R.dispatch.map((d, i) => (
              <tr key={i} style={{ cursor: "default" }}>
                <td style={{ whiteSpace: "normal", maxWidth: 220 }}>{d.cond}</td>
                <td style={{ whiteSpace: "normal" }}>{d.target}</td>
                <td className="num">{d.sla}</td>
                <td>{d.notify}</td>
                <td style={{ whiteSpace: "normal", maxWidth: 150 }}>{d.escalate}</td>
                <td><RiskBadge level={d.on ? "low" : "medium"} label={d.on ? "啟用" : "停用"} /></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <SectionT>SLA 與預警規則</SectionT>
      <div className="tbl-wrap">
        <table style={{ minWidth: 0 }}>
          <thead>
            <tr><th>風險等級</th><th>首次回應</th><th>完成處理</th><th>提醒時間</th><th>通知角色</th><th>計算規則</th></tr>
          </thead>
          <tbody>
            {R.sla.map((s, i) => (
              <tr key={i} style={{ cursor: "default" }}>
                <td><RiskBadge level={s.risk === "高風險" ? "high" : s.risk === "中風險" ? "medium" : "low"} label={s.risk} /></td>
                <td>{s.first}</td>
                <td>{s.resolve}</td>
                <td>{s.remind}</td>
                <td style={{ whiteSpace: "normal", maxWidth: 180 }}>{s.notify}</td>
                <td>{s.calc}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <SectionT>AI 分類規則 <span className="note">（問題分類 × 關鍵字 × 風險權重）</span></SectionT>
      <div className="tbl-wrap">
        <table style={{ minWidth: 0 }}>
          <thead>
            <tr><th>問題分類</th><th className="num">評論數</th><th>風險權重</th><th>命中關鍵字</th><th>狀態</th></tr>
          </thead>
          <tbody>
            {R.ai_categories.map((c) => (
              <tr key={c.name} style={{ cursor: "default" }}>
                <td style={{ fontWeight: 600 }}>{c.name}</td>
                <td className="num">{c.count}</td>
                <td><RiskBadge level={c.weight === "高" ? "high" : c.weight === "中" ? "medium" : "low"} label={c.weight} /></td>
                <td style={{ whiteSpace: "normal", maxWidth: 320 }}>
                  {c.keywords.map((k) => <span key={k} className="tag kw" style={{ marginRight: 4 }}>{k}</span>)}
                </td>
                <td><RiskBadge level="low" label="啟用" /></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="note" style={{ marginTop: 10 }}>
        支援：新增/編輯/合併/停用分類、查看誤判案例、人工修正並回饋 AI（POC 為唯讀示範）。
      </div>
    </>
  );
}
