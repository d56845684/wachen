/** 資料來源管理 — 對應 PAGES.sources */
import { DB, useApp } from "../lib/db";
import { PageHeader, pocAlert, RiskBadge } from "../components/ui";

export default function Sources() {
  useApp();
  const stColor = (s: string) => (s === "已串接" ? "low" : s === "規劃中" ? "medium" : "high");
  return (
    <>
      <PageHeader title="資料來源管理" sub="管理平台整合的資料來源與同步狀態" />
      <div className="tbl-wrap">
        <table>
          <thead>
            <tr>
              <th>來源</th><th>類型</th><th>狀態</th><th>同步頻率</th><th>最後同步</th>
              <th className="num">資料筆數</th><th className="num">錯誤</th><th>操作</th>
            </tr>
          </thead>
          <tbody>
            {DB.sources.map((s) => (
              <tr key={s.name} style={{ cursor: "default" }}>
                <td style={{ fontWeight: 600 }}>{s.name}</td>
                <td>{s.type}</td>
                <td><RiskBadge level={stColor(s.status)} label={s.status} /></td>
                <td>{s.sync}</td>
                <td>{s.last}</td>
                <td className="num">{s.rows || "—"}</td>
                <td className="num">{s.err}</td>
                <td>
                  <button className="btn sm" onClick={() => pocAlert(s.status === "已串接" ? "重新同步" : "設定串接")}>
                    {s.status === "已串接" ? "重新同步" : "設定串接"}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="note" style={{ marginTop: 10 }}>
        第一階段 MVP 先串 Google Maps Reviews 與人工匯入 CSV；NPS、LINE、社群、客服信箱為規劃中；POS/CRM/會員/訂位/外送為第二階段。
      </div>
    </>
  );
}
