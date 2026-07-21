/** 角色 / 選單 / 資料範圍 — 由 瓦城顧客體驗中台.html 的 ROLES / MENU / MENU_ROLE 移植 */
import { bump, CASES, DB, type WaCase, type WaStore } from "./db";
import { auth } from "../api";

export type RoleId = "hq" | "region" | "store" | "cs" | "pr" | "tk";

export interface Role {
  name: string; title: string; av: string; scope: string | null;
  home: string; region?: string; store?: string; brand?: string;
  menu: "all" | "region" | "store" | "cs" | "pr";
  pii: boolean; rules: boolean; riskOnly?: boolean;
}

/* ponytail: 目前只有燦X一個外部品牌租戶，直接用 brand_short 硬切；
 * 第三個品牌集團出現時再抽 tenant 欄位。 */
const EXTERNAL_BRANDS = ["燦X"];

export const ROLES: Record<RoleId, Role> = {
  hq: { name: "系統管理員", title: "總部管理者", av: "系", scope: "全集團 · 全品牌 · 全門市", home: "/dashboard", menu: "all", pii: true, rules: true },
  region: { name: "林區經理", title: "區經理（北一區）", av: "林", scope: "北一區", home: "/dashboard", region: "北一區", menu: "region", pii: true, rules: false },
  store: { name: "陳店經理", title: "店經理", av: "陳", scope: null, home: "/store", store: DB.stores[0].store, menu: "store", pii: false, rules: false },
  cs: { name: "何采潔", title: "客服人員", av: "何", scope: "指派案件", home: "/cases", menu: "cs", pii: true, rules: false },
  pr: { name: "鍾岳霖", title: "公關／法務", av: "鍾", scope: "高風險案件", home: "/reviews", menu: "pr", pii: true, rules: false, riskOnly: true },
  tk: { name: "蘇志豪", title: "燦X管理者", av: "燦", scope: "燦X3C · 大台北門市", home: "/dashboard", brand: "燦X", menu: "region", pii: true, rules: false },
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

/* ---------- 帳號 ↔ 角色綁定（後端 users.role → 前端 RoleId） ---------- */
const BACKEND_ROLE: Record<string, RoleId> = {
  admin: "hq", tsannkuen: "tk",
  hq_service: "cs", pr_legal: "pr", store_manager: "store", district_manager: "region",
};
/** 登入帳號的預設角色（後端 role 未知 → 退回 hq） */
export const loginRoleId = (): RoleId => BACKEND_ROLE[auth.backendRole()] ?? "hq";
/** 依登入帳號的租戶（brand），限定可切換的角色 —— 燦坤帳號切不到瓦城角色，反之亦然 */
export function allowedRoleIds(): RoleId[] {
  const brand = ROLES[loginRoleId()].brand;
  return (Object.keys(ROLES) as RoleId[]).filter((id) => ROLES[id].brand === brand);
}

/* ---------- scope（顯示層裁切，同 HTML） ---------- */
export function scopedCases(): WaCase[] {
  const r = getRole();
  // 品牌租戶隔離：外部品牌角色只看自家，瓦城端角色一律排除外部品牌
  let cs = r.brand
    ? CASES.filter((c) => c.brand_short === r.brand)
    : CASES.filter((c) => !EXTERNAL_BRANDS.includes(c.brand_short));
  if (r.store) cs = cs.filter((c) => c.store === r.store);
  else if (r.region) cs = cs.filter((c) => c.region === r.region);
  if (r.riskOnly) cs = cs.filter((c) => c.risk_level === "high");
  return cs;
}
/** 通知裁切：案件通知跟著案件範圍走；彙總通知（case_id 空，皆為瓦城集團層級）只給集團全域角色。
 *  ponytail: 通知資料沒有 recipient/store 欄位，跨門市彙總類對裁切角色一律隱藏；
 *  要做到「區經理看得到區域彙總」需後端補 recipient 欄位。 */
export function scopedNotifications() {
  const r = getRole();
  const ids = new Set(scopedCases().map((c) => c.id));
  const full = !r.store && !r.region && !r.riskOnly && !r.brand;
  return DB.notifications.filter((n) => (n.case_id ? ids.has(n.case_id) : full));
}

/** 依角色範圍現算的統計 — 取代全域 AGG（AGG 是瓦城全集團的靜態彙總，燦坤租戶會吃錯資料）。
 *  傳入已過濾的案件列表（如 reviewFilters 結果）則統計跟著篩選連動。 */
export function scopedStats(cs: WaCase[] = scopedCases()) {
  const neg = cs.filter((c) => c.sentiment === "negative");
  const byMonth = new Map<string, { reviews: number; neg: number }>();
  for (const c of cs) {
    const m = c.posted_at.slice(0, 7);
    const e = byMonth.get(m) ?? { reviews: 0, neg: 0 };
    e.reviews++;
    if (c.sentiment === "negative") e.neg++;
    byMonth.set(m, e);
  }
  const monthly = [...byMonth.entries()].sort((a, b) => a[0].localeCompare(b[0]))
    .map(([month, v]) => ({ month, ...v }));
  const star: Record<string, number> = {};
  for (const c of cs) star[String(c.rating)] = (star[String(c.rating)] ?? 0) + 1;
  const catCnt = new Map<string, number>();
  for (const c of cs) for (const k of c.categories) catCnt.set(k, (catCnt.get(k) ?? 0) + 1);
  const category: [string, number][] = [...catCnt.entries()].sort((a, b) => b[1] - a[1]);
  const hour = Array(24).fill(0) as number[];
  const weekday = Array(7).fill(0) as number[];
  for (const c of neg) { hour[c.hour]++; weekday[c.weekday]++; }
  const kw = (list: WaCase[]): [string, number][] => {
    const m = new Map<string, number>();
    for (const c of list) for (const k of c.keywords) m.set(k, (m.get(k) ?? 0) + 1);
    return [...m.entries()].sort((a, b) => b[1] - a[1]).slice(0, 24);
  };
  return {
    monthly, star, category, hour, weekday,
    kwNeg: kw(neg), kwPos: kw(cs.filter((c) => c.sentiment === "positive")),
  };
}

export function scopedStores(): WaStore[] {
  const r = getRole();
  let ss = r.brand
    ? DB.stores.filter((s) => s.brand_short === r.brand)
    : DB.stores.filter((s) => !EXTERNAL_BRANDS.includes(s.brand_short));
  if (r.store) ss = ss.filter((s) => s.store === r.store);
  else if (r.region) ss = ss.filter((s) => s.region === r.region);
  return ss;
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
