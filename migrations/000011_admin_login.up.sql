-- =============================================================
-- 000011: M6 後台登入（PoC：帳密認證，無 RBAC）
--   預設帳號 admin@example.com / Wachen!2026
--   bcrypt 由 pgcrypto 的 crypt(+gen_salt('bf')) 產生與驗證——
--   Go 端零新密碼學依賴，驗證走 SQL：password_hash = crypt($pw, password_hash)
-- =============================================================

SET app.current_actor = 'svc:migration';

UPDATE users
SET password_hash = crypt('Wachen!2026', gen_salt('bf'))
WHERE email = 'admin@example.com';
