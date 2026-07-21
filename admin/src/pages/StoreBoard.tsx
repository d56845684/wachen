/** 店經理即時儀表板 — 對應 PAGES.store */
import { useNavigate } from "react-router-dom";
import {
  CASES, CASE_STATUS, cnt, DB, fmtD, fmtDT, isActive, META, openCase, useApp,
} from "../lib/db";
import { getRole } from "../lib/roles";
import { AlertRow, Kpi, PageHeader, RiskBadge, SectionT, Stars, SynthBar } from "../components/ui";
import { pocAlert } from "../components/ui";

export default function StoreBoard() {
  useApp();
  const nav = useNavigate();
  const r = getRole();
  const store = r.store ?? DB.stores[0].store;
  const so = DB.stores.find((s) => s.store === store) ?? DB.stores[0];
  const cs = CASES.filter((c) => c.store === store);

  const neg = cnt(cs, (c) => c.sentiment === "negative");
  const openCnt = cnt(cs, (c) => ["unassigned", "open", "in_progress"].includes(c.cstatus));
  const overdue = cnt(cs, (c) => isActive(c) && Date.parse(c.sla_due_at) < Date.now());
  const regionStores = DB.stores.filter((s) => s.region === so.region);
  const rank = [...regionStores].sort((a, b) => a.neg - b.neg).findIndex((s) => s.store === store) + 1;

  const reminders: { level: string; title: string; body: string }[] = [];
  if (overdue) reminders.push({ level: "critical", title: `有 ${overdue} 筆案件已超過 SLA 未處理`, body: "請立即處理或指派人員" });
  const hi = cs.filter((c) => c.risk_level === "high");
  if (hi.length) reminders.push({ level: "critical", title: `新增 ${hi.length} 筆高風險評論`, body: hi[0].summary.slice(0, 40) });
  reminders.push({ level: "warning", title: "本週「服務態度」負評上升", body: "建議檢視尖峰時段排班與新人訓練" });
  reminders.push({ level: "serious", title: `平均評分較上月變化 ${so.trend >= 0 ? "+" : ""}${so.trend}`, body: so.trend < 0 ? "需關注下降趨勢" : "維持良好" });

  const recent = [...cs].sort((a, b) => b.posted_at.localeCompare(a.posted_at)).slice(0, 10);

  return (
    <>
      <PageHeader
        title={store}
        sub={`${so.brand} · ${so.region} · ${so.city} · 店經理 ${so.manager} · 營業中 · 最後更新 ${fmtDT(META.generated_ref)}`}
      />
      <div className="kpis">
        <Kpi v={so.avg_rating} l="本店平均評分" delta={so.trend} />
        <Kpi v={cs.length} l="評論數" s={`負評 ${neg}`} />
        <Kpi v={neg} l="負評數" cls="alarm" />
        <Kpi v={openCnt} l="待處理案件" cls={openCnt ? "warnv" : ""} onGo={() => nav("/cases")} />
        <Kpi v={overdue} l="超時案件" cls="alarm" />
        <Kpi v={`${rank}/${regionStores.length}`} l={`${so.region}區域排名`} s="負評由少到多" />
      </div>
      <SynthBar>來客數、平均出餐時間、翻桌率、營收、客單價等指標將於串接 POS 後開放（第二階段）。</SynthBar>

      <SectionT>即時提醒</SectionT>
      <div className="alist">
        {reminders.map((a, i) => (
          <AlertRow
            key={i}
            level={a.level}
            title={a.title}
            body={a.body}
            actions={
              <>
                <button className="btn sm" onClick={() => nav("/cases")}>查看詳情</button>
                <button className="btn sm" onClick={() => pocAlert("立即處理")}>立即處理</button>
                <button className="btn sm" onClick={() => pocAlert("標記已讀")}>標記已讀</button>
              </>
            }
          />
        ))}
      </div>

      <SectionT>最新負評 <span className="note">（點列開啟）</span></SectionT>
      <div className="tbl-wrap">
        <table>
          <thead>
            <tr>
              <th>評論時間</th><th>平台</th><th className="num">星等</th><th>顧客評論</th>
              <th>AI 分類</th><th>風險</th><th>狀態</th><th>負責人</th>
            </tr>
          </thead>
          <tbody>
            {recent.map((c) => (
              <tr key={c.id} onClick={() => openCase(c.id)}>
                <td>{fmtD(c.posted_at)}</td>
                <td>{c.platform}</td>
                <td className="num"><Stars n={c.rating} /></td>
                <td className="wrap">{c.summary.slice(0, 50)}</td>
                <td>{c.categories[0] ?? "—"}</td>
                <td><RiskBadge level={c.risk_level} /></td>
                <td><span className={`pill st-${c.cstatus}`}>{CASE_STATUS[c.cstatus]}</span></td>
                <td>{c.assignee.replace("店經理 · ", "")}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
