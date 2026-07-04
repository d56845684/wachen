import { ReactNode } from "react";
import {
  BrowserRouter,
  Navigate,
  NavLink,
  Route,
  Routes,
  useNavigate,
} from "react-router-dom";
import { auth } from "./api";
import Login from "./pages/Login";
import Inbox from "./pages/Inbox";
import CaseDetailPage from "./pages/CaseDetail";
import Pipeline from "./pages/Pipeline";
import Approvals from "./pages/Approvals";

function Shell({ children }: { children: ReactNode }) {
  const nav = useNavigate();
  if (!auth.token()) return <Navigate to="/login" replace />;
  return (
    <>
      <header className="topbar">
        <div className="brand-seal">哨</div>
        <h1>負評哨站</h1>
        <nav className="topnav">
          <NavLink to="/" end className={({ isActive }) => (isActive ? "on" : "")}>
            收件匣
          </NavLink>
          <NavLink to="/pipeline" className={({ isActive }) => (isActive ? "on" : "")}>
            AI 進度
          </NavLink>
          <NavLink to="/approvals" className={({ isActive }) => (isActive ? "on" : "")}>
            回覆審核
          </NavLink>
        </nav>
        <div className="spacer" />
        <span className="user">{auth.name()}</span>
        <button
          className="btn-ghost"
          onClick={() => {
            auth.clear();
            nav("/login");
          }}
        >
          登出
        </button>
      </header>
      {children}
    </>
  );
}

export default function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/" element={<Shell><Inbox /></Shell>} />
        <Route path="/pipeline" element={<Shell><Pipeline /></Shell>} />
        <Route path="/approvals" element={<Shell><Approvals /></Shell>} />
        <Route path="/cases/:id" element={<Shell><CaseDetailPage /></Shell>} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </BrowserRouter>
  );
}
