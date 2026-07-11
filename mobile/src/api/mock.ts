/**
 * Mock adapter — 未設 EXPO_PUBLIC_API_URL 時以 bundled db.json 模擬 backend。
 * 案件狀態依 HTML PoC 的 seedStatus 演算法決定（deterministic，高風險保持進行中），
 * 狀態變更記在記憶體，重啟即還原（demo 語意）。
 */
import raw from '@/mocks/db.json';
import type { Case, CaseStatus, DB } from '@/types/domain';

const db = raw as unknown as DB;

function seedStatus(c: { id: string; risk_level: string }): CaseStatus {
  const n = parseInt(c.id.replace(/[^0-9a-f]/g, '').slice(0, 6), 16) % 100;
  if (c.risk_level === 'high') return n < 60 ? 'in_progress' : n < 85 ? 'open' : 'unassigned';
  if (c.risk_level === 'medium') {
    if (n < 18) return 'done'; if (n < 28) return 'closed'; if (n < 45) return 'in_progress';
    if (n < 58) return 'pending_customer'; if (n < 70) return 'pending_review';
    if (n < 82) return 'unassigned'; return 'open';
  }
  if (n < 32) return 'closed'; if (n < 52) return 'done';
  if (n < 62) return 'in_progress'; if (n < 70) return 'unassigned'; return 'open';
}

const cases: Case[] = db.cases.map((c) => ({ ...c, cstatus: seedStatus(c) }));
const byId = new Map(cases.map((c) => [c.id, c]));

export async function mockFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const method = init?.method ?? 'GET';
  const url = new URL(path, 'http://mock');
  const parts = url.pathname.split('/').filter(Boolean); // ['api','v1',...]

  if (method === 'POST' && url.pathname === '/api/v1/login')
    return { token: 'mock-token' } as T;

  if (method === 'GET' && url.pathname === '/api/v1/cases')
    return { cases, total: cases.length } as T;

  if (method === 'GET' && parts[2] === 'cases' && parts[3])
    return byId.get(parts[3]) as T;

  if (method === 'PATCH' && parts[2] === 'cases' && parts[4] === 'status') {
    const c = byId.get(parts[3]!);
    const { status } = JSON.parse(String(init?.body ?? '{}'));
    if (c) {
      if (status === 'open' && (c.cstatus === 'closed' || c.cstatus === 'done')) c.reopened_count++;
      c.cstatus = status;
    }
    return c as T;
  }

  if (method === 'GET' && url.pathname === '/api/v1/facets')
    return {
      brands: db.brands, regions: db.regions, stores: db.stores,
      agg: db.agg, meta: db.meta, tasks: db.tasks,
      notifications: db.notifications, insights: db.insights, sources: db.sources,
    } as T;

  if (method === 'GET' && url.pathname === '/api/v1/pipeline')
    return { recent: [] } as T;

  throw new Error(`mock: unhandled ${method} ${path}`);
}
