/** 門市管理 — 對應 PAGES.stores / renderStoresTbl */
import { useMemo, useState } from "react";
import { openStore, RISK_LABEL, useApp, type WaStore } from "../lib/db";
import { scopedStores } from "../lib/roles";
import { PageHeader, pocAlert, RiskBadge } from "../components/ui";

const COLS: { k: keyof WaStore; t: string; num?: boolean }[] = [
  { k: "brand_short", t: "品牌" }, { k: "store", t: "門市名稱" }, { k: "code", t: "代碼" },
  { k: "region", t: "區域" }, { k: "manager", t: "店經理" },
  { k: "avg_rating", t: "評分", num: true }, { k: "neg", t: "負評", num: true },
  { k: "open_cases", t: "未結案", num: true }, { k: "sla_rate", t: "SLA", num: true },
  { k: "risk_status", t: "風險" },
];

export default function Stores() {
  useApp();
  const [q, setQ] = useState("");
  const [sort, setSort] = useState<{ k: keyof WaStore; dir: 1 | -1 } | null>(null);
  const all = scopedStores();

  const rows = useMemo(() => {
    let r = all.filter((s) => !q || (s.store + s.brand + s.manager).toLowerCase().includes(q.toLowerCase()));
    if (sort) {
      r = [...r].sort((a, b) => {
        const av = a[sort.k], bv = b[sort.k];
        return typeof av === "string"
          ? sort.dir * String(av).localeCompare(String(bv), "zh-Hant")
          : sort.dir * ((av as number) - (bv as number));
      });
    }
    return r;
  }, [all, q, sort]);

  return (
    <>
      <PageHeader
        title="門市管理"
        sub={`管理所有品牌與門市基本資料及績效狀態 · 共 ${all.length} 家`}
        right={
          <>
            <button className="btn" onClick={() => pocAlert("新增門市")}>新增門市</button>
            <button className="btn" onClick={() => pocAlert("綁定評論來源")}>綁定評論來源</button>
          </>
        }
      />
      <div className="filters">
        <input type="search" placeholder="搜尋門市 / 品牌 / 店經理…" value={q} onChange={(e) => setQ(e.target.value)} />
        <span className="synth" style={{ marginLeft: "auto" }}>品牌/區域/店經理/SLA 為推導或示意值</span>
      </div>
      <div className="tbl-wrap">
        <table>
          <thead>
            <tr>
              {COLS.map((c) => (
                <th key={c.k} className={c.num ? "num" : ""} onClick={() => setSort((s) => ({ k: c.k, dir: s?.k === c.k && s.dir === 1 ? -1 : 1 }))}>
                  {c.t}
                </th>
              ))}
              <th>營業</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((s) => (
              <tr key={s.code} onClick={() => openStore(s.store)}>
                <td><b>{s.brand_short}</b></td>
                <td style={{ whiteSpace: "normal", maxWidth: 230, fontWeight: 600 }}>{s.store}</td>
                <td>{s.code}</td>
                <td>{s.region}</td>
                <td>{s.manager}</td>
                <td className="num">{s.avg_rating}</td>
                <td className="num">
                  {s.neg}
                  <span className="mini" style={{ width: Math.min(40, s.neg * 8) }} />
                </td>
                <td className="num">{s.open_cases}</td>
                <td className="num">{s.sla_rate}%</td>
                <td><RiskBadge level={s.risk_status} label={RISK_LABEL[s.risk_status]} /></td>
                <td>{s.biz_status}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
