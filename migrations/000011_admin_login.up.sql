-- =============================================================
-- 000011: M6 後台登入
--   管理員帳密不再寫死在 migration——改由 API 服務啟動時以環境變數
--   ADMIN_EMAIL / ADMIN_PASSWORD 建立/更新（見 cmd/api EnsureAdmin）。
--   密碼永不進版控；bcrypt 由 pgcrypto crypt(+gen_salt('bf')) 產生。
--   （管理員 user 列本身由 000005 種下，此處刻意不設密碼）
-- =============================================================

SET app.current_actor = 'svc:migration';
-- no-op：保留遷移編號連續，實際帳密由 API 從環境變數注入
