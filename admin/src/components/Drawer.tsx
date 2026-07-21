/**
 * 右側 drawer — 案件詳情（renderCaseDrawer）與門市詳情（openStore）的 React 移植。
 * overlay 永遠掛載，以 .show 切換 transition（右進右出、可中斷、出場比進場快）。
 */
import { useEffect } from "react";
import {
  byId, CASE_STATUS, CASE_TRANS, CASES, CAT_COLORS, closeDrawer, cnt, DB,
  fmtD, fmtDT, isActive, openCase, openId, RISK_LABEL, SENT_LABEL, setStatus, useApp,
} from "../lib/db";
import { getRole, mask } from "../lib/roles";
import { BarChart } from "./charts";
import { Kpi, pocAlert, RiskBadge, Stars, StatusPill } from "./ui";

export function DrawerHost() {
  useApp();
  const show = openId != null;

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") closeDrawer(); };
    if (show) document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [show]);

  const isStore = openId?.startsWith("store:");
  return (
    <div
      className={`overlay ${show ? "show" : ""}`}
      onClick={(e) => { if (e.target === e.currentTarget) closeDrawer(); }}
    >
      <div className="drawer">
        {show && (isStore ? <StoreDrawer name={openId!.slice(6)} /> : <CaseDrawer id={openId!} />)}
      </div>
    </div>
  );
}

function CaseDrawer({ id }: { id: string }) {
  const c = byId[id];
  if (!c) return null;
  const acts = CASE_TRANS[c.cstatus] ?? [];
  const sc = c.sentiment_score;
  const scPos = sc == null ? 50 : ((sc + 1) / 2) * 100;
  const isFood =
    c.categories.includes("環境清潔") ||
    (c.risk_reasons ?? []).some((r) => r.includes("食安") || r.includes("不新鮮") || r.includes("異物"));
  const phone = "0912-345-" + c.id.slice(0, 3).replace(/\D/g, "0").padEnd(3, "0");
  const email = (c.author_name || "guest").replace(/\s/g, "").toLowerCase().slice(0, 6) + "@example.com";
  const role = getRole();

  const timeline: { t: string; b: string; who: string }[] = [
    { t: fmtDT(c.posted_at), b: "顧客張貼評論", who: c.platform },
    { t: fmtDT(c.created_at), b: `AI 判定完成（${RISK_LABEL[c.risk_level]}）`, who: `${c.model_name} · ${c.prompt_version}` },
    { t: fmtDT(c.created_at), b: `系統自動分派 → ${c.assignee}`, who: "派工規則" },
  ];
  if (c.cstatus !== "unassigned") timeline.push({ t: fmtDT(c.created_at), b: "案件受理", who: c.assignee });
  if (["in_progress", "pending_review", "pending_customer", "done", "closed"].includes(c.cstatus))
    timeline.push({ t: fmtDT(c.created_at), b: "開始處理", who: c.assignee });
  if (["done", "closed"].includes(c.cstatus))
    timeline.push({ t: fmtDT(c.sla_due_at), b: "案件完成", who: c.assignee });

  return (
    <>
      <div className="dhead">
        <button className="btn sm" onClick={closeDrawer}>← 返回</button>
        <RiskBadge level={c.risk_level} />
        <StatusPill status={c.cstatus} />
        {c.escalated ? <RiskBadge level="high" label="已升級" /> : null}
        <span style={{ marginLeft: "auto", fontVariantNumeric: "tabular-nums", color: "var(--muted)", fontSize: 12 }}>{c.code}</span>
      </div>
      <div className="dbody">
        <div className="dtitle">{c.store}</div>
        <div className="dmeta">
          <Stars n={c.rating} />
          <span>{c.brand_short} · {c.region}</span>
          <span>評論 {fmtD(c.posted_at)}</span>
          <span>{c.platform}</span>
          <span>負責人 {c.assignee}</span>
        </div>

        <div className="actbar">
          <span className="lbl">操作</span>
          {acts.length
            ? acts.map(([to, label, style]) => (
                <button key={to} className={`act ${style}`} onClick={() => setStatus(id, to)}>{label}</button>
              ))
            : <span className="note">此狀態無後續操作</span>}
          <span style={{ marginLeft: "auto", display: "flex", gap: 6 }}>
            <button className="act" onClick={() => pocAlert("轉派")}>轉派</button>
            <button className="act" onClick={() => pocAlert("升級")}>升級</button>
            <button className="act" onClick={() => pocAlert("標記誤判")}>標記誤判</button>
          </span>
        </div>

        <div className="sect">
          <h4>① 原始評論{c.has_image ? <> · <span className="tag">含圖片</span></> : null}</h4>
          <div className="dmeta" style={{ marginBottom: 8 }}>
            <span>顧客：{c.author_name || "匿名"}</span><Stars n={c.rating} /><span>{fmtDT(c.posted_at)}</span>
          </div>
          <div className="review-box">{c.review_content || "（純星等，無文字內容）"}</div>
          {c.store_reply ? (
            <div style={{ marginTop: 12, padding: "10px 12px", background: "var(--page)", borderRadius: 9, border: "1px solid var(--grid)" }}>
              <div className="note" style={{ marginBottom: 4 }}>門市回覆</div>
              {c.store_reply}
            </div>
          ) : (
            <div className="note" style={{ marginTop: 10 }}>門市尚未回覆</div>
          )}
          {c.source_url ? (
            <div style={{ marginTop: 10 }}>
              <a href={c.source_url} target="_blank" rel="noopener noreferrer">原始評論連結 ↗</a>
            </div>
          ) : null}
        </div>

        <div className="sect">
          <h4>② AI 分析結果（{c.model_name} · {c.prompt_version}）</h4>
          <dl className="kv">
            <dt>情緒判定</dt><dd><span className={`sent ${c.sentiment}`}>{SENT_LABEL[c.sentiment]}</span></dd>
            <dt>情緒分數</dt>
            <dd>
              {sc == null ? "—" : sc}
              <div className="btrack" style={{ marginTop: 5 }}>
                <div className="bfill" style={{ width: `${scPos}%`, background: sc != null && sc <= -0.3 ? "var(--critical)" : sc != null && sc >= 0.3 ? "var(--good)" : "var(--warning)" }} />
              </div>
            </dd>
            <dt>問題類型</dt>
            <dd className="chips">{c.categories.length ? c.categories.map((x) => <span key={x} className="tag">{x}</span>) : "—"}</dd>
            <dt>風險等級</dt><dd><RiskBadge level={c.risk_level} /></dd>
            <dt>疑似食安</dt><dd>{isFood ? <span style={{ color: "var(--critical)", fontWeight: 700 }}>是</span> : "否"}</dd>
            <dt>疑似法律/公關風險</dt><dd>{c.risk_level === "high" ? <span style={{ color: "var(--serious)", fontWeight: 700 }}>是</span> : "否"}</dd>
          </dl>
          {c.keywords.length ? (
            <>
              <h4 style={{ marginTop: 14 }}>關鍵字</h4>
              <div className="chips">{c.keywords.map((k) => <span key={k} className="tag kw">{k}</span>)}</div>
            </>
          ) : null}
          {c.risk_reasons.length ? (
            <>
              <h4 style={{ marginTop: 14 }}>AI 風險判讀 / 建議依據</h4>
              <ul className="reasons">{c.risk_reasons.map((r) => <li key={r}>{r}</li>)}</ul>
            </>
          ) : null}
          <h4 style={{ marginTop: 14 }}>建議處理方式</h4>
          <p style={{ margin: 0, color: "var(--ink-2)" }}>
            {isFood
              ? "立即聯繫顧客了解狀況、保留餐點與現場紀錄，通報食安窗口與公關法務，並於 2 小時內回應。"
              : c.risk_level === "medium"
                ? "由區經理與店主管於 24 小時內聯繫顧客致歉、了解細節並提出補償或改善說明。"
                : "由店經理於 48 小時內回覆致謝／致歉，記錄問題並納入門市改善。"}
          </p>
        </div>

        <div className={`sect ${role.pii ? "" : "hidewrap"}`}>
          <h4>③ 顧客資訊 {role.pii ? "" : "· 個資已依權限遮罩"}</h4>
          <dl className="kv">
            <dt>顧客名稱</dt><dd>{c.author_name || "匿名"}</dd>
            <dt>電話</dt><dd className="masked">{mask(phone, "phone")}</dd>
            <dt>Email</dt><dd>{mask(email, "email")}</dd>
            <dt>會員資料</dt><dd className="note">POC 未串接 CRM／會員（第二階段開放）</dd>
          </dl>
        </div>

        <div className="sect">
          <h4>④ 處理流程時間軸</h4>
          <div className="timeline">
            {timeline.map((t, i) => (
              <div className="tl-item" key={i}>
                <div className="tt">{t.t} · {t.who}</div>
                <div className="tb">{t.b}</div>
              </div>
            ))}
          </div>
        </div>

        <div className="sect">
          <h4>指派與通知</h4>
          <div className="chips" style={{ marginBottom: 8 }}>
            {(c.assignments ?? []).length ? c.assignments.map((a) => <span key={a} className="tag route">{a}</span>) : "—"}
          </div>
          {(c.notifications ?? []).length ? (
            <>
              <h4>通知紀錄（{c.notifications.length}）</h4>
              {c.notifications.map((n, i) => (
                <div key={i} style={{ fontSize: 12.5, padding: "7px 10px", border: "1px solid var(--grid)", borderRadius: 8, marginBottom: 6 }}>
                  <b>{n.channel} → {n.recipient}</b> · <span style={{ color: "var(--good)" }}>{n.status}</span>
                  <br />{n.subject}
                </div>
              ))}
            </>
          ) : (
            <div className="note">尚無通知</div>
          )}
        </div>

        <div className="sect">
          <h4>補償方案 / 結案資訊 <span className="note">（示範表單）</span></h4>
          <dl className="kv">
            <dt>補償類型</dt><dd>優惠券 / 贈品 / 重新招待（待填）</dd>
            <dt>根本原因</dt><dd className="note">待填寫</dd>
            <dt>改善措施</dt><dd className="note">待填寫</dd>
            <dt>是否需回訪</dt><dd>—</dd>
          </dl>
        </div>
      </div>
    </>
  );
}

function StoreDrawer({ name }: { name: string }) {
  const s = DB.stores.find((x) => x.store === name);
  if (!s) return null;
  const cs = CASES.filter((c) => c.store === name);
  const catC: Record<string, number> = {};
  cs.forEach((c) => c.categories.forEach((x) => { catC[x] = (catC[x] ?? 0) + 1; }));
  const catRows = Object.entries(catC).sort((a, b) => b[1] - a[1]).map((x, i) => ({ n: x[0], v: x[1], color: CAT_COLORS[i % 8] }));
  const hourRows = [];
  for (let h = 11; h <= 21; h++)
    hourRows.push({ n: h + ":00", v: cs.filter((c) => c.hour === h && c.sentiment === "negative").length, color: "var(--s1)" });
  const wknd = cnt(cs, (c) => c.is_weekend);
  const wkday = cs.length - wknd;
  const regionStores = DB.stores.filter((x) => x.region === s.region);
  const rank = [...regionStores].sort((a, b) => a.neg - b.neg).findIndex((x) => x.store === name) + 1;
  const open = cs.filter(isActive);

  return (
    <>
      <div className="dhead">
        <button className="btn sm" onClick={closeDrawer}>← 返回</button>
        <RiskBadge level={s.risk_status} />
        <span style={{ marginLeft: "auto", color: "var(--muted)", fontSize: 12 }}>{s.code}</span>
      </div>
      <div className="dbody">
        <div className="dtitle">{s.store}</div>
        <div className="dmeta">
          <span>{s.brand}</span><span>{s.region} · {s.city}</span><span>店經理 {s.manager}</span><span>{s.biz_status}</span>
        </div>
        <div className="kpis" style={{ marginBottom: 14 }}>
          <Kpi v={s.avg_rating} l="平均評分" delta={s.trend} />
          <Kpi v={s.total} l="評論總數" />
          <Kpi v={s.neg_rate} unit="%" l="負評率" cls="alarm" />
          <Kpi v={s.open_cases} l="未結案件" />
          <Kpi v={s.sla_rate} unit="%" l="SLA 達成" synth />
          <Kpi v={`${rank}/${regionStores.length}`} l="區域排名" />
        </div>
        <div className="sect">
          <h4>AI 門市摘要</h4>
          <p style={{ margin: 0, lineHeight: 1.8 }}>
            本門市共 {s.total} 則評論，平均評分 {s.avg_rating}
            {s.trend < 0 ? `，較前期下降 ${Math.abs(s.trend)} 分` : "，表現穩定"}。
            主要負評來源為「{catRows[0]?.n ?? "—"}」{catRows[1] ? `與「${catRows[1].n}」` : ""}，
            且集中於{wknd >= wkday ? "週末" : "平日"}用餐時段。
            建議{s.risk_status === "high" ? "由區經理介入、優先檢查食材品質與尖峰人力配置" : "持續追蹤服務與出餐流程"}。
          </p>
        </div>
        <div className="sect"><h4>各類問題分布</h4><BarChart rows={catRows} /></div>
        <div style={{ display: "grid", gap: 12, gridTemplateColumns: "1fr 1fr" }}>
          <div className="sect"><h4>時段分析（負評）</h4><BarChart rows={hourRows} showPct={false} /></div>
          <div className="sect">
            <h4>平日 / 假日</h4>
            <BarChart rows={[{ n: "平日", v: wkday, color: "var(--s1)" }, { n: "假日", v: wknd, color: "var(--warning)" }]} />
          </div>
        </div>
        <div className="sect">
          <h4>未結案件（{open.length}）</h4>
          {open.length ? open.slice(0, 8).map((c) => (
            <div
              key={c.id}
              style={{ display: "flex", gap: 8, alignItems: "center", padding: "7px 0", borderBottom: "1px solid var(--grid)", cursor: "pointer" }}
              onClick={() => openCase(c.id)}
            >
              <RiskBadge level={c.risk_level} />
              <span style={{ flex: 1, fontSize: 13 }}>{c.summary.slice(0, 40)}</span>
              <span className={`pill st-${c.cstatus}`}>{CASE_STATUS[c.cstatus]}</span>
            </div>
          )) : <div className="note">無未結案件</div>}
        </div>
      </div>
    </>
  );
}
