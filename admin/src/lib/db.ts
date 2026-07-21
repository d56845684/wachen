/**
 * PoC 資料層 — 完全對應 瓦城顧客體驗中台.html 的行為：
 * bundled db.json + seedStatus（deterministic 案件狀態）+ localStorage 覆寫。
 * 全域 app 狀態（角色 / 篩選 / drawer / 通知已讀）走極簡 pub-sub，
 * 元件以 useApp() 訂閱後直接讀模組內 mutable state（等同 HTML 的 rerender()）。
 */
import { useSyncExternalStore } from "react";
import raw from "../data/db.json";

/* ---------- types（對齊 db.json schema） ---------- */
export type RiskLevel = "high" | "medium" | "low";
export type Sentiment = "negative" | "neutral" | "positive";
export type CaseStatus =
  | "unassigned" | "open" | "in_progress" | "pending_review"
  | "pending_customer" | "done" | "closed" | "canceled";

export interface WaCase {
  id: string; risk_level: RiskLevel; status: string; reopened_count: number;
  sentiment: Sentiment; sentiment_score: number | null; rating: number;
  summary: string; categories: string[]; keywords: string[]; risk_reasons: string[];
  review_content: string; author_name: string; posted_at: string; created_at: string;
  sla_due_at: string; assignments: string[];
  notifications: { channel: string; recipient: string; subject: string; status: string }[];
  model_name: string; prompt_version: string; source_url: string;
  store: string; brand: string; brand_short: string; region: string; city: string;
  store_code: string; platform: string; assignee: string; priority: string;
  weekday: number; hour: number; is_peak: boolean; is_weekend: boolean;
  first_response_min: number | null; resolution_hr: number | null;
  has_image: boolean; store_reply: string | null;
  // client 端附加
  cstatus: CaseStatus; code: string; read: boolean; escalated: boolean;
}

export interface WaStore {
  store: string; brand: string; brand_short: string; code: string; region: string;
  city: string; manager: string; maps_url: string; biz_status: string;
  total: number; neg: number; high: number; avg_rating: number; neg_rate: number;
  open_cases: number; sla_rate: number; risk_status: RiskLevel; trend: number;
  avg_handle_hr: number;
}

export interface WaDB {
  meta: { generated_ref: string; date_min: string; date_max: string; n_cases: number; n_stores: number; n_brands: number; source: string };
  cases: WaCase[];
  stores: WaStore[];
  brands: { name: string; total: number; neg: number; high: number; stores: number; avg_rating: number; neg_rate: number; sla_rate: number }[];
  regions: { name: string; total: number; neg: number; high: number; stores: number; avg_rating: number; neg_rate: number; sla_rate: number }[];
  agg: {
    monthly: { month: string; reviews: number; neg: number; avg_rating: number }[];
    category: [string, number][]; sentiment: [string, number][];
    star: Record<string, number>; risk: [string, number][];
    weekday: number[]; hour: number[];
    kw_neg: [string, number][]; kw_pos: [string, number][];
    kpis: { new_reviews: number; sla_rate: number; first_resp_min: number; resolve_hr: number; revisit_rate: number };
  };
  tasks: { id: string; name: string; category: string; store: string; brand: string; region: string; owner: string; collab: string; start: string; due: string; priority: string; status: string; kpi: string; verify: string; progress: number; source: string }[];
  improve: { rows: { item: string; before: string; after: string; delta: string; good?: boolean }[]; store_rank: { store: string; delta: number }[] };
  notifications: { id: string; type: string; channel: string; title: string; body: string; case_id: string | null; time: string; read: boolean; level: "critical" | "warning" | "serious" | "good" }[];
  insights: { anomalies: { t?: string; d?: string; title?: string; body?: string; sev?: string; level?: string }[]; rootcause: string; suggestions: string[]; qa: [string, string][] };
  // 燦坤租戶版（generator 產出；ponytail: 單一外部租戶用扁平 key，第三個租戶再抽結構）
  insights_tk?: WaDB["insights"];
  improve_rows_tk?: WaDB["improve"]["rows"];
  rules: {
    dispatch: { cond: string; target: string; sla: string; notify: string; escalate: string; on: boolean }[];
    sla: { risk: string; first: string; resolve: string; remind: string; notify: string; calc: string }[];
    ai_categories: { name: string; count: number; weight: string; keywords: string[] }[];
  };
  sources: { name: string; type: string; status: string; sync: string; last: string; rows: number; err: number }[];
  org: {
    roles: { role: string; scope: string; pii: boolean; ai: boolean; assign: boolean; close: boolean; export: boolean; rules: boolean }[];
    tree: { 集團: string; brands: string[]; regions: string[]; stores: number; people: number };
  };
}

export const DB = raw as unknown as WaDB;
export const AGG = DB.agg;
export const KPI = DB.agg.kpis;
export const META = DB.meta;

/* ---------- constants ---------- */
export const RISK_LABEL: Record<RiskLevel, string> = { high: "高風險", medium: "中風險", low: "低風險" };
export const RISK_RANK: Record<RiskLevel, number> = { high: 3, medium: 2, low: 1 };
export const SENT_LABEL: Record<Sentiment, string> = { negative: "負面", neutral: "中立", positive: "正面" };
export const CAT_COLORS = ["var(--s1)", "var(--s2)", "var(--s3)", "var(--s4)", "var(--s5)", "var(--s6)", "var(--s7)", "var(--s8)"];
export const RISK_COLOR: Record<RiskLevel, string> = { high: "var(--critical)", medium: "var(--warning)", low: "var(--good)" };
export const SENT_COLOR: Record<Sentiment, string> = { negative: "var(--critical)", neutral: "var(--muted)", positive: "var(--good)" };
export const WEEKDAY = ["週一", "週二", "週三", "週四", "週五", "週六", "週日"];

export const CASE_STATUS: Record<CaseStatus, string> = {
  unassigned: "待分派", open: "待處理", in_progress: "處理中", pending_review: "待主管確認",
  pending_customer: "待顧客回覆", done: "已完成", closed: "已結案", canceled: "已取消",
};

/** 案件狀態機：[目標狀態, 按鈕文字, 樣式] */
export const CASE_TRANS: Record<CaseStatus, [CaseStatus, string, string][]> = {
  unassigned: [["open", "指派受理", "pri"]],
  open: [["in_progress", "開始處理", "pri"], ["canceled", "取消案件", ""]],
  in_progress: [["pending_customer", "待顧客回覆", ""], ["pending_review", "送主管確認", ""], ["done", "標記完成", "ok"]],
  pending_customer: [["in_progress", "顧客已回覆", "pri"], ["done", "標記完成", "ok"]],
  pending_review: [["done", "主管核准", "ok"], ["in_progress", "退回重辦", ""]],
  done: [["closed", "結案", "pri"], ["in_progress", "重新處理", ""]],
  closed: [["open", "回開", ""]],
  canceled: [["open", "重新啟用", ""]],
};

/* ---------- helpers ---------- */
export const fmtD = (s?: string | null) => (s ? String(s).slice(0, 10) : "");
export const fmtDT = (s?: string | null) => (s ? String(s).slice(0, 16).replace("T", " ") : "");
export const pct = (a: number, b: number) => (b ? Math.round((a / b) * 100) : 0);
export const sum = <T,>(a: T[], f: (x: T) => number) => a.reduce((x, c) => x + f(c), 0);
export const cnt = <T,>(a: T[], f: (x: T) => boolean) => a.filter(f).length;
export const isActive = (c: Pick<WaCase, "cstatus">) => !["done", "closed", "canceled"].includes(c.cstatus);

/* ---------- case state（seedStatus + localStorage 覆寫，同 HTML） ---------- */
const LS = "wacity_admin_v1";
let ov: Record<string, { s?: CaseStatus; read?: boolean }> = {};
try { ov = JSON.parse(localStorage.getItem(LS) ?? "{}") ?? {}; } catch { /* ignore */ }

function seedStatus(c: { id: string; risk_level: RiskLevel }): CaseStatus {
  const saved = ov[c.id]?.s;
  if (saved) return saved;
  const n = parseInt(c.id.replace(/[^0-9a-f]/g, "").slice(0, 6), 16) % 100;
  if (c.risk_level === "high") return n < 60 ? "in_progress" : n < 85 ? "open" : "unassigned";
  if (c.risk_level === "medium") {
    if (n < 18) return "done"; if (n < 28) return "closed"; if (n < 45) return "in_progress";
    if (n < 58) return "pending_customer"; if (n < 70) return "pending_review";
    if (n < 82) return "unassigned"; return "open";
  }
  if (n < 32) return "closed"; if (n < 52) return "done";
  if (n < 62) return "in_progress"; if (n < 70) return "unassigned"; return "open";
}

export const CASES: WaCase[] = DB.cases.map((c) => {
  const s = seedStatus(c);
  return {
    ...c, cstatus: s,
    code: "C-" + c.id.slice(0, 6).toUpperCase(),
    read: ov[c.id]?.read ?? false,
    escalated: c.risk_level === "high" && s === "in_progress",
  };
});
export const byId: Record<string, WaCase> = Object.fromEntries(CASES.map((c) => [c.id, c]));

function persist() {
  const o: typeof ov = {};
  CASES.forEach((c) => { o[c.id] = { s: c.cstatus, read: c.read }; });
  try { localStorage.setItem(LS, JSON.stringify(o)); } catch { /* ignore */ }
}

/* ---------- 極簡全域 store ---------- */
export interface ReviewFilters {
  q: string; brand: string; store: string; risk: RiskLevel | "";
  sent: Sentiment | ""; status: CaseStatus | ""; cat: string; overdue: "" | "1";
}
export const rf: ReviewFilters = { q: "", brand: "", store: "", risk: "", sent: "", status: "", cat: "", overdue: "" };
export function clearFilters() {
  Object.assign(rf, { q: "", brand: "", store: "", risk: "", sent: "", status: "", cat: "", overdue: "" });
  bump();
}

/** drawer：案件 id 或 "store:門市名"；null = 關閉 */
export let openId: string | null = null;
export function openCase(id: string) {
  const c = byId[id];
  if (!c) return;
  c.read = true;
  persist();
  openId = id;
  document.body.style.overflow = "hidden";
  bump();
}
export function openStore(name: string) {
  openId = "store:" + name;
  document.body.style.overflow = "hidden";
  bump();
}
export function closeDrawer() {
  openId = null;
  document.body.style.overflow = "";
  bump();
}

export function setStatus(id: string, to: CaseStatus) {
  const c = byId[id];
  if (!c) return;
  if (to === "open" && (c.cstatus === "closed" || c.cstatus === "done")) c.reopened_count++;
  c.cstatus = to;
  c.escalated = c.risk_level === "high" && to === "in_progress";
  persist();
  bump();
}

export function markRead(nid: string) {
  const n = DB.notifications.find((x) => x.id === nid);
  if (n) n.read = true;
  bump();
}
export function markAllRead(ids?: string[]) {
  const set = ids && new Set(ids);
  DB.notifications.forEach((n) => { if (!set || set.has(n.id)) n.read = true; });
  bump();
}

/* ---------- pub-sub ---------- */
let version = 0;
const listeners = new Set<() => void>();
export function bump() { version++; listeners.forEach((l) => l()); }
export function useApp() {
  return useSyncExternalStore(
    (cb) => { listeners.add(cb); return () => { listeners.delete(cb); }; },
    () => version,
  );
}

/* ---------- 共用秒針（SLA 倒數）：單一 interval 廣播 ---------- */
let now = Date.now();
const tickers = new Set<() => void>();
setInterval(() => { now = Date.now(); tickers.forEach((l) => l()); }, 1000);
export function useNow() {
  return useSyncExternalStore(
    (cb) => { tickers.add(cb); return () => { tickers.delete(cb); }; },
    () => now,
  );
}
