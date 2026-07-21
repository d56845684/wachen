-- =============================================================
-- 000013: 多品牌租戶角色
--   users.role 加入 'tsannkuen'（燦坤3C 租戶管理者），供帳號綁定資料範圍。
--   帳密由 API 啟動時以 TSANNKUEN_EMAIL / TSANNKUEN_PASSWORD 注入（見 EnsureUser）。
-- =============================================================

SET app.current_actor = 'svc:migration';

ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check
    CHECK (role IN ('store_manager', 'district_manager', 'hq_service', 'pr_legal', 'admin', 'tsannkuen'));
