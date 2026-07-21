/** 客訴案件 — 對應 PAGES.cases / renderCases */
import {
  CASE_STATUS, cnt, fmtD, isActive, openCase, useApp, type CaseStatus,
} from "../lib/db";
import { reviewFilters } from "../lib/filter";
import { scopedCases } from "../lib/roles";
import { FilterBar } from "../components/FilterBar";
import { PageHeader, pocAlert, RiskBadge } from "../components/ui";

const TALLY: CaseStatus[] = ["unassigned", "open", "in_progress", "pending_review", "pending_customer", "done", "closed"];

export default function Cases() {
  useApp();
  const cs = reviewFilters(scopedCases()).sort((a, b) => {
    const ao = isActive(a) ? 0 : 1, bo = isActive(b) ? 0 : 1;
    if (ao !== bo) return ao - bo;
    return (Date.parse(a.sla_due_at) || 0) - (Date.parse(b.sla_due_at) || 0);
  });
  return (
    <>
      <PageHeader
        title="客訴案件"
        sub="將負評轉為可追蹤、可管理的正式案件 · 完整生命週期"
        right={
          <>
            <button className="btn" onClick={() => pocAlert("匯出")}>匯出</button>
            <button className="btn pri" onClick={() => pocAlert("批次處理")}>批次處理</button>
          </>
        }
      />
      <FilterBar full />
      <div className="note" style={{ marginBottom: 10 }}>
        共 {cs.length} 案 ·{" "}
        {TALLY.map((s, i) => (
          <span key={s}>{i > 0 ? " · " : ""}{CASE_STATUS[s]} <b>{cnt(cs, (c) => c.cstatus === s)}</b></span>
        ))}
      </div>
      <div className="tbl-wrap">
        <table>
          <thead>
            <tr>
              <th>案件編號</th><th>建立時間</th><th>品牌/門市</th><th>問題類型</th><th>風險</th>
              <th>顧客摘要</th><th>負責人</th><th>狀態</th><th>升級</th>
            </tr>
          </thead>
          <tbody>
            {cs.slice(0, 120).map((c) => (
              <tr key={c.id} onClick={() => openCase(c.id)}>
                <td style={{ fontVariantNumeric: "tabular-nums" }}>{c.code}</td>
                <td>{fmtD(c.created_at)}</td>
                <td style={{ whiteSpace: "normal", maxWidth: 180 }}>
                  <b>{c.brand_short}</b> {c.store.replace(c.brand, "").replace(/^[ -]+/, "")}
                </td>
                <td>{c.categories[0] ?? "—"}</td>
                <td><RiskBadge level={c.risk_level} /></td>
                <td className="wrap">{c.summary.slice(0, 44)}</td>
                <td style={{ maxWidth: 130, overflow: "hidden", textOverflow: "ellipsis" }}>{c.assignee}</td>
                <td><span className={`pill st-${c.cstatus}`}>{CASE_STATUS[c.cstatus]}</span></td>
                <td>{c.escalated ? <RiskBadge level="high" label="已升級" /> : null}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
