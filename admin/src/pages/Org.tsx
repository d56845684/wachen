/** 組織與權限 — 對應 PAGES.org（需總部權限） */
import { CAT_COLORS, DB, useApp } from "../lib/db";
import { getRole } from "../lib/roles";
import { BarChart } from "../components/charts";
import { PageHeader, SectionT } from "../components/ui";

const PERM_KEYS = ["pii", "ai", "assign", "close", "export", "rules"] as const;

export default function Org() {
  useApp();
  const role = getRole();
  if (!role.rules) {
    return <div className="empty">此頁需要「總部管理員／超級管理員」權限。目前角色為 {role.title}。</div>;
  }
  const O = DB.org;
  return (
    <>
      <PageHeader title="組織與權限" sub="管理品牌、區域、門市、人員與角色" />
      <div className="grid g2" style={{ marginBottom: 16 }}>
        <div className="card">
          <h3>組織架構</h3>
          <p style={{ margin: 0, lineHeight: 2 }}>
            <b>{O.tree["集團"]}</b>（集團）<br />
            ├─ 品牌 × {O.tree.brands.length}：{O.tree.brands.map((b) => (b.length > 6 ? b.slice(0, 6) : b)).join("、")}<br />
            ├─ 區域 × {O.tree.regions.length}：{O.tree.regions.join("、")}<br />
            ├─ 門市 × {O.tree.stores}<br />
            └─ 人員 × {O.tree.people}
          </p>
        </div>
        <div className="card">
          <h3>角色數量</h3>
          <BarChart rows={O.roles.map((r, i) => ({ n: r.role, v: 1, color: CAT_COLORS[i % 8] }))} showPct={false} fmt={() => ""} />
          <div className="note">共 {O.roles.length} 種角色</div>
        </div>
      </div>
      <SectionT>角色權限矩陣</SectionT>
      <div className="tbl-wrap">
        <table>
          <thead>
            <tr>
              <th>角色</th><th>資料範圍</th><th>顧客個資</th><th>修改 AI 判定</th>
              <th>分派案件</th><th>結案</th><th>匯出</th><th>規則設定</th>
            </tr>
          </thead>
          <tbody>
            {O.roles.map((r) => (
              <tr key={r.role} style={{ cursor: "default" }}>
                <td style={{ fontWeight: 600 }}>{r.role}</td>
                <td>{r.scope}</td>
                {PERM_KEYS.map((k) => (
                  <td key={k}>
                    {r[k] ? <span style={{ color: "var(--good)" }}>✔</span> : <span style={{ color: "var(--muted)" }}>—</span>}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
