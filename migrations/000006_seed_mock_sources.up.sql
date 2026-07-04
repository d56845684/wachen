-- =============================================================
-- 000006: mock 來源（PoC 展示用；正式環境請 enabled = false）
--   api_base_url 指向 mockgoogle 容器，google adapter 程式碼不變
-- =============================================================

SET app.current_actor = 'svc:migration';

INSERT INTO sources (name, adapter, config, capabilities, schedule_cron, enabled) VALUES
    ('google_review_mock_a', 'google_review',
     '{"api_base_url": "http://mockgoogle:8081", "account_id": "accounts/mock-account", "location_ids": ["locations/mock-loc-1"], "max_rating": 3}',
     '{"can_reply": true, "reply_editable": true}',
     '* * * * *', true),
    ('google_review_mock_b', 'google_review',
     '{"api_base_url": "http://mockgoogle:8081", "account_id": "accounts/mock-account", "location_ids": ["locations/mock-loc-2"], "max_rating": 3}',
     '{"can_reply": true, "reply_editable": true}',
     '* * * * *', true);
