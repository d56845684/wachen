const RISK_CHAR: Record<string, string> = { high: "高", medium: "中", low: "低" };

export function RiskSeal({ risk, small }: { risk: string; small?: boolean }) {
  return <span className={`seal ${risk}${small ? " sm" : ""}`}>{RISK_CHAR[risk] ?? "？"}</span>;
}

const STATUS_LABEL: Record<string, string> = {
  open: "待處理",
  in_progress: "處理中",
  resolved: "已解決",
  closed: "已結案",
};

export function StatusPill({ status }: { status: string }) {
  return <span className={`pill ${status}`}>{STATUS_LABEL[status] ?? status}</span>;
}

export function statusLabel(s: string) {
  return STATUS_LABEL[s] ?? s;
}

export function Stars({ rating }: { rating: number | null }) {
  if (rating == null) return null;
  const full = Math.round(rating);
  return (
    <span className="stars" title={`${rating} 星`}>
      {"★".repeat(full)}
      {"☆".repeat(Math.max(0, 5 - full))}
    </span>
  );
}
