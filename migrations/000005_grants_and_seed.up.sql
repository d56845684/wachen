-- =============================================================
-- 000005: 應用程式角色權限 + 種子資料
--   app_user: 各服務連線用的非超級使用者
--     - audit_logs / raw_reviews 不可 UPDATE/DELETE（trigger 之外的第二道防線）
--     - 全部業務表不可 DELETE（軟刪除策略）
-- =============================================================

SET app.current_actor = 'svc:migration';

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'app_user') THEN
        -- PoC 用固定密碼；正式環境改用 secrets 管理並於部署時 ALTER ROLE
        CREATE ROLE app_user LOGIN PASSWORD 'app_dev_password';
    END IF;
END
$$;

GRANT USAGE ON SCHEMA public TO app_user;
GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA public TO app_user;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO app_user;
-- append-only 表連 UPDATE 都不給
REVOKE UPDATE ON audit_logs, raw_reviews FROM app_user;
-- migration 版本表不歸應用管
REVOKE ALL ON schema_migrations FROM app_user;

-- ---------- 種子資料 ----------

-- 分流規則（對應截圖 ③：高/中/低風險）
INSERT INTO routing_rules (risk_level, assignee_roles, sla_hours, require_approval, priority) VALUES
    ('high',   ARRAY['hq_service', 'pr_legal'],            2,  true,  10),
    ('medium', ARRAY['district_manager', 'store_manager'], 24, false, 20),
    ('low',    ARRAY['store_manager'],                     48, false, 30);

-- 管理員帳號（PoC 登入時再補 password_hash）
INSERT INTO users (email, display_name, role) VALUES
    ('admin@example.com', '系統管理員', 'admin');

-- 來源設定範例（google_review 先停用，等 API 憑證就緒後啟用）
INSERT INTO sources (name, adapter, config, capabilities, schedule_cron, enabled) VALUES
    ('google_review_main', 'google_review',
     '{"account_id": "", "location_ids": [], "max_rating": 3}',
     '{"can_reply": true, "reply_editable": true, "reply_max_length": 4096}',
     '*/15 * * * *', false),
    ('webhook_generic', 'webhook_generic',
     '{}',
     '{"can_reply": false}',
     NULL, true);
