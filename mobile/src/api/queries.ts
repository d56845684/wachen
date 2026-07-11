/**
 * React Query hooks — server state 唯一入口。
 * 元件不得直接 fetch；filter/排序等 UI state 放 zustand（state/filters.ts）。
 */
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from './client';
import { useSession } from '@/auth/session';
import { scopeCases, scopeStores } from '@/auth/roles';
import type { Agg, BrandAgg, Case, CaseStatus, DataSource, ImproveTask, Insight, Notice, RegionAgg, Store } from '@/types/domain';

export interface Facets {
  brands: BrandAgg[];
  regions: RegionAgg[];
  stores: Store[];
  agg: Agg;
  meta: { generated_ref: string };
  tasks: ImproveTask[];
  notifications: Notice[];
  insights: { anomalies: Insight[]; rootcause: Insight[]; suggestions: Insight[]; qa: Insight[] };
  sources: DataSource[];
}

export const keys = {
  cases: ['cases'] as const,
  caseDetail: (id: string) => ['cases', id] as const,
  facets: ['facets'] as const,
};

export function useLogin() {
  return useMutation({
    mutationFn: (body: { email: string; password: string }) =>
      api<{ token: string }>('/api/v1/login', { method: 'POST', body: JSON.stringify(body) }),
  });
}

/** 全部案件（依角色 scope 裁切後回傳） */
export function useCases() {
  const role = useSession((s) => s.role);
  return useQuery({
    queryKey: keys.cases,
    queryFn: () => api<{ cases: Case[] }>('/api/v1/cases'),
    select: (data) => scopeCases(role, data.cases),
    staleTime: 60_000,
  });
}

export function useCaseDetail(id: string) {
  return useQuery({
    queryKey: keys.caseDetail(id),
    queryFn: () => api<Case>(`/api/v1/cases/${id}`),
  });
}

export function useFacets() {
  const role = useSession((s) => s.role);
  return useQuery({
    queryKey: keys.facets,
    queryFn: () => api<Facets>('/api/v1/facets'),
    select: (f) => ({ ...f, stores: scopeStores(role, f.stores) }),
    staleTime: 5 * 60_000,
  });
}

/**
 * 案件狀態變更 — optimistic update：
 * 立即回饋（emil：response 是一切的基礎），失敗時 rollback + toast。
 */
export function useUpdateStatus() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, status }: { id: string; status: CaseStatus }) =>
      api<Case>(`/api/v1/cases/${id}/status`, { method: 'PATCH', body: JSON.stringify({ status }) }),
    onMutate: async ({ id, status }) => {
      await qc.cancelQueries({ queryKey: keys.cases });
      const prev = qc.getQueryData<{ cases: Case[] }>(keys.cases);
      qc.setQueryData<{ cases: Case[] }>(keys.cases, (old) =>
        old ? { cases: old.cases.map((c) => (c.id === id ? { ...c, cstatus: status } : c)) } : old,
      );
      qc.setQueryData<Case>(keys.caseDetail(id), (old) => (old ? { ...old, cstatus: status } : old));
      return { prev };
    },
    onError: (_e, _v, ctx) => ctx?.prev && qc.setQueryData(keys.cases, ctx.prev),
    onSettled: (_d, _e, { id }) => {
      qc.invalidateQueries({ queryKey: keys.cases });
      qc.invalidateQueries({ queryKey: keys.caseDetail(id) });
    },
  });
}

export function useCreateReply() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, content }: { id: string; content: string }) =>
      api(`/api/v1/cases/${id}/replies`, { method: 'POST', body: JSON.stringify({ content }) }),
    onSettled: (_d, _e, { id }) => qc.invalidateQueries({ queryKey: keys.caseDetail(id) }),
  });
}
