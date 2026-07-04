SET app.current_actor = 'svc:migration';

DROP INDEX IF EXISTS idx_replies_sending;
UPDATE sources
SET config = config - 'reply_channel',
    capabilities = capabilities || '{"can_reply": false}'::jsonb
WHERE name IN ('google_places_wacheng', 'webhook_generic');
