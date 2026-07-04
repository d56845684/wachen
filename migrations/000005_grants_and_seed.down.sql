SET app.current_actor = 'svc:migration';

DELETE FROM sources WHERE name IN ('google_review_main', 'webhook_generic');
DELETE FROM users WHERE email = 'admin@example.com';
DELETE FROM routing_rules;

REVOKE ALL ON ALL TABLES IN SCHEMA public FROM app_user;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM app_user;
REVOKE USAGE ON SCHEMA public FROM app_user;
-- 不 DROP ROLE：其他資料庫可能引用；PoC 重置用 docker compose down -v
