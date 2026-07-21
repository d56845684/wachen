/** 改善成效分析 — 對應 PAGES.improve */
import { DB, useApp } from "../lib/db";
import { getRole, scopedStores } from "../lib/roles";
import { BarChart } from "../components/charts";
import { PageHeader, SectionT, SynthBar } from "../components/ui";

export default function Improve() {
  useApp();
  const rows = (getRole().brand ? DB.improve_rows_tk : null) ?? DB.improve.rows;
  // 門市排名改由 scoped 門市的 trend 現算 —— 兩個租戶同一條路
  const storeRank = [...scopedStores()]
    .sort((a, b) => b.trend - a.trend).slice(0, 8)
    .map((s) => ({ store: s.store, delta: s.trend }));
  return (
    <>
      <PageHeader title="改善成效分析" sub="驗證處理客訴後，是否真的改善顧客體驗" />
      <SynthBar>
        本頁「改善前/後」為 POC 示意數據，用以展示成效追蹤框架；正式版將以案件關聯的實際前後期資料計算。
      </SynthBar>
      <SectionT>改善前後對比</SectionT>
      <div className="tbl-wrap">
        <table style={{ minWidth: 0 }}>
          <thead>
            <tr><th>改善項目</th><th className="num">改善前</th><th className="num">改善後</th><th className="num">變化</th></tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.item} style={{ cursor: "default" }}>
                <td style={{ fontWeight: 600 }}>{r.item}</td>
                <td className="num">{r.before}</td>
                <td className="num">{r.after}</td>
                <td className="num" style={{ color: r.good ? "var(--good)" : "var(--critical)", fontWeight: 700 }}>{r.delta}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="grid g2" style={{ marginTop: 16 }}>
        <div className="card">
          <h3>指標關聯說明</h3>
          <ul className="reasons">
            <li>同類問題再發率下降 → 改善措施有效</li>
            <li>顧客回訪率上升 → 服務體驗恢復</li>
            <li>SLA 達成率上升 → 案件處理效率提升</li>
            <li>平均評分回升 → 整體體驗改善</li>
          </ul>
        </div>
        <div className="card">
          <h3>門市改善幅度排名</h3>
          <p className="cap">評分改善幅度（示意）</p>
          <BarChart
            rows={storeRank.map((s) => ({
              n: s.store.length > 10 ? s.store.slice(0, 10) : s.store,
              v: s.delta,
              color: s.delta >= 0 ? "var(--good)" : "var(--critical)",
            }))}
            fmt={(v) => (v >= 0 ? "+" : "") + v}
            showPct={false}
          />
        </div>
      </div>
    </>
  );
}
