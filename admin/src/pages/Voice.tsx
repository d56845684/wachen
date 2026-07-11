/** 顧客聲量與情緒分析 — 對應 PAGES.voice */
import { AGG, cnt, DB, pct, SENT_COLOR, SENT_LABEL, sum, useApp } from "../lib/db";
import { scopedCases } from "../lib/roles";
import { BarChart, Donut, KwCloud, LineChart } from "../components/charts";
import { Kpi, PageHeader } from "../components/ui";
import { FilterBar } from "../components/FilterBar";

export default function Voice() {
  useApp();
  const cs = scopedCases();
  const months = AGG.monthly.map((m) => m.month.slice(2));
  const starRows = [5, 4, 3, 2, 1].map((n) => ({
    n: "★".repeat(n),
    v: AGG.star[String(n)] ?? 0,
    color: ["var(--seq5)", "var(--seq4)", "var(--seq3)", "var(--seq2)", "var(--seq1)"][5 - n],
  }));
  const brandVoice = DB.brands.map((b) => ({ n: b.name.length > 6 ? b.name.slice(0, 6) : b.name, v: b.avg_rating, color: "var(--s2)" }));
  const sentRows = (["positive", "neutral", "negative"] as const).map((k) => ({
    n: SENT_LABEL[k], v: cnt(cs, (c) => c.sentiment === k), color: SENT_COLOR[k],
  }));
  const pos = cnt(cs, (c) => c.sentiment === "positive");
  const neu = cnt(cs, (c) => c.sentiment === "neutral");
  const neg = cnt(cs, (c) => c.sentiment === "negative");

  return (
    <>
      <PageHeader title="顧客聲量與情緒分析" sub="看見整體顧客意見，而不只關注一星評論 — 也找出顧客真正喜歡什麼" />
      <FilterBar />
      <div className="kpis">
        <Kpi v={pos} l="正面聲量" s={pct(pos, cs.length) + "%"} />
        <Kpi v={neu} l="中立聲量" s={pct(neu, cs.length) + "%"} />
        <Kpi v={neg} l="負面聲量" cls="alarm" s={pct(neg, cs.length) + "%"} />
        <Kpi v={(sum(cs, (c) => c.rating || 0) / (cs.length || 1)).toFixed(2)} l="平均星等" />
      </div>
      <div className="grid g2">
        <div className="card">
          <h3>情緒趨勢（月）</h3>
          <p className="cap">負評量與評論量變化</p>
          <LineChart
            labels={months}
            series={[
              { label: "評論量", color: "var(--s1)", data: AGG.monthly.map((m) => m.reviews) },
              { label: "負評量", color: "var(--critical)", data: AGG.monthly.map((m) => m.neg) },
            ]}
          />
        </div>
        <div className="card">
          <h3>星等分布</h3>
          <p className="cap">1–5 星</p>
          <BarChart rows={starRows} showPct={false} />
        </div>
        <div className="card"><h3>情緒占比</h3><Donut rows={sentRows} centerB={cs.length} centerS="則評論" /></div>
        <div className="card">
          <h3>各品牌平均評分</h3>
          <p className="cap">品牌聲量比較</p>
          <BarChart rows={brandVoice} fmt={(v) => String(v)} showPct={false} />
        </div>
      </div>
      <div className="grid g2">
        <div className="card"><h3>熱門正面關鍵字</h3><p className="cap">顧客稱讚什麼</p><KwCloud list={AGG.kw_pos} tone="pos" /></div>
        <div className="card"><h3>熱門負面關鍵字</h3><p className="cap">顧客抱怨什麼</p><KwCloud list={AGG.kw_neg} tone="neg" /></div>
      </div>
    </>
  );
}
