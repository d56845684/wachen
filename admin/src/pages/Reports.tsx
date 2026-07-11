/** 報表中心 — 對應 PAGES.reports */
import { DB, META, useApp } from "../lib/db";
import { PageHeader, pocAlert } from "../components/ui";

const REPORTS = [
  "集團週報", "品牌月報", "區域績效報表", "門市評分報表", "負評原因報表",
  "客訴案件報表", "SLA 報表", "改善成效報表", "高風險事件報表",
];

export default function Reports() {
  useApp();
  return (
    <>
      <PageHeader title="報表中心" sub="定期下載或排程寄送報表" />
      <div className="filters">
        <select><option>日期區間：本月</option><option>上月</option><option>本季</option></select>
        <select>
          <option>全部品牌</option>
          {DB.brands.map((b) => <option key={b.name}>{b.name}</option>)}
        </select>
        <button className="btn" onClick={() => pocAlert("排程寄送設定")}>排程寄送設定</button>
      </div>
      <div className="grid g3">
        {REPORTS.map((r) => (
          <div className="card" key={r}>
            <h3>{r}</h3>
            <p className="cap">最近產製：{META.date_max}</p>
            <div style={{ display: "flex", gap: 8 }}>
              <button className="btn sm" onClick={() => pocAlert(`${r} Excel 匯出`)}>Excel 匯出</button>
              <button className="btn sm" onClick={() => pocAlert(`${r} PDF 匯出`)}>PDF 匯出</button>
              <button className="btn sm" onClick={() => pocAlert(`${r} 排程`)}>排程</button>
            </div>
          </div>
        ))}
      </div>
      <div className="note" style={{ marginTop: 12 }}>POC：報表產製與排程寄送為介面示範。</div>
    </>
  );
}
