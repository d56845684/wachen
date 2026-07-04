-- =============================================================
-- 000009: M3 外部審查修正
--   1. raw 去重改「連續去重」：撤三欄唯一鍵（A→B→A 回改必須成為新版本），
--      改由應用層在 advisory xact lock 下比對「最新版本 hash」
--   2. rating 上限修正：NPS 0-10 無法放進 numeric(2,1)（上限 9.9）
--   3. ingest_quarantine：normalize 失敗的 raw 隔離區，對帳掃描排除，
--      人工修復後刪列即可重入管線（raw_reviews 本身 append-only 不可標記）
-- =============================================================

SET app.current_actor = 'svc:migration';

-- 1. 連續去重（app 層），唯一鍵撤除
ALTER TABLE raw_reviews DROP CONSTRAINT raw_reviews_versioned_key;

-- 2. rating 承載 0-10（NPS）
ALTER TABLE reviews ALTER COLUMN rating TYPE numeric(3,1);
ALTER TABLE reviews ADD CONSTRAINT reviews_rating_range
    CHECK (rating IS NULL OR (rating >= 0 AND rating <= 10));

-- 3. 隔離表
CREATE TABLE ingest_quarantine (
    raw_review_id uuid PRIMARY KEY REFERENCES raw_reviews (id),
    reason        text        NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    created_by    text        NOT NULL DEFAULT current_actor()
);
GRANT SELECT, INSERT, DELETE ON ingest_quarantine TO app_user;  -- DELETE = 人工放行重入
