SET app.current_actor = 'svc:migration';

DROP TABLE IF EXISTS ingest_quarantine;

ALTER TABLE reviews DROP CONSTRAINT IF EXISTS reviews_rating_range;
ALTER TABLE reviews ALTER COLUMN rating TYPE numeric(2,1);

-- 注意：若歷史資料已含回改產生的同 hash 多版本，此約束會建立失敗（預期行為）
ALTER TABLE raw_reviews
    ADD CONSTRAINT raw_reviews_versioned_key
    UNIQUE (source_name, external_id, content_hash);
