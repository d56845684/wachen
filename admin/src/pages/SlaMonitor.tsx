/** SLA 監控中心 — 對應 PAGES.sla */
import { CASE_STATUS, DB, isActive, KPI, openCase, useApp, useNow } from "../lib/db";
import { scopedCases } from "../lib/roles";
import { BarChart } from "../components/charts";
import { Kpi, PageHeader, RiskBadge, SectionT, Sla, SynthBar } from "../components/ui";

export default function SlaMonitor() {
  useApp();
  useNow(); // 逾期分類即時更新
  const cs = scopedCases();
  const act = cs.filter(isActive);
  const overdue = act.filter((c) => Date.parse(c.sla_due_at) < Date.now());
  const soon = act.filter((c) => {
    const d = Date.parse(c.sla_due_at) - Date.now();
    return d >= 0 && d < 3.6e6;
  });
  const rate = KPI.sla_rate;
  const byBrand = [...DB.brands].sort((a, b) => a.sla_rate - b.sla_rate);

  return (
    <>
      <PageHeader title="SLA 監控中心" sub="即時追蹤案件是否在時限內處理 · 逾期自動升級" />
      <div className="kpis">
        <Kpi v={rate} unit="%" l="整體 SLA 達成率" synth cls={rate < 80 ? "warnv" : ""} />
        <Kpi v={Math.min(99, rate + 6)} unit="%" l="首次回應達成率" synth />
        <Kpi v={Math.max(50, rate - 4)} unit="%" l="結案達成率" synth />
        <Kpi v={overdue.length} l="已逾期案件" cls="alarm" />
        <Kpi v={soon.length} l="即將逾期" cls="warnv" />
        <Kpi v={KPI.resolve_hr} unit="hr" l="平均處理時間" synth />
      </div>
      <SynthBar>
        SLA 達成率為 POC 示意值（依門市雜湊產生）；正式版將以實際「首次回應/結案時間 vs 規則時限」計算。倒數計時使用真實 sla_due_at。
      </SynthBar>

      <SectionT>自動化規則</SectionT>
      <div className="grid g2">
        <div className="card">
          <h3>SLA 提醒與升級規則</h3>
          <ul className="reasons">
            <li>剩餘 50% 時間 → 提醒負責人</li>
            <li>剩餘 20% 時間 → 提醒主管</li>
            <li>逾期 → 自動升級上一層</li>
            <li>高風險案件逾期 → 立即通知總部客服／公關／法務</li>
          </ul>
        </div>
        <div className="card">
          <h3>各品牌 SLA 達成率</h3>
          <BarChart
            rows={byBrand.map((b) => ({
              n: b.name.length > 6 ? b.name.slice(0, 6) : b.name,
              v: b.sla_rate,
              color: b.sla_rate < 75 ? "var(--critical)" : b.sla_rate < 85 ? "var(--warning)" : "var(--good)",
            }))}
            fmt={(v) => v + "%"}
            showPct={false}
          />
        </div>
      </div>

      <SectionT>逾期與即將逾期案件（{overdue.length + soon.length}）</SectionT>
      <div className="tbl-wrap">
        <table>
          <thead>
            <tr><th>案件</th><th>門市</th><th>風險</th><th>負責人</th><th>狀態</th><th>SLA</th><th>顯示狀態</th></tr>
          </thead>
          <tbody>
            {[...overdue, ...soon].slice(0, 60).map((c) => {
              const d = Date.parse(c.sla_due_at) - Date.now();
              return (
                <tr key={c.id} onClick={() => openCase(c.id)}>
                  <td>{c.code}</td>
                  <td style={{ whiteSpace: "normal", maxWidth: 200 }}>{c.store}</td>
                  <td><RiskBadge level={c.risk_level} /></td>
                  <td>{c.assignee}</td>
                  <td><span className={`pill st-${c.cstatus}`}>{CASE_STATUS[c.cstatus]}</span></td>
                  <td><Sla c={c} /></td>
                  <td><RiskBadge level={d < 0 ? "high" : "medium"} label={c.escalated ? "已升級" : d < 0 ? "已逾期" : "即將逾期"} /></td>
                </tr>
              );
            })}
            {!overdue.length && !soon.length ? (
              <tr><td colSpan={7} style={{ textAlign: "center", color: "var(--muted)", padding: 20 }}>目前沒有逾期或即將逾期的案件</td></tr>
            ) : null}
          </tbody>
        </table>
      </div>
    </>
  );
}
