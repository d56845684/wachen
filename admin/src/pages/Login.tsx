/** 登入 — 中台設計；帳密走正式 API，角色由帳號決定（瓦城 admin / 燦坤 tsannkuen） */
import { FormEvent, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, auth } from "../api";
import { ROLES, loginRoleId, setRole } from "../lib/roles";
import { pocAlert } from "../components/ui";

export default function Login() {
  const nav = useNavigate();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setErr("");
    try {
      const { token, name, role } = await api.login(email, password);
      auth.save(token, name, role);
      const rid = loginRoleId();
      setRole(rid);
      nav(ROLES[rid].home, { replace: true });
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "登入失敗");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="login-page">
      <form className="login-card" onSubmit={submit}>
        <div className="login-logo"><span className="m">客</span>顧客體驗中台</div>
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
        <button className="login-btn" disabled={busy}>{busy ? "驗證中…" : "登入"}</button>

        <div className="sso">
          <button type="button" onClick={() => pocAlert("Google Workspace SSO")}>Google Workspace SSO</button>
          <button type="button" onClick={() => pocAlert("Microsoft SSO")}>Microsoft SSO</button>
        </div>
        <div className="login-foot"><a>忘記密碼？</a><span>v0.9 POC</span></div>
      </form>
    </div>
  );
}
