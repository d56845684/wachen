SET app.current_actor = 'svc:migration';

DROP INDEX IF EXISTS idx_crawl_jobs_reap_pending;
DROP INDEX IF EXISTS idx_crawl_jobs_reap_running;
DROP INDEX IF EXISTS idx_crawl_jobs_sched;

ALTER FUNCTION audit_trigger_fn() SECURITY INVOKER;
GRANT INSERT ON audit_logs TO app_user;

ALTER TABLE crawl_jobs DROP COLUMN IF EXISTS location_id;
ALTER TABLE raw_reviews DROP COLUMN IF EXISTS location_id;

ALTER TABLE reviews DROP COLUMN IF EXISTS store_id;
ALTER TABLE reviews ADD COLUMN store_id text;

DROP TABLE IF EXISTS stores;

DROP INDEX IF EXISTS idx_raw_reviews_latest;
ALTER TABLE raw_reviews DROP CONSTRAINT raw_reviews_versioned_key;
ALTER TABLE raw_reviews
    ADD CONSTRAINT raw_reviews_source_name_external_id_key
    UNIQUE (source_name, external_id);
