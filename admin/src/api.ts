// API 客戶端：JWT 存 localStorage；401 一律導回登入
const TOKEN_KEY = "wachen_token";
const NAME_KEY = "wachen_name";

export const auth = {
  token: () => localStorage.getItem(TOKEN_KEY),
  name: () => localStorage.getItem(NAME_KEY) ?? "",
  save: (token: string, name: string) => {
    localStorage.setItem(TOKEN_KEY, token);
    localStorage.setItem(NAME_KEY, name);
  },
  clear: () => {
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(NAME_KEY);
  },
};

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(init.headers as Record<string, string>),
  };
  const token = auth.token();
  if (token) headers.Authorization = `Bearer ${token}`;

  const resp = await fetch(`/api/v1${path}`, { ...init, headers });
  if (resp.status === 401 && !path.startsWith("/login")) {
    auth.clear();
    window.location.href = "/login";
    throw new Error("unauthorized");
  }
  const data = await resp.json().catch(() => ({}));
  if (!resp.ok) throw new Error((data as { error?: string }).error ?? `HTTP ${resp.status}`);
  return data as T;
}

export interface CaseSummary {
  id: string;
  risk_level: "high" | "medium" | "low";
  status: "open" | "in_progress" | "resolved" | "closed";
  sla_due_at: string;
  sla_reminded: boolean;
  reopened_count: number;
  created_at: string;
  store_name: string;
  source_name: string;
  source_url: string;
  rating: number | null;
  summary: string;
  sentiment: string;
  categories: string[];
  posted_at: string | null;
}

export interface Notification {
  channel: string;
  recipient: string;
  subject: string;
  status: "pending" | "sent" | "failed";
  sent_at: string | null;
}

export interface CaseDetail extends CaseSummary {
  review_content: string;
  author_name: string;
  keywords: string[];
  risk_reasons: string[];
  sentiment_score: number | null;
  model_name: string;
  prompt_version: string;
  assignments: string[];
  notifications: Notification[];
}

export interface Facet {
  value: string;
  label: string;
  count: number;
}

export interface CaseFilters {
  risk: string;
  status: string;
  store: string;
  source: string;
}

export interface RecentAnalysis {
  review_id: string;
  store_name: string;
  source_name: string;
  risk_level: "high" | "medium" | "low";
  sentiment: string;
  model_name: string;
  latency_ms: number | null;
  summary: string;
  created_at: string;
  fallback: boolean;
}

export interface PipelineStats {
  funnel: {
    raw_reviews: number;
    reviews: number;
    awaiting_analysis: number;
    analyzed: number;
    awaiting_routing: number;
    cased: number;
  };
  ai: {
    models: string[];
    total_analyses: number;
    avg_latency_ms: number;
    max_latency_ms: number;
    quarantine_count: number;
    last_5min: number;
    last_hour: number;
    fallback_count: number;
  };
  risk: Facet[];
  sentiment: Facet[];
  recent: RecentAnalysis[];
}

export const api = {
  login: (email: string, password: string) =>
    request<{ token: string; name: string }>("/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    }),
  listCases: (f: CaseFilters) => {
    const q = new URLSearchParams({ ...f }).toString();
    return request<{ cases: CaseSummary[] }>(`/cases?${q}`);
  },
  facets: () => request<{ stores: Facet[]; sources: Facet[] }>("/facets"),
  pipeline: () => request<PipelineStats>("/pipeline"),
  caseDetail: (id: string) => request<CaseDetail>(`/cases/${id}`),
  updateStatus: (id: string, status: string) =>
    request<{ status: string }>(`/cases/${id}/status`, {
      method: "PATCH",
      body: JSON.stringify({ status }),
    }),
};
