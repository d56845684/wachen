-- =============================================================
-- 000012: M7 開啟回覆能力
--   google_places_wacheng 是 Places 唯讀來源，真實 Places API 不支援回覆；
--   PoC 以「回覆通道 echo」模擬（該店自有系統/客服回覆），讓後台能實際操作
--   回覆生命週期。正式接 GBP 寫入權限見 M-R（docs/google-setup.md）。
--   reply_channel=echo：Reply Worker 記錄回覆並標記送出（不打外部 API）。
-- =============================================================

SET app.current_actor = 'svc:migration';

UPDATE sources
SET config = config || '{"reply_channel": "echo"}'::jsonb,
    capabilities = capabilities || '{"can_reply": true, "reply_max_length": 4096}'::jsonb
WHERE name IN ('google_places_wacheng', 'webhook_generic');

-- 回覆送出佇列的追蹤索引
CREATE INDEX idx_replies_sending ON replies (status) WHERE status IN ('approved', 'sending');
