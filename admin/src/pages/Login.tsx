import { FormEvent, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, auth } from "../api";

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
      const { token, name } = await api.login(email, password);
      auth.save(token, name);
      nav("/", { replace: true });
    } catch (ex) {
      setErr(ex instanceof Error ? ex.message : "登入失敗");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="login-wrap">
      <form className="login-card" onSubmit={submit}>
        <div className="brand">
          <div className="brand-seal">哨</div>
          <div>
            <h1>負評哨站</h1>
            <small>WACHEN · CASE DESK</small>
          </div>
        </div>
        <p className="hint">顧客負評追蹤系統 — 案件收件匣（PoC）</p>

        {err && <div className="login-err">{err}</div>}

        <div className="field">
          <label>帳號 EMAIL</label>
          <input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            placeholder="admin@example.com"
            autoComplete="username"
            required
          />
        </div>
        <div className="field">
          <label>密碼 PASSWORD</label>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            autoComplete="current-password"
            required
          />
        </div>
        <button className="btn-primary" disabled={busy}>
          {busy ? "驗證中…" : "進入哨站"}
        </button>
      </form>
    </div>
  );
}
