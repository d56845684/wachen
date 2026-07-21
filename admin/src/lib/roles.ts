/** 角色 / 選單 / 資料範圍 — 由 瓦城顧客體驗中台.html 的 ROLES / MENU / MENU_ROLE 移植 */
import { bump, CASES, DB, type WaCase, type WaStore } from "./db";

export type RoleId = "hq" | "region" | "store" | "cs" | "pr";

export interface Role {
  name: string; title: string; av: string; scope: string | null;
  home: string; region?: string; store?: string;
  menu: "all" | "region" | "store" | "cs" | "pr";
  pii: boolean; rules: boolean; riskOnly?: boolean;
}

export const ROLES: Record<RoleId, Role> = {
  hq: { name: "系統管理員", title: "總部管理者", av: "系", scope: "全集團 · 全品牌 · 全門市", home: "/dashboard", menu: "all", pii: true, rules: true },
  region: { name: "林區經理", title: "區經理（北一區）", av: "林", scope: "北一區", home: "/dashboard", region: "北一區", menu: "region", pii: true, rules: false },
  store: { name: "陳店經理", title: "店經理", av: "陳", scope: null, home: "/store", store: DB.stores[0].store, menu: "store", pii: false, rules: false },
  cs: { name: "何采潔", title: "客服人員", av: "何", scope: "指派案件", home: "/cases", menu: "cs", pii: true, rules: false },
  pr: { name: "鍾岳霖", title: "公關／法務", av: "鍾", scope: "高風險案件", home: "/reviews", menu: "pr", pii: true, rules: false, riskOnly: true },
};

/* 側欄選單（中台 16 頁 + 後台整合 3 頁） */
export const MENU: { g: string; items: { id: string; t: string }[] }[] = [
  { g: "總覽", items: [{ id: "dashboard", t: "首頁總覽" }] },
  {
    g: "案件與評論",
    items: [
      { id: "reviews", t: "負評管理" },
      { id: "cases", t: "客訴案件" },
      { id: "sla", t: "SLA 監控" },
    ],
  },
  {
    g: "門市與分析",
    items: [
      { id: "stores", t: "門市管理" },
      { id: "causes", t: "負評原因分析" },
      { id: "voice", t: "顧客聲量與情緒" },
      { id: "ai", t: "AI 洞察中心" },
    ],
  },
  {
    g: "改善與成效",
    items: [
      { id: "tasks", t: "改善任務" },
      { id: "improve", t: "改善成效分析" },
    ],
  },
  {
    g: "系統",
    items: [
      { id: "notifications", t: "通知中心" },
      { id: "reports", t: "報表中心" },
      { id: "rules", t: "規則設定" },
      { id: "org", t: "組織與權限" },
      { id: "sources", t: "資料來源" },
    ],
  },
  {
    g: "後台整合（正式 API）",
    items: [
      { id: "inbox", t: "即時案件（DB）" },
      { id: "pipeline", t: "AI 處理進度" },
      { id: "approvals", t: "回覆審核" },
    ],
  },
];

const MENU_ROLE: Record<Role["menu"], string[] | null> = {
  store: ["dashboard", "reviews", "cases", "sla", "tasks", "notifications"],
  cs: ["dashboard", "reviews", "cases", "sla", "notifications", "tasks"],
  pr: ["dashboard", "reviews", "cases", "sla", "notifications"],
  region: ["dashboard", "reviews", "cases", "sla", "stores", "causes", "voice", "ai", "tasks", "improve", "notifications", "reports"],
  all: null,
};

export const TITLES: Record<string, string> = {
  dashboard: "總部管理儀表板", store: "店經理即時儀表板", reviews: "負評管理", cases: "客訴案件",
  sla: "SLA 監控中心", stores: "門市管理", causes: "負評原因分析", voice: "顧客聲量與情緒分析",
  ai: "AI 洞察中心", tasks: "改善任務管理", improve: "改善成效分析", notifications: "通知中心",
  reports: "報表中心", rules: "規則設定", org: "組織與權限", sources: "資料來源管理",
  inbox: "即時案件（正式資料）", pipeline: "AI 處理進度", approvals: "回覆審核",
};

/* ---------- 目前角色（localStorage persist，demo 切換） ---------- */
const ROLE_KEY = "wacity_admin_role";
let roleId: RoleId = (localStorage.getItem(ROLE_KEY) as RoleId) || "hq";
if (!ROLES[roleId]) roleId = "hq";

export const getRoleId = () => roleId;
export const getRole = () => ROLES[roleId];
export function setRole(id: RoleId) {
  roleId = id;
  localStorage.setItem(ROLE_KEY, id);
  bump();
}

export function allowedMenu(id: string): boolean {
  const m = MENU_ROLE[getRole().menu];
  return !m || m.includes(id);
}

/* ---------- scope（顯示層裁切，同 HTML） ---------- */
export function scopedCases(): WaCase[] {
  const r = getRole();
  let cs = CASES;
  if (r.store) cs = cs.filter((c) => c.store === r.store);
  else if (r.region) cs = cs.filter((c) => c.region === r.region);
  if (r.riskOnly) cs = cs.filter((c) => c.risk_level === "high");
  return cs;
}
/** 通知裁切：有案件裁切的角色（store/region/pr）只看得到自己案件範圍內的通知。
 *  ponytail: 通知資料沒有 recipient/store 欄位，跨門市彙總類（case_id 空）對裁切角色一律隱藏；
 *  要做到「區經理看得到區域彙總」需後端補 recipient 欄位。 */
export function scopedNotifications() {
  const cs = scopedCases();
  if (cs === CASES) return DB.notifications; // 未裁切角色（hq/cs）看全部
  const ids = new Set(cs.map((c) => c.id));
  return DB.notifications.filter((n) => n.case_id && ids.has(n.case_id));
}

export function scopedStores(): WaStore[] {
  const r = getRole();
  if (r.store) return DB.stores.filter((s) => s.store === r.store);
  if (r.region) return DB.stores.filter((s) => s.region === r.region);
  return DB.stores;
}

/** PII 遮罩 */
export function mask(v: string, type: "phone" | "email" | "name"): string {
  if (getRole().pii) return v;
  if (type === "phone") return "09XX-XXX-" + String(v).slice(-3);
  if (type === "email") {
    const p = String(v).split("@");
    return p[0].slice(0, 2) + "***@" + (p[1] ?? "");
  }
  return "***";
}
