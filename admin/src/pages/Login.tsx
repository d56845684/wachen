/** 登入 — 中台設計；帳密走正式 API（deploy/.env 的 ADMIN_EMAIL/ADMIN_PASSWORD），角色選擇為 demo scope */
import { FormEvent, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, auth } from "../api";
import { ROLES, setRole, type RoleId } from "../lib/roles";
import { pocAlert } from "../components/ui";

export default function Login() {
  const nav = useNavigate();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [roleId, setRoleId] = useState<RoleId>("hq");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr("");
    try {
      const { token, name } = await api.login(email, password);
      auth.save(token, name);
      setRole(roleId);
      nav(ROLES[roleId].home, { replace: true });
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "登入失敗");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="login-page">
      <form className="login-card" onSubmit={submit}>
        <div className="login-logo"><span className="m">瓦</span>瓦城顧客體驗中台</div>
        <div className="login-sub">Customer Experience & Operations Data Hub</div>

        {err ? <div className="synthbar" style={{ borderColor: "var(--critical)", color: "var(--critical)" }}>{err}</div> : null}

        <label>公司帳號 EMAIL</label>
        <input
          type="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder="admin@example.com"
          autoComplete="username"
          required
        />
        <label>密碼 PASSWORD</label>
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoComplete="current-password"
          required
        />
        <label>登入角色（示範用）</label>
        <select value={roleId} onChange={(e) => setRoleId(e.target.value as RoleId)}>
          {Object.entries(ROLES).map(([k, r]) => <option key={k} value={k}>{r.title}</option>)}
        </select>

        <button className="login-btn" disabled={busy}>{busy ? "驗證中…" : "登入"}</button>

        <div className="sso">
          <button type="button" onClick={() => pocAlert("Google Workspace SSO")}>Google Workspace SSO</button>
          <button type="button" onClick={() => pocAlert("Microsoft SSO")}>Microsoft SSO</button>
        </div>
        <div className="login-foot"><a>忘記密碼？</a><span>v0.9 POC</span></div>
        <div className="login-hint">選擇不同角色登入，會看到不同的首頁與資料範圍（帳密為正式 API 驗證）。</div>
      </form>
    </div>
  );
}
