/**
 * 角色 / 資料範圍 / 選單可見性 — 由 HTML PoC 的 ROLES、MENU、MENU_ROLE 移植。
 * 正式版角色與 scope 應由 POST /api/v1/login 回傳，這裡是 PoC 的本地對照表；
 * client 端過濾只影響顯示，實際資料權限仍由 backend 依 token 裁切。
 */
import type { Case, Store } from '@/types/domain';

export type RoleId = 'hq' | 'region' | 'store' | 'cs' | 'pr';
export type ViewId =
  | 'dashboard' | 'reviews' | 'cases' | 'sla' | 'stores' | 'causes' | 'voice'
  | 'ai' | 'tasks' | 'improve' | 'notifications' | 'reports' | 'rules' | 'org' | 'sources';

export interface Role {
  id: RoleId;
  name: string;
  title: string;
  avatar: string;
  scopeLabel: string | null;
  home: 'dashboard' | 'store' | 'cases' | 'reviews';
  region?: string;
  store?: string;        // login 後由 session 填入
  menu: ViewId[] | null; // null = 全部
  pii: boolean;          // 可見顧客個資；false 時電話/email 遮罩
  rules: boolean;        // 可管理規則設定
  riskOnly?: boolean;    // 只看高風險（公關/法務）
}

const ALL: ViewId[] | null = null;

export const ROLES: Record<RoleId, Role> = {
  hq: {
    id: 'hq', name: '系統管理員', title: '總部管理者', avatar: '系',
    scopeLabel: '全集團 · 全品牌 · 全門市', home: 'dashboard',
    menu: ALL, pii: true, rules: true,
  },
  region: {
    id: 'region', name: '林區經理', title: '區經理（北一區）', avatar: '林',
    scopeLabel: '北一區', home: 'dashboard', region: '北一區',
    menu: ['dashboard', 'reviews', 'cases', 'sla', 'stores', 'causes', 'voice', 'ai', 'tasks', 'improve', 'notifications', 'reports'],
    pii: true, rules: false,
  },
  store: {
    id: 'store', name: '陳店經理', title: '店經理', avatar: '陳',
    scopeLabel: null, home: 'store',
    menu: ['dashboard', 'reviews', 'cases', 'sla', 'tasks', 'notifications'],
    pii: false, rules: false,
  },
  cs: {
    id: 'cs', name: '何采潔', title: '客服人員', avatar: '何',
    scopeLabel: '指派案件', home: 'cases',
    menu: ['dashboard', 'reviews', 'cases', 'sla', 'notifications', 'tasks'],
    pii: true, rules: false,
  },
  pr: {
    id: 'pr', name: '鍾岳霖', title: '公關／法務', avatar: '鍾',
    scopeLabel: '高風險案件', home: 'reviews',
    menu: ['dashboard', 'reviews', 'cases', 'sla', 'notifications'],
    pii: true, rules: false, riskOnly: true,
  },
};

export const allowedView = (role: Role, view: ViewId) =>
  role.menu === null || role.menu.includes(view);

/** 依角色裁切案件（顯示層防呆；權威裁切在 backend） */
export function scopeCases(role: Role, cases: Case[]): Case[] {
  let cs = cases;
  if (role.store) cs = cs.filter((c) => c.store === role.store);
  else if (role.region) cs = cs.filter((c) => c.region === role.region);
  if (role.riskOnly) cs = cs.filter((c) => c.risk_level === 'high');
  return cs;
}

export function scopeStores(role: Role, stores: Store[]): Store[] {
  if (role.store) return stores.filter((s) => s.store === role.store);
  if (role.region) return stores.filter((s) => s.region === role.region);
  return stores;
}

/** PII 遮罩 — 無 pii 權限時電話/Email 打碼 */
export function maskPii(role: Role, value: string, kind: 'phone' | 'email' | 'name'): string {
  if (role.pii) return value;
  if (kind === 'phone') return '09XX-XXX-' + value.slice(-3);
  if (kind === 'email') {
    const [user, host] = value.split('@');
    return user.slice(0, 2) + '***@' + (host ?? '');
  }
  return '***';
}

/** 「更多」頁的次要功能清單（bottom tabs 放不下的） */
export const MORE_MENU: { id: ViewId; icon: string; title: string; group: string }[] = [
  { id: 'sla', icon: '⏱️', title: 'SLA 監控', group: '案件與評論' },
  { id: 'stores', icon: '🏪', title: '門市管理', group: '門市與分析' },
  { id: 'causes', icon: '🔍', title: '負評原因分析', group: '門市與分析' },
  { id: 'voice', icon: '📈', title: '顧客聲量與情緒', group: '門市與分析' },
  { id: 'ai', icon: '🤖', title: 'AI 洞察中心', group: '門市與分析' },
  { id: 'tasks', icon: '✅', title: '改善任務', group: '改善與成效' },
  { id: 'improve', icon: '📊', title: '改善成效分析', group: '改善與成效' },
  { id: 'reports', icon: '📑', title: '報表中心', group: '系統' },
  { id: 'rules', icon: '⚙️', title: '規則設定', group: '系統' },
  { id: 'org', icon: '🏢', title: '組織與權限', group: '系統' },
  { id: 'sources', icon: '🔌', title: '資料來源', group: '系統' },
];
