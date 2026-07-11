/**
 * 案件狀態機 — 由 HTML PoC 的 CASE_TRANS 移植。
 * 每個狀態列出可執行的轉移：[目標狀態, 按鈕文字, 樣式]。
 * UI 只能呈現這裡列出的動作；狀態變更一律走 PATCH /cases/{id}/status。
 */
import type { CaseStatus } from '@/types/domain';

export type TransitionStyle = 'primary' | 'ok' | 'plain';

export interface Transition {
  to: CaseStatus;
  label: string;
  style: TransitionStyle;
}

export const CASE_TRANSITIONS: Record<CaseStatus, Transition[]> = {
  unassigned: [{ to: 'open', label: '指派受理', style: 'primary' }],
  open: [
    { to: 'in_progress', label: '開始處理', style: 'primary' },
    { to: 'canceled', label: '取消案件', style: 'plain' },
  ],
  in_progress: [
    { to: 'pending_customer', label: '待顧客回覆', style: 'plain' },
    { to: 'pending_review', label: '送主管確認', style: 'plain' },
    { to: 'done', label: '標記完成', style: 'ok' },
  ],
  pending_customer: [
    { to: 'in_progress', label: '顧客已回覆', style: 'primary' },
    { to: 'done', label: '標記完成', style: 'ok' },
  ],
  pending_review: [
    { to: 'done', label: '主管核准', style: 'ok' },
    { to: 'in_progress', label: '退回重辦', style: 'plain' },
  ],
  done: [
    { to: 'closed', label: '結案', style: 'primary' },
    { to: 'in_progress', label: '重新處理', style: 'plain' },
  ],
  closed: [{ to: 'open', label: '回開', style: 'plain' }],
  canceled: [{ to: 'open', label: '重新啟用', style: 'plain' }],
};

export const canTransition = (from: CaseStatus, to: CaseStatus) =>
  CASE_TRANSITIONS[from]?.some((t) => t.to === to) ?? false;
