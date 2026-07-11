/** Domain types — 對應 backend API 與 PoC 資料集（db.json）的 schema */

export type RiskLevel = 'high' | 'medium' | 'low';
export type Sentiment = 'negative' | 'neutral' | 'positive';

export type CaseStatus =
  | 'unassigned' | 'open' | 'in_progress' | 'pending_review'
  | 'pending_customer' | 'done' | 'closed' | 'canceled';

export interface Case {
  id: string;
  risk_level: RiskLevel;
  status: string;
  cstatus: CaseStatus;          // client 端案件狀態（PATCH 後由 server 回填）
  reopened_count: number;
  sentiment: Sentiment;
  sentiment_score: number;
  rating: number;
  summary: string;
  categories: string[];
  keywords: string[];
  risk_reasons: string[];
  review_content: string;
  author_name: string;
  posted_at: string;
  created_at: string;
  sla_due_at: string;
  model_name: string;
  prompt_version: string;
  source_url: string;
  store: string;
  brand: string;
  brand_short: string;
  region: string;
  city: string;
  store_code: string;
  platform: string;
  assignee: string;
  priority: string;
  first_response_min: number | null;
  resolution_hr: number | null;
  has_image: boolean;
  store_reply: string | null;
  read?: boolean;
}

export interface Store {
  store: string;
  brand: string;
  brand_short: string;
  code: string;
  region: string;
  city: string;
  manager: string;
  maps_url: string;
  biz_status: string;
  total: number;
  neg: number;
  high: number;
  avg_rating: number;
  neg_rate: number;
  open_cases: number;
  sla_rate: number;
  risk_status: RiskLevel;
  trend: number;
  avg_handle_hr: number;
}

export interface BrandAgg {
  name: string; total: number; neg: number; high: number;
  stores: number; avg_rating: number; neg_rate: number; sla_rate: number;
}
export interface RegionAgg extends BrandAgg {}

export interface MonthlyPoint {
  month: string; reviews: number; neg: number; avg_rating: number;
}

export interface Kpis {
  new_reviews: number; sla_rate: number; first_resp_min: number;
  resolve_hr: number; revisit_rate: number;
}

export interface Agg {
  monthly: MonthlyPoint[];
  category: [string, number][];
  sentiment: [string, number][];
  star: [string, number][];
  risk: [string, number][];
  weekday: number[];
  hour: number[];
  kw_neg: [string, number][];
  kw_pos: [string, number][];
  kpis: Kpis;
}

export interface ImproveTask {
  id: string; name: string; category: string; store: string; brand: string;
  region: string; owner: string; collab: string; start: string; due: string;
  priority: string; status: string; kpi: string; verify: string;
  progress: number; source: string;
}

export interface Notice {
  id: string; type: string; channel: string; title: string; body: string;
  case_id: string | null; time: string; read: boolean;
  level: 'critical' | 'warning' | 'serious' | 'good';
}

export interface Insight { t?: string; d?: string; title?: string; body?: string; sev?: string; level?: string }

export interface DataSource {
  name: string; type: string; status: string; sync: string;
  last: string; rows: number; err: number;
}

export interface DB {
  meta: { generated_ref: string; date_min: string; date_max: string; n_cases: number; n_stores: number; n_brands: number; source: string };
  cases: Case[];
  stores: Store[];
  brands: BrandAgg[];
  regions: RegionAgg[];
  agg: Agg;
  tasks: ImproveTask[];
  improve: { rows: unknown[]; store_rank: unknown[] };
  notifications: Notice[];
  insights: { anomalies: Insight[]; rootcause: Insight[]; suggestions: Insight[]; qa: Insight[] };
  rules: { dispatch: unknown[]; sla: unknown[]; ai_categories: string[] };
  sources: DataSource[];
  org: { roles: unknown[]; tree: unknown };
}

export const RISK_LABEL: Record<RiskLevel, string> = { high: '高風險', medium: '中風險', low: '低風險' };
export const RISK_RANK: Record<RiskLevel, number> = { high: 3, medium: 2, low: 1 };
export const SENT_LABEL: Record<Sentiment, string> = { negative: '負面', neutral: '中立', positive: '正面' };

export const CASE_STATUS_LABEL: Record<CaseStatus, string> = {
  unassigned: '待分派', open: '待處理', in_progress: '處理中', pending_review: '待主管確認',
  pending_customer: '待顧客回覆', done: '已完成', closed: '已結案', canceled: '已取消',
};

export const isActive = (c: Pick<Case, 'cstatus'>) =>
  !['done', 'closed', 'canceled'].includes(c.cstatus);
