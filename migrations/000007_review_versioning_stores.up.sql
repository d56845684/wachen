-- =============================================================
-- 000007: 工程審查修正批次（2026-07-04 /plan-eng-review）
--   T1-A 版本化 raw_reviews（編輯的評論 = 新版本列，不再靜默丟棄）
--   T3-A stores 表（location→門市對映，M3 起填 reviews.store_id）
--   T5-A per-location 任務粒度（crawl_jobs.location_id）
--   稽核偽造洞：app_user 不可直寫 audit_logs，trigger 改 SECURITY DEFINER
--   索引補齊：scheduler 查詢 + reaper 掃描
-- =============================================================

SET app.current_actor = 'svc:migration';

-- ---------- T1-A: 版本化 raw ----------
-- 同一則評論被編輯 → payload 變 → content_hash 變 → 新列（新版本）
-- 完全相同的重抓 → 仍冪等跳過
ALTER TABLE raw_reviews
    DROP CONSTRAINT raw_reviews_source_name_external_id_key;
ALTER TABLE raw_reviews
    ADD CONSTRAINT raw_reviews_versioned_key
    UNIQUE (source_name, external_id, content_hash);
-- M3 ingestion 取「同 external_id 最新版本」用
CREATE INDEX idx_raw_reviews_latest
    ON raw_reviews (source_name, external_id, created_at DESC);

-- ---------- T3-A: stores 表 ----------
CREATE TABLE stores (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name                text NOT NULL,
    google_location_id  text UNIQUE,   -- "locations/123"
    google_place_id     text,          -- deep link 用（T2-A）
    created_at   timestamptz NOT NULL DEFAULT now(),
    created_by   text        NOT NULL DEFAULT current_actor(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    updated_by   text        NOT NULL DEFAULT current_actor(),
    deleted_at   timestamptz,
    deleted_by   text,
    version      integer     NOT NULL DEFAULT 1
);
SELECT apply_audit_triggers('stores');

-- reviews.store_id: 自由文字 → FK（表尚空，M3 才會填）
ALTER TABLE reviews DROP COLUMN store_id;
ALTER TABLE reviews ADD COLUMN store_id uuid REFERENCES stores (id);
CREATE INDEX idx_reviews_store ON reviews (store_id);

-- 抓取時就記下歸屬，M3 不用回頭解析 payload
ALTER TABLE raw_reviews ADD COLUMN location_id text;

-- ---------- T5-A: per-location 任務 ----------
ALTER TABLE crawl_jobs ADD COLUMN location_id text;

-- ---------- 稽核偽造洞 ----------
REVOKE INSERT ON audit_logs FROM app_user;
-- trigger 以定義者（migration 擁有者）身分寫 audit_logs，
-- app_user 完全無法直接觸碰該表
ALTER FUNCTION audit_trigger_fn() SECURITY DEFINER SET search_path = public;

-- ---------- 索引 ----------
CREATE INDEX idx_crawl_jobs_sched
    ON crawl_jobs (source_id, location_id, scheduled_at DESC);
CREATE INDEX idx_crawl_jobs_reap_running
    ON crawl_jobs (started_at) WHERE status = 'running';
CREATE INDEX idx_crawl_jobs_reap_pending
    ON crawl_jobs (created_at) WHERE status = 'pending';

-- ---------- 種子：mock 門市對映 ----------
INSERT INTO stores (name, google_location_id, google_place_id) VALUES
    ('Mock 一號店', 'locations/mock-loc-1', 'mock-place-1'),
    ('Mock 二號店', 'locations/mock-loc-2', 'mock-place-2');

-- grants 補 stores（000005 的 GRANT 只涵蓋當時既有的表）
GRANT SELECT, INSERT, UPDATE ON stores TO app_user;
