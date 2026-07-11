/** 共用 UI 元件 — 對應 HTML 的 kpiCard / badge / pill / stars / slaCell / alertRow */
import { ReactNode } from "react";
import {
  CASE_STATUS, RISK_LABEL, isActive, useNow,
  type CaseStatus, type RiskLevel, type WaCase,
} from "../lib/db";

export function Kpi(props: {
  v: ReactNode; unit?: string; l: string; s?: ReactNode; delta?: number;
  deltaUnit?: string; cls?: string; synth?: boolean; onGo?: () => void;
}) {
  const { v, unit, l, s, delta, deltaUnit = "", cls = "", synth, onGo } = props;
  return (
    <div className={`kpi ${cls} ${onGo ? "click" : ""}`} onClick={onGo}>
      <div className="v">
        {v}
        {unit ? <small>{unit}</small> : null}
        {synth ? <span className="synth">模擬</span> : null}
      </div>
      <div className="l">{l}</div>
      {delta != null ? (
        <div className={`d ${delta >= 0 ? "up" : "down"}`}>
          {delta >= 0 ? "▲" : "▼"} {Math.abs(delta)}{deltaUnit}
        </div>
      ) : null}
      {s ? <div className="s">{s}</div> : null}
    </div>
  );
}

export function RiskBadge({ level, label }: { level: RiskLevel | string; label?: string }) {
  return <span className={`badge b-${level}`}>{label ?? RISK_LABEL[level as RiskLevel] ?? level}</span>;
}

export function StatusPill({ status }: { status: CaseStatus }) {
  return <span className={`pill st-${status}`}>{CASE_STATUS[status]}</span>;
}

export function Stars({ n }: { n: number }) {
  const full = n || 0;
  return (
    <span className="stars">
      {"★".repeat(full)}
      <span className="off">{"★".repeat(5 - full)}</span>
    </span>
  );
}

/** SLA 倒數 — 共用秒針，逾期紅 / 一小時內黃 */
export function Sla({ c }: { c: WaCase }) {
  const now = useNow();
  if (!isActive(c)) return <span className="sla done">已結束</span>;
  const due = Date.parse(c.sla_due_at);
  if (Number.isNaN(due)) return <span className="sla done">—</span>;
  const diff = due - now;
  const over = diff < 0;
  const a = Math.abs(diff);
  const h = Math.floor(a / 3.6e6);
  const m = Math.floor((a % 3.6e6) / 6e4);
  const s = Math.floor((a % 6e4) / 1e3);
  const p = (x: number) => String(x).padStart(2, "0");
  return (
    <span className={`sla ${over ? "due" : diff < 3.6e6 ? "warn" : ""}`}>
      {(over ? "逾期 " : "") + `${h}:${p(m)}:${p(s)}`}
    </span>
  );
}

/** 警示列：嚴重度只靠左側色條表達（單一編碼，不疊 icon） */
export function AlertRow(props: {
  level?: string; title: string; body: string; actions?: ReactNode; extra?: ReactNode; dim?: boolean;
}) {
  const { level = "", title, body, actions, extra, dim } = props;
  return (
    <div className={`alert ${level} ${dim ? "hidewrap" : ""}`}>
      <div style={{ flex: 1 }}>
        <div className="at">{title}</div>
        <div className="ad">{body}</div>
        {extra}
        {actions ? <div className="aa">{actions}</div> : null}
      </div>
    </div>
  );
}

export function SectionT({ children }: { children: ReactNode }) {
  return <div className="section-t">{children}</div>;
}

export function PageHeader({ title, sub, right }: { title: ReactNode; sub?: ReactNode; right?: ReactNode }) {
  return (
    <div className="ph">
      <div>
        <h1>{title}</h1>
        {sub ? <p>{sub}</p> : null}
      </div>
      {right ? <div className="r">{right}</div> : null}
    </div>
  );
}

export function SynthBar({ children }: { children: ReactNode }) {
  return <div className="synthbar">{children}</div>;
}

export function pocAlert(label: string) {
  window.alert("POC 示範：" + label + "（正式版將開啟對應功能）");
}
