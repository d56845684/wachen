/** 篩選列 — 對應 HTML 的 filterBar()；直接讀寫全域 rf，改動即 bump() 重繪 */
import { AGG, bump, CASE_STATUS, clearFilters, DB, rf, RISK_LABEL, SENT_LABEL } from "../lib/db";
import { scopedCases } from "../lib/roles";

export function FilterBar({ full }: { full?: boolean }) {
  const brands = DB.brands.map((b) => b.name);
  const stores = [...new Set(scopedCases().map((c) => c.store))].sort();
  const set = (k: keyof typeof rf, v: string) => {
    (rf as unknown as Record<string, string>)[k] = v;
    bump();
  };
  return (
    <div className="filters">
      {full ? (
        <input
          type="search"
          placeholder="關鍵字：門市 / 摘要 / 評論 / 顧客…"
          value={rf.q}
          onChange={(e) => set("q", e.target.value)}
        />
      ) : null}
      <select value={rf.brand} onChange={(e) => set("brand", e.target.value)}>
        <option value="">全部品牌</option>
        {brands.map((b) => <option key={b} value={b}>{b}</option>)}
      </select>
      <select value={rf.store} onChange={(e) => set("store", e.target.value)}>
        <option value="">全部門市</option>
        {stores.map((s) => <option key={s} value={s}>{s}</option>)}
      </select>
      <select value={rf.risk} onChange={(e) => set("risk", e.target.value)}>
        <option value="">全部風險</option>
        {(["high", "medium", "low"] as const).map((k) => <option key={k} value={k}>{RISK_LABEL[k]}</option>)}
      </select>
      <select value={rf.sent} onChange={(e) => set("sent", e.target.value)}>
        <option value="">全部情緒</option>
        {(["negative", "neutral", "positive"] as const).map((k) => <option key={k} value={k}>{SENT_LABEL[k]}</option>)}
      </select>
      {full ? (
        <select value={rf.status} onChange={(e) => set("status", e.target.value)}>
          <option value="">全部狀態</option>
          {Object.entries(CASE_STATUS).map(([k, v]) => <option key={k} value={k}>{v}</option>)}
        </select>
      ) : null}
      <select value={rf.cat} onChange={(e) => set("cat", e.target.value)}>
        <option value="">全部問題</option>
        {AGG.category.map(([c]) => <option key={c} value={c}>{c}</option>)}
      </select>
      {full ? (
        <select value={rf.overdue} onChange={(e) => set("overdue", e.target.value)}>
          <option value="">全部</option>
          <option value="1">僅逾期</option>
        </select>
      ) : null}
      <button className="btn sm" onClick={clearFilters}>清除篩選 ✕</button>
    </div>
  );
}

/** 依 rf 過濾（對應 HTML reviewFilters） */
export { reviewFilters } from "../lib/filter";
