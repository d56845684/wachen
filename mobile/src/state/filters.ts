/** 負評/案件列表的 filter state（純 UI state，不進 React Query） */
import { create } from 'zustand';
import type { CaseStatus, RiskLevel, Sentiment } from '@/types/domain';
import { RISK_RANK, isActive } from '@/types/domain';
import type { Case } from '@/types/domain';

export interface ReviewFilters {
  q: string;
  brand: string;
  store: string;
  risk: RiskLevel | '';
  sentiment: Sentiment | '';
  status: CaseStatus | '';
  category: string;
  overdueOnly: boolean;
}

const initial: ReviewFilters = {
  q: '', brand: '', store: '', risk: '', sentiment: '', status: '', category: '', overdueOnly: false,
};

interface FilterState extends ReviewFilters {
  set: <K extends keyof ReviewFilters>(key: K, value: ReviewFilters[K]) => void;
  reset: () => void;
}

export const useFilters = create<FilterState>((set) => ({
  ...initial,
  set: (key, value) => set({ [key]: value }),
  reset: () => set(initial),
}));

export function applyFilters(cases: Case[], f: ReviewFilters, now = Date.now()): Case[] {
  return cases
    .filter((c) => {
      if (f.q) {
        const hay = (c.store + c.summary + c.review_content + c.keywords.join('') + c.author_name).toLowerCase();
        if (!hay.includes(f.q.toLowerCase())) return false;
      }
      if (f.brand && c.brand !== f.brand) return false;
      if (f.store && c.store !== f.store) return false;
      if (f.risk && c.risk_level !== f.risk) return false;
      if (f.sentiment && c.sentiment !== f.sentiment) return false;
      if (f.status && c.cstatus !== f.status) return false;
      if (f.category && !c.categories.includes(f.category)) return false;
      if (f.overdueOnly && !(isActive(c) && Date.parse(c.sla_due_at) < now)) return false;
      return true;
    })
    .sort((a, b) => RISK_RANK[b.risk_level] - RISK_RANK[a.risk_level] || b.posted_at.localeCompare(a.posted_at));
}
