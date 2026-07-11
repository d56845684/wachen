/** rf 過濾邏輯 — 對應 HTML reviewFilters() */
import { isActive, rf, type WaCase } from "./db";

export function reviewFilters(cs: WaCase[]): WaCase[] {
  return cs.filter((c) => {
    if (rf.q) {
      const h = (c.store + c.summary + c.review_content + c.keywords.join("") + c.author_name).toLowerCase();
      if (!h.includes(rf.q.toLowerCase())) return false;
    }
    if (rf.brand && c.brand !== rf.brand) return false;
    if (rf.store && c.store !== rf.store) return false;
    if (rf.risk && c.risk_level !== rf.risk) return false;
    if (rf.sent && c.sentiment !== rf.sent) return false;
    if (rf.status && c.cstatus !== rf.status) return false;
    if (rf.cat && !c.categories.includes(rf.cat)) return false;
    if (rf.overdue === "1" && !(isActive(c) && Date.parse(c.sla_due_at) < Date.now())) return false;
    return true;
  });
}
