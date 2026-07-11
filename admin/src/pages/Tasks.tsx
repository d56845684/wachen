/** 改善任務管理 — 對應 PAGES.tasks */
import { cnt, DB, useApp } from "../lib/db";
import { Kpi, PageHeader, pocAlert, RiskBadge } from "../components/ui";

const STATUSES = ["待開始", "進行中", "待驗證", "已完成", "延遲"];

export default function Tasks() {
  useApp();
  const T = DB.tasks;
  return (
    <>
      <PageHeader
        title="改善任務管理"
        sub="避免案件結案後沒有真正改善 · 任務來源：客訴 / AI 洞察 / 稽核 / 人工"
        right={<button className="btn pri" onClick={() => pocAlert("新增任務")}>＋ 新增任務</button>}
      />
      <div className="kpis">
        {STATUSES.map((s) => <Kpi key={s} v={cnt(T, (t) => t.status === s)} l={s} />)}
      </div>
      <div className="tbl-wrap">
        <table>
          <thead>
            <tr>
              <th>任務</th><th>問題類型</th><th>門市</th><th>負責人</th><th>優先</th>
              <th>期限</th><th>進度</th><th>狀態</th><th>改善 KPI</th>
            </tr>
          </thead>
          <tbody>
            {T.map((t) => (
              <tr key={t.id} style={{ cursor: "default" }}>
                <td style={{ whiteSpace: "normal", maxWidth: 190, fontWeight: 600 }}>{t.name}</td>
                <td>{t.category}</td>
                <td style={{ whiteSpace: "normal", maxWidth: 160 }}>{t.store}</td>
                <td>{t.owner}</td>
                <td><RiskBadge level={t.priority === "高" ? "high" : "medium"} label={t.priority} /></td>
                <td>{t.due}</td>
                <td style={{ minWidth: 110 }}><progress value={t.progress} max={100} /></td>
                <td><span className={`pill st-${t.status === "已完成" ? "done" : t.status === "延遲" ? "open" : "in_progress"}`}>{t.status}</span></td>
                <td style={{ whiteSpace: "normal", maxWidth: 180, color: "var(--ink-2)" }}>{t.kpi}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="note" style={{ marginTop: 8 }}>任務由真實負評熱點（問題類型 × 高負評門市）自動生成示範。</div>
    </>
  );
}
