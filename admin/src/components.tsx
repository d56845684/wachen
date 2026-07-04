import { useEffect, useState } from "react";

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

/** SLA 倒數：mono 字體活時鐘，逾期轉紅閃爍（開放中案件才有意義） */
export function SLACountdown({ dueAt, active }: { dueAt: string; active: boolean }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [active]);

  if (!active) return <span className="sla">SLA —</span>;

  const ms = new Date(dueAt).getTime() - now;
  const overdue = ms < 0;
  const abs = Math.abs(ms);
  const h = Math.floor(abs / 3_600_000);
  const m = Math.floor((abs % 3_600_000) / 60_000);
  const s = Math.floor((abs % 60_000) / 1000);
  const pad = (n: number) => String(n).padStart(2, "0");
  const text = `${h}:${pad(m)}:${pad(s)}`;

  return (
    <span className={`sla${overdue ? " due" : ""}`}>
      {overdue ? `逾期 ${text}` : `SLA ${text}`}
    </span>
  );
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
