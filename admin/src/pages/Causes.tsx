/** 負評原因分析 — 對應 PAGES.causes */
import { AGG, CAT_COLORS, cnt, DB, useApp, WEEKDAY } from "../lib/db";
import { scopedCases } from "../lib/roles";
import { BarChart, Donut, KwCloud } from "../components/charts";
import { PageHeader, SectionT } from "../components/ui";
import { FilterBar } from "../components/FilterBar";

export default function Causes() {
  useApp();
  const cs = scopedCases();
  const cat = AGG.category.map((x, i) => ({ n: x[0], v: x[1], color: CAT_COLORS[i % 8] }));
  const topBrands = DB.brands.slice(0, 4);
  const catByBrand = AGG.category.slice(0, 6).map(([c]) => ({
    c,
    vals: topBrands.map((b) => cnt(cs, (x) => x.brand === b.name && x.categories.includes(c))),
  }));
  const hourRows = [];
  for (let h = 10; h <= 22; h++)
    hourRows.push({ n: h + ":00", v: AGG.hour[h], color: AGG.hour[h] >= Math.max(...AGG.hour) * 0.7 ? "var(--critical)" : "var(--s1)" });
  const wkRows = WEEKDAY.map((w, i) => ({ n: w, v: AGG.weekday[i], color: i >= 5 ? "var(--warning)" : "var(--s1)" }));

  return (
    <>
      <PageHeader
        title="負評原因分析"
        sub="拆解顧客負評來源 · AI 自動歸類同義說法（如「等太久 / 上菜慢 / 餐點沒來」→ 出餐速度）"
      />
      <FilterBar />
      <div className="grid g2">
        <div className="card">
          <h3>問題類型占比</h3>
          <p className="cap">全部評論的問題標記</p>
          <Donut rows={cat.slice(0, 7)} centerB={AGG.category.reduce((a, x) => a + x[1], 0)} centerS="標記數" />
        </div>
        <div className="card">
          <h3>問題類型 × 時段（負評）</h3>
          <p className="cap">負評集中於午餐與晚餐尖峰</p>
          <BarChart rows={hourRows} showPct={false} />
        </div>
        <div className="card">
          <h3>問題類型 × 星期（負評）</h3>
          <p className="cap">週末負評明顯偏高</p>
          <BarChart rows={wkRows} showPct={false} />
        </div>
        <div className="card">
          <h3>問題類型 × 品牌</h3>
          <p className="cap">前 4 大品牌 × 前 6 類問題（負評/全部標記數）</p>
          <div className="tbl-wrap" style={{ boxShadow: "none", border: "none" }}>
            <table style={{ minWidth: 0 }}>
              <thead>
                <tr>
                  <th>問題</th>
                  {topBrands.map((b) => <th key={b.name} className="num">{b.name.slice(0, 4)}</th>)}
                </tr>
              </thead>
              <tbody>
                {catByBrand.map((r) => (
                  <tr key={r.c} style={{ cursor: "default" }}>
                    <td>{r.c}</td>
                    {r.vals.map((v, i) => <td key={i} className="num">{v || ""}</td>)}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      </div>
      <SectionT>高頻負面關鍵字</SectionT>
      <div className="card"><KwCloud list={AGG.kw_neg} tone="neg" /></div>
      <SectionT>AI 歸類說明</SectionT>
      <div className="card">
        <p style={{ margin: 0, color: "var(--ink-2)" }}>
          AI 會將語意相近的不同說法合併為同一問題類型，例如「等好久」「上菜很慢」「餐點一直沒來」統一歸為 <b>出餐速度</b>；
          「態度差」「不理人」「臉很臭」歸為 <b>服務態度</b>。目前共歸納出 {AGG.category.length} 個問題類型。
        </p>
      </div>
    </>
  );
}
