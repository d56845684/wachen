/** 負評管理 — 對應 PAGES.reviews / renderReviews */
import {
  CASE_STATUS, fmtD, openCase, RISK_LABEL, RISK_RANK, SENT_LABEL, useApp,
} from "../lib/db";
import { reviewFilters } from "../lib/filter";
import { scopedCases } from "../lib/roles";
import { FilterBar } from "../components/FilterBar";
import { PageHeader, pocAlert, RiskBadge, Sla, Stars } from "../components/ui";

export default function Reviews() {
  useApp();
  const cs = reviewFilters(scopedCases()).sort(
    (a, b) => RISK_RANK[b.risk_level] - RISK_RANK[a.risk_level] || b.posted_at.localeCompare(a.posted_at),
  );
  return (
    <>
      <PageHeader
        title="負評管理"
        sub="集中管理來自各平台的顧客評論 · 目前來源：Google Maps Reviews"
        right={
          <>
            <button className="btn" onClick={() => pocAlert("匯出資料")}>匯出資料</button>
            <button className="btn" onClick={() => pocAlert("批次指派")}>批次指派</button>
          </>
        }
      />
      <FilterBar full />
      <div className="note" style={{ marginBottom: 10 }}>共 {cs.length} 則評論</div>
      <div className="tbl-wrap">
        <table>
          <thead>
            <tr>
              <th>評論時間</th><th>品牌/門市</th><th>平台</th><th className="num">星等</th><th>評論摘要</th>
              <th>AI 情緒</th><th>AI 分類</th><th>風險</th><th>案件狀態</th><th>負責人</th><th>SLA 倒數</th>
            </tr>
          </thead>
          <tbody>
            {cs.slice(0, 120).map((c) => (
              <tr key={c.id} onClick={() => openCase(c.id)}>
                <td>{fmtD(c.posted_at)}</td>
                <td style={{ whiteSpace: "normal", maxWidth: 200 }}>
                  <b>{c.brand_short}</b> {c.store.replace(c.brand, "").replace(/^[ -]+/, "")}
                </td>
                <td>{c.platform}</td>
                <td className="num"><Stars n={c.rating} /></td>
                <td className="wrap">{c.summary.slice(0, 54)}</td>
                <td><span className={`sent ${c.sentiment}`}>{SENT_LABEL[c.sentiment]}</span></td>
                <td>{c.categories.slice(0, 2).map((x) => <span key={x} className="tag" style={{ marginRight: 4 }}>{x}</span>)}</td>
                <td><RiskBadge level={c.risk_level} label={RISK_LABEL[c.risk_level]} /></td>
                <td><span className={`pill st-${c.cstatus}`}>{CASE_STATUS[c.cstatus]}</span></td>
                <td style={{ maxWidth: 130, overflow: "hidden", textOverflow: "ellipsis" }}>{c.assignee}</td>
                <td><Sla c={c} /></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {cs.length > 120 ? (
        <div className="note" style={{ marginTop: 8 }}>僅顯示前 120 筆（共 {cs.length}）— 請用篩選縮小範圍</div>
      ) : null}
    </>
  );
}
