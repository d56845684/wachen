SET app.current_actor = 'svc:migration';

-- 縮回約束前，先移除違反新約束的租戶帳號
DELETE FROM users WHERE role = 'tsannkuen';

ALTER TABLE users DROP CONSTRAINT users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check
    CHECK (role IN ('store_manager', 'district_manager', 'hq_service', 'pr_legal', 'admin'));
