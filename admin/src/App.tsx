/** App shell — 側欄 + 頂欄 + 角色切換 + 案件/門市 drawer（瓦城顧客體驗中台 設計） */
import { ReactNode, useEffect, useState } from "react";
import {
  BrowserRouter, Navigate, Route, Routes, useLocation, useNavigate,
} from "react-router-dom";
import { auth } from "./api";
import { cnt, useApp } from "./lib/db";
import {
  allowedMenu, allowedRoleIds, getRole, getRoleId, loginRoleId, MENU, ROLES, scopedCases, scopedNotifications, setRole, TITLES, type RoleId,
} from "./lib/roles";
import { DrawerHost } from "./components/Drawer";
import Login from "./pages/Login";
import Dashboard from "./pages/Dashboard";
import StoreBoard from "./pages/StoreBoard";
import Reviews from "./pages/Reviews";
import Cases from "./pages/Cases";
import SlaMonitor from "./pages/SlaMonitor";
import Stores from "./pages/Stores";
import Causes from "./pages/Causes";
import Voice from "./pages/Voice";
import AiInsights from "./pages/AiInsights";
import Tasks from "./pages/Tasks";
import Improve from "./pages/Improve";
import NotificationsPage from "./pages/NotificationsPage";
import Reports from "./pages/Reports";
import Rules from "./pages/Rules";
import Org from "./pages/Org";
import Sources from "./pages/Sources";
import Inbox from "./pages/Inbox";
import CaseDetailPage from "./pages/CaseDetail";
import Pipeline from "./pages/Pipeline";
import Approvals from "./pages/Approvals";

function Shell({ children }: { children: ReactNode }) {
  useApp();
  const nav = useNavigate();
  const loc = useLocation();
  const [sbOpen, setSbOpen] = useState(false);
  if (!auth.token()) return <Navigate to="/login" replace />;

  // 租戶隔離：目前角色一律夾在登入帳號可切的角色內（防跨租戶殘留）
  const allowedRoles = allowedRoleIds();
  const curRole: RoleId = allowedRoles.includes(getRoleId()) ? getRoleId() : loginRoleId();
  useEffect(() => { if (getRoleId() !== curRole) setRole(curRole); }, [curRole]);

  const role = ROLES[curRole];
  const view = loc.pathname.split("/")[1] || "dashboard";

  // 角色看不到的頁 → 導回角色首頁（store 角色的 dashboard 對應 /store）
  const effView = view === "dashboard" && role.home === "/store" ? "store" : view;
  if (effView !== "store" && effView !== "cases" && !allowedMenu(effView) && TITLES[effView]) {
    return <Navigate to={role.home} replace />;
  }

  const sc = scopedCases();
  const openCnt = cnt(sc, (c) => ["unassigned", "open"].includes(c.cstatus));
  const alerts = scopedNotifications().filter((n) => !n.read).length;
  const scopeLabel = role.store ? "門市：" + role.store : role.scope ?? "—";

  return (
    <>
      <nav className={`sidebar ${sbOpen ? "open" : ""}`}>
        <div className="sb-brand">
          <span className="m">客</span>
          <div>顧客體驗中台<small>CX Hub</small></div>
        </div>
        {MENU.map((grp) => {
          const items = grp.items.filter((it) => allowedMenu(it.id));
          if (!items.length) return null;
          return (
            <div className="sb-group" key={grp.g}>
              <div className="lbl">{grp.g}</div>
              {items.map((it) => {
                const target = it.id === "dashboard" ? (role.home === "/store" ? "/store" : "/dashboard") : `/${it.id}`;
                const on = it.id === "dashboard" ? view === "dashboard" || view === "store" : view === it.id;
                const count = it.id === "cases" ? openCnt : it.id === "notifications" ? alerts : 0;
                return (
                  <button
                    key={it.id}
                    className={`sb-item ${on ? "on" : ""}`}
                    onClick={() => { nav(target); setSbOpen(false); }}
                  >
                    {it.t}
                    {count ? <span className="cnt">{count}</span> : null}
                  </button>
                );
              })}
            </div>
          );
        })}
      </nav>

      <div className="main">
        <div className="topbar">
          <button className="menu-toggle" onClick={() => setSbOpen((o) => !o)}>☰</button>
          <span className="crumb">{TITLES[view] ?? "總覽"}</span>
          <span className="spacer" />
          <label className="role-sel">
            角色
            {allowedRoles.length > 1 ? (
              <select value={curRole} onChange={(e) => { setRole(e.target.value as RoleId); nav(ROLES[e.target.value as RoleId].home); }}>
                {allowedRoles.map((k) => <option key={k} value={k}>{ROLES[k].title}</option>)}
              </select>
            ) : (
              <span style={{ marginLeft: 6, fontWeight: 600 }}>{ROLES[curRole].title}</span>
            )}
          </label>
          <button className="bell" aria-label="通知" onClick={() => nav("/notifications")}>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
              <path d="M13.7 21a2 2 0 0 1-3.4 0" />
            </svg>
            {alerts ? <span className="cnt">{alerts}</span> : null}
          </button>
          <div className="usr">
            <div className="av">{role.av}</div>
            <div><b>{auth.name() || role.name}</b><small>{role.title}</small></div>
          </div>
          <button className="btn sm" onClick={() => { auth.clear(); nav("/login"); }}>登出</button>
        </div>
        <div className="content">
          <div className="scope">
            目前角色 <b>{role.title}</b> · 資料範圍 <b>{scopeLabel}</b> · 可見案件 <b>{sc.length}</b> 筆
            {role.pii ? "" : " · 顧客個資已遮罩"}
          </div>
          {children}
        </div>
      </div>
      <DrawerHost />
    </>
  );
}

function Home() {
  useApp();
  return <Navigate to={getRole().home} replace />;
}

const VIEWS: [string, () => JSX.Element][] = [
  ["/dashboard", Dashboard], ["/store", StoreBoard], ["/reviews", Reviews], ["/cases", Cases],
  ["/sla", SlaMonitor], ["/stores", Stores], ["/causes", Causes], ["/voice", Voice],
  ["/ai", AiInsights], ["/tasks", Tasks], ["/improve", Improve], ["/notifications", NotificationsPage],
  ["/reports", Reports], ["/rules", Rules], ["/org", Org], ["/sources", Sources],
  ["/inbox", Inbox], ["/pipeline", Pipeline], ["/approvals", Approvals],
];

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/" element={<Shell><Home /></Shell>} />
        {VIEWS.map(([path, C]) => (
          <Route key={path} path={path} element={<Shell><C /></Shell>} />
        ))}
        <Route path="/cases/:id" element={<Shell><CaseDetailPage /></Shell>} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  );
}
