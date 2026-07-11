/**
 * API client — 對接 backend/cmd/api（Go）：
 *   POST  /api/v1/login
 *   GET   /api/v1/cases            GET /api/v1/cases/{id}
 *   PATCH /api/v1/cases/{id}/status
 *   POST  /api/v1/cases/{id}/replies
 *   GET   /api/v1/facets           GET /api/v1/pipeline
 *   GET   /api/v1/approvals        POST /api/v1/replies/{id}/(approve|reject)
 *
 * EXPO_PUBLIC_API_URL 未設定時走 mock adapter（bundled db.json），
 * 讓 app 跟 HTML PoC 一樣可離線展示。
 */
import { useSession } from '@/auth/session';
import { mockFetch } from './mock';

const BASE = process.env.EXPO_PUBLIC_API_URL ?? '';
export const isMock = BASE === '';

export class ApiError extends Error {
  constructor(public status: number, message: string) {
    super(message);
  }
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  if (isMock) return mockFetch<T>(path, init);

  const token = useSession.getState().token;
  const res = await fetch(BASE + path, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...init?.headers,
    },
  });
  if (res.status === 401) {
    useSession.getState().signOut();
    throw new ApiError(401, 'unauthorized');
  }
  if (!res.ok) throw new ApiError(res.status, await res.text());
  return res.json() as Promise<T>;
}
