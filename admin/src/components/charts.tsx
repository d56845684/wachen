/** 圖表 — 對應 HTML 的 barChart / donut / lineChart / kwCloud（功能性圖表不做動畫） */
import { Fragment } from "react";
import { pct } from "../lib/db";

export interface BarRow { n: string; v: number; color?: string }

export function BarChart({ rows, tot, fmt, showPct = true }: {
  rows: BarRow[]; tot?: number; fmt?: (v: number) => string; showPct?: boolean;
}) {
  const max = Math.max(...rows.map((r) => r.v), 1);
  const total = tot ?? rows.reduce((a, r) => a + r.v, 0);
  return (
    <div className="bars">
      {rows.map((r, i) => (
        <div className="brow" key={r.n}>
          <div className="n" title={r.n}>{r.n}</div>
          <div className="btrack">
            <div
              className="bfill"
              style={{
                width: `${((r.v / max) * 100).toFixed(1)}%`,
                background: r.color ?? "var(--s1)",
                animationDelay: `${Math.min(i, 8) * 30}ms`,
              }}
            />
          </div>
          <div className="v">
            {fmt ? fmt(r.v) : r.v}
            {showPct ? <span className="pct"> · {pct(r.v, total)}%</span> : null}
          </div>
        </div>
      ))}
    </div>
  );
}

export function Donut({ rows, centerB, centerS }: { rows: BarRow[]; centerB: string | number; centerS: string }) {
  const tot = rows.reduce((a, r) => a + r.v, 0) || 1;
  let acc = 0;
  const segs = rows.map((r) => {
    const st = (acc / tot) * 360;
    const en = ((acc + r.v) / tot) * 360;
    acc += r.v;
    return `${r.color} ${st}deg ${en}deg`;
  });
  return (
    <div className="donut-wrap">
      <div className="donut" style={{ background: `conic-gradient(${segs.join(",")})` }}>
        <div className="donut-c"><b>{centerB}</b><span>{centerS}</span></div>
      </div>
      <div className="legend">
        {rows.map((r) => (
          <div className="li" key={r.n}>
            <i style={{ background: r.color }} />{r.n}
            <span className="v">{r.v} · {pct(r.v, tot)}%</span>
          </div>
        ))}
      </div>
    </div>
  );
}

export interface LineSeries { label: string; color: string; data: number[] }

export function LineChart({ series, labels, h = 180 }: { series: LineSeries[]; labels: string[]; h?: number }) {
  const W = 640, pl = 34, pr = 12, pt = 12, pb = 24;
  const n = labels.length;
  const allv = series.flatMap((s) => s.data);
  const mx = Math.max(...allv, 1);
  const mn = Math.min(0, ...allv);
  const X = (i: number) => pl + (W - pl - pr) * (n <= 1 ? 0 : i / (n - 1));
  const Y = (v: number) => pt + (h - pt - pb) * (1 - (v - mn) / (mx - mn || 1));
  return (
    <>
      <svg className="linechart" viewBox={`0 0 ${W} ${h}`} preserveAspectRatio="xMidYMid meet">
        {[0, 0.25, 0.5, 0.75, 1].map((f) => {
          const y = pt + (h - pt - pb) * f;
          return (
            <Fragment key={f}>
              <line className="lc-axis" x1={pl} y1={y} x2={W - pr} y2={y} />
              <text className="lc-txt" x={4} y={y + 3}>{Math.round(mx - (mx - mn) * f)}</text>
            </Fragment>
          );
        })}
        {series.map((s) => (
          <Fragment key={s.label}>
            {/* pathLength=1 正規化路徑長，描線動畫不用量測 */}
            <polyline fill="none" stroke={s.color} strokeWidth={2} pathLength={1} points={s.data.map((v, i) => `${X(i)},${Y(v)}`).join(" ")} />
            {s.data.map((v, i) => <circle key={i} cx={X(i)} cy={Y(v)} r={2.6} fill={s.color} />)}
          </Fragment>
        ))}
        {labels.map((l, i) =>
          n > 8 && i % 2 ? null : (
            <text key={i} className="lc-txt" x={X(i)} y={h - 8} textAnchor="middle">{l}</text>
          ),
        )}
      </svg>
      {series.length > 1 ? (
        <div className="legend" style={{ flexDirection: "row", gap: 14, marginTop: 6 }}>
          {series.map((s) => (
            <div className="li" key={s.label}><i style={{ background: s.color }} />{s.label}</div>
          ))}
        </div>
      ) : null}
    </>
  );
}

export function KwCloud({ list, tone }: { list: [string, number][]; tone: "neg" | "pos" }) {
  const max = Math.max(...list.map((k) => k[1]), 1);
  const base = tone === "neg" ? "var(--critical)" : "var(--good)";
  return (
    <div className="kwcloud">
      {list.slice(0, 30).map(([k, v]) => (
        <span
          key={k}
          title={`${v} 次`}
          style={{
            fontSize: `${(13 + (v / max) * 13).toFixed(0)}px`,
            background: `color-mix(in srgb,${base} ${10 + (v / max) * 30}%,transparent)`,
            color: "var(--ink-2)",
          }}
        >
          {k}
        </span>
      ))}
    </div>
  );
}
