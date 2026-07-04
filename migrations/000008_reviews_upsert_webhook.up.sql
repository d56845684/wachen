-- =============================================================
-- 000008: M3 前置
--   reviews 邏輯唯一鍵：一則評論（source, external_id）只有一列，
--   raw_reviews 的新版本到達時就地更新（audit trigger 留痕舊值），
--   status 重設 'new' 觸發重新分析（升級性編輯 → 重新分流）
--   webhook_generic 來源補上驗證密鑰
-- =============================================================

SET app.current_actor = 'svc:migration';

ALTER TABLE reviews
    ADD CONSTRAINT reviews_source_external_key UNIQUE (source_name, external_id);

-- PoC 用固定密鑰；正式環境改 secrets 管理（見 TODOS.md #2）
UPDATE sources
SET config = config || '{"webhook_secret": "dev_webhook_secret"}'::jsonb
WHERE name = 'webhook_generic';
