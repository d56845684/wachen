/** 總部管理儀表板 — 對應 PAGES.dashboard */
import { useNavigate } from "react-router-dom";
import { useMemo, useState } from "react";
import {
  AGG, CAT_COLORS, clearFilters, cnt, DB, isActive, KPI, rf,
  RISK_COLOR, RISK_LABEL, SENT_COLOR, SENT_LABEL, sum, useApp, bump, openStore,
  type WaStore,
} from "../lib/db";
import { scopedCases, scopedStores } from "../lib/roles";
import { BarChart, Donut, LineChart } from "../components/charts";
import { AlertRow, Kpi, PageHeader, RiskBadge, SectionT, SynthBar } from "../components/ui";

export default function Dashboard() {
  useApp();
  const nav = useNavigate();
  const cs = scopedCases();
  const stores = scopedStores();

  const neg = cnt(cs, (c) => c.sentiment === "negative");
  const highOpen = cnt(cs, (c) => c.risk_level === "high" && isActive(c));
  const overdue = cnt(cs, (c) => isActive(c) && Date.parse(c.sla_due_at) < Date.now());
  const avg = (sum(cs, (c) => c.rating || 0) / (cs.length || 1)).toFixed(2);

  const go = (dest: "neg" | "cases" | "sla" | "high" | "overdue") => {
    clearFilters();
    if (dest === "neg") rf.sent = "negative";
    if (dest === "high") rf.risk = "high";
    if (dest === "overdue") rf.overdue = "1";
    bump();
    nav(dest === "sla" ? "/sla" : dest === "cases" || dest === "overdue" ? "/cases" : "/reviews");
  };

  const cat = AGG.category.map((x, i) => ({ n: x[0], v: x[1], color: CAT_COLORS[i % 8] }));
  const months = AGG.monthly.map((m) => m.month.slice(2));

  return (
    <>
      <PageHeader
        title="總部管理儀表板"
        sub="即時掌握全台門市評分、負評、客訴與 SLA — 快速定位需要介入的品牌、區域與門市"
      />
      <div className="kpis">
        <Kpi v={avg} l="集團平均評分" delta={0.3} s="較改善前 4.1" synth />
        <Kpi v={KPI.new_reviews} l="新增評論（近30天）" s={`累計 ${cs.length} 則`} />
        <Kpi v={neg} l="負評數" cls="alarm" s={`負評率 ${cs.length ? Math.round((neg / cs.length) * 100) : 0}%`} onGo={() => go("neg")} />
        <Kpi v={cs.length} l="客訴案件數" onGo={() => go("cases")} />
        <Kpi v={KPI.sla_rate} unit="%" l="SLA 達成率" cls={KPI.sla_rate < 80 ? "warnv" : ""} synth onGo={() => go("sla")} />
        <Kpi v={KPI.first_resp_min} unit="分" l="平均首次回應" synth />
        <Kpi v={KPI.resolve_hr} unit="hr" l="平均結案時間" synth />
        <Kpi v={highOpen} l="高風險未結" cls="alarm" onGo={() => go("high")} />
        <Kpi v={overdue} l="SLA 逾期" cls="alarm" onGo={() => go("overdue")} />
        <Kpi v={KPI.revisit_rate} unit="%" l="顧客回訪率" synth />
      </div>
      <SynthBar>
        標記「模擬」的指標（SLA、回應/結案時間、回訪率、改善幅度）為 POC 示意值；未串接 POS 前，暫不呈現營收與來客數等無法驗證的數字。
      </SynthBar>

      <SectionT>趨勢與分布</SectionT>
      <div className="grid g2">
        <div className="card">
          <h3>負評趨勢（月）</h3>
          <p className="cap">評論量 vs 負評量，近 {AGG.monthly.length} 個月</p>
          <LineChart
            labels={months}
            series={[
              { label: "評論量", color: "var(--s1)", data: AGG.monthly.map((m) => m.reviews) },
              { label: "負評量", color: "var(--critical)", data: AGG.monthly.map((m) => m.neg) },
            ]}
          />
        </div>
        <div className="card">
          <h3>平均評分趨勢（月）</h3>
          <p className="cap">集團整體評分變化</p>
          <LineChart labels={months} h={150} series={[{ label: "平均評分", color: "var(--s2)", data: AGG.monthly.map((m) => m.avg_rating) }]} />
        </div>
        <div className="card">
          <h3>負評原因分布</h3>
          <p className="cap">各問題類型占比</p>
          <Donut rows={cat.slice(0, 7)} centerB={AGG.category.reduce((a, x) => a + x[1], 0)} centerS="問題標記" />
        </div>
        <div className="card">
          <h3>風險等級分布</h3>
          <p className="cap">依 SLA 與食安關鍵字分級</p>
          <BarChart rows={(["high", "medium", "low"] as const).map((k) => ({ n: RISK_LABEL[k], v: cnt(cs, (c) => c.risk_level === k), color: RISK_COLOR[k] }))} />
          <div style={{ marginTop: 14 }} />
          <h3 style={{ fontSize: 13 }}>情緒傾向</h3>
          <BarChart rows={(["negative", "neutral", "positive"] as const).map((k) => ({ n: SENT_LABEL[k], v: cnt(cs, (c) => c.sentiment === k), color: SENT_COLOR[k] }))} />
        </div>
      </div>

      <SectionT>區域熱點 · 各區風險概況</SectionT>
      <div className="grid g3">
        {DB.regions.map((rg) => {
          const st = rg.neg_rate >= 35 ? "critical" : rg.neg_rate >= 25 ? "warning" : "good";
          return (
            <div key={rg.name} className="card" style={{ borderLeft: `4px solid var(--${st})` }}>
              <h3>
                {rg.name}{" "}
                <RiskBadge level={st === "critical" ? "high" : st === "warning" ? "medium" : "low"} label={st === "critical" ? "高風險" : st === "warning" ? "中風險" : "正常"} />
              </h3>
              <p className="cap">{rg.stores} 家門市 · {rg.total} 則評論</p>
              <dl className="kv">
                <dt>平均評分</dt><dd>{rg.avg_rating}</dd>
                <dt>負評數 / 率</dt><dd>{rg.neg}（{rg.neg_rate}%）</dd>
                <dt>高風險</dt><dd>{rg.high}</dd>
                <dt>SLA 達成</dt><dd>{rg.sla_rate}%</dd>
              </dl>
            </div>
          );
        })}
      </div>
      <SynthBar>
        台灣區域熱點地圖（依風險紅/黃/綠著色、可點擊下鑽）將於正式版整合地圖套件；POC 先以區域卡片呈現。目前資料集中於台北市。
      </SynthBar>

      <SectionT>門市排名 <span className="note">（點門市看詳情，點欄位排序）</span></SectionT>
      <StoreRank stores={stores} />

      <SectionT>AI 洞察與建議</SectionT>
      <div className="alist">
        {DB.insights.anomalies.slice(0, 3).map((a, i) => (
          <AlertRow
            key={i}
            level={a.sev ?? a.level}
            title={a.t ?? a.title ?? ""}
            body={a.d ?? a.body ?? ""}
            actions={
              <>
                <button className="btn sm" onClick={() => nav("/ai")}>查看分析</button>
                <button className="btn sm" onClick={() => nav("/tasks")}>建立改善任務</button>
              </>
            }
          />
        ))}
      </div>
    </>
  );
}

const RANK_COLS: { k: keyof WaStore; t: string; num?: boolean }[] = [
  { k: "store", t: "門市" }, { k: "brand_short", t: "品牌" }, { k: "region", t: "區域" },
  { k: "avg_rating", t: "平均評分", num: true }, { k: "neg", t: "負評", num: true },
  { k: "neg_rate", t: "負評率", num: true }, { k: "sla_rate", t: "SLA", num: true },
  { k: "avg_handle_hr", t: "處理時數", num: true }, { k: "trend", t: "趨勢", num: true },
  { k: "risk_status", t: "風險" },
];

function StoreRank({ stores }: { stores: WaStore[] }) {
  const [sort, setSort] = useState<{ k: keyof WaStore; dir: 1 | -1 } | null>(null);
  const rows = useMemo(() => {
    const r = [...stores].slice(0, sort ? stores.length : 8);
    if (!sort) return r.slice(0, 8);
    r.sort((a, b) => {
      const av = a[sort.k], bv = b[sort.k];
      return typeof av === "string"
        ? sort.dir * String(av).localeCompare(String(bv), "zh-Hant")
        : sort.dir * ((av as number) - (bv as number));
    });
    return r.slice(0, 8);
  }, [stores, sort]);

  return (
    <div className="tbl-wrap">
      <table>
        <thead>
          <tr>
            {RANK_COLS.map((c) => (
              <th
                key={c.k}
                className={c.num ? "num" : ""}
                onClick={() => setSort((s) => ({ k: c.k, dir: s?.k === c.k && s.dir === 1 ? -1 : 1 }))}
              >
                {c.t}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((s) => (
            <tr key={s.code} onClick={() => openStore(s.store)}>
              <td style={{ whiteSpace: "normal", fontWeight: 600, maxWidth: 220 }}>{s.store}</td>
              <td>{s.brand_short}</td>
              <td>{s.region}</td>
              <td className="num">{s.avg_rating}</td>
              <td className="num">{s.neg}</td>
              <td className="num">{s.neg_rate}%</td>
              <td className="num">{s.sla_rate}%</td>
              <td className="num">{s.avg_handle_hr}</td>
              <td className="num" style={{ color: s.trend >= 0 ? "var(--good)" : "var(--critical)" }}>
                {s.trend >= 0 ? "▲" : "▼"}{Math.abs(s.trend)}
              </td>
              <td><RiskBadge level={s.risk_status} /></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
