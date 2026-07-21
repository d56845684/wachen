/** 通知中心 — 對應 PAGES.notifications */
import { fmtDT, markAllRead, markRead, openCase, useApp } from "../lib/db";
import { scopedNotifications } from "../lib/roles";
import { AlertRow, PageHeader, RiskBadge } from "../components/ui";

export default function NotificationsPage() {
  useApp();
  const ns = scopedNotifications();
  return (
    <>
      <PageHeader
        title="通知中心"
        sub="集中查看系統所有提醒 · 管道：站內 / Email / LINE / App Push"
        right={<button className="btn" onClick={() => markAllRead(ns.map((n) => n.id))}>全部標為已讀</button>}
      />
      <div className="alist">
        {ns.map((n) => (
          <AlertRow
            key={n.id}
            level={n.level}
            dim={n.read}
            title={n.title}
            body={n.body}
            extra={
              <>
                {!n.read ? <RiskBadge level="high" label="新" /> : null}
                <div className="note" style={{ marginTop: 4 }}>{n.type} · {n.channel} · {fmtDT(n.time)}</div>
              </>
            }
            actions={
              <>
                {n.case_id ? <button className="btn sm" onClick={() => openCase(n.case_id!)}>查看案件</button> : null}
                <button className="btn sm" onClick={() => markRead(n.id)}>標記已讀</button>
              </>
            }
          />
        ))}
        {ns.length === 0 && <div className="note">目前沒有你範圍內的通知</div>}
      </div>
    </>
  );
}
