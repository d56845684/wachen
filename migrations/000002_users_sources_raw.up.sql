-- =============================================================
-- 000002: users / sources / crawl_jobs / raw_reviews
-- =============================================================

-- 使用者（RBAC 角色對齊 routing_rules.assignee_roles）
CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text    NOT NULL UNIQUE,
    display_name  text    NOT NULL,
    role          text    NOT NULL CHECK (role IN ('store_manager', 'district_manager', 'hq_service', 'pr_legal', 'admin')),
    store_id      text,
    password_hash text,
    is_active     boolean NOT NULL DEFAULT true,
    -- 標準稽核欄位
    created_at    timestamptz NOT NULL DEFAULT now(),
    created_by    text        NOT NULL DEFAULT current_actor(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    updated_by    text        NOT NULL DEFAULT current_actor(),
    deleted_at    timestamptz,
    deleted_by    text,
    version       integer     NOT NULL DEFAULT 1
);
SELECT apply_audit_triggers('users');

-- 爬蟲來源設定（新增來源 = 加一筆設定 + 實作 Adapter）
CREATE TABLE sources (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name          text    NOT NULL UNIQUE,
    adapter       text    NOT NULL,
    config        jsonb   NOT NULL DEFAULT '{}',
    capabilities  jsonb   NOT NULL DEFAULT '{"can_reply": false}',
    schedule_cron text,
    enabled       boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    created_by    text        NOT NULL DEFAULT current_actor(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    updated_by    text        NOT NULL DEFAULT current_actor(),
    deleted_at    timestamptz,
    deleted_by    text,
    version       integer     NOT NULL DEFAULT 1
);
SELECT apply_audit_triggers('sources');

-- 抓取任務（爬蟲側稽核：誰抓的、抓了什麼、結果如何）
CREATE TABLE crawl_jobs (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id     uuid    NOT NULL REFERENCES sources (id),
    status        text    NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'dead_letter')),
    cursor_state  jsonb,
    worker_id     text,
    scheduled_at  timestamptz NOT NULL DEFAULT now(),
    started_at    timestamptz,
    finished_at   timestamptz,
    error         text,
    stats         jsonb   NOT NULL DEFAULT '{}',
    created_at    timestamptz NOT NULL DEFAULT now(),
    created_by    text        NOT NULL DEFAULT current_actor(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    updated_by    text        NOT NULL DEFAULT current_actor(),
    deleted_at    timestamptz,
    deleted_by    text,
    version       integer     NOT NULL DEFAULT 1
);
CREATE INDEX idx_crawl_jobs_source_status ON crawl_jobs (source_id, status);
SELECT apply_audit_triggers('crawl_jobs');

-- 原始留言（append-only：只有 created_*，UPDATE/DELETE 一律禁止）
CREATE TABLE raw_reviews (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source_name   text        NOT NULL,
    external_id   text        NOT NULL,
    payload       jsonb       NOT NULL,
    content_hash  text        NOT NULL,
    source_url    text,
    fetched_at    timestamptz NOT NULL,
    crawl_job_id  uuid        REFERENCES crawl_jobs (id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    created_by    text        NOT NULL DEFAULT current_actor(),
    UNIQUE (source_name, external_id)
);
CREATE INDEX idx_raw_reviews_fetched_at ON raw_reviews (fetched_at);

CREATE TRIGGER trg_audit AFTER INSERT ON raw_reviews
    FOR EACH ROW EXECUTE FUNCTION audit_trigger_fn();
CREATE TRIGGER trg_raw_reviews_immutable BEFORE UPDATE OR DELETE ON raw_reviews
    FOR EACH ROW EXECUTE FUNCTION forbid_change_fn();
