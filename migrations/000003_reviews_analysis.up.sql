-- =============================================================
-- 000003: reviews（正規化留言）/ analysis_results（AI 分析結果）
-- =============================================================

CREATE TABLE reviews (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    raw_review_id  uuid    NOT NULL UNIQUE REFERENCES raw_reviews (id),
    source_name    text    NOT NULL,
    external_id    text    NOT NULL,
    author_name    text,
    rating         numeric(2,1),
    content        text    NOT NULL,
    posted_at      timestamptz,
    store_id       text,
    source_url     text    NOT NULL,  -- 該則留言的 permalink，一鍵跳回原頁
    status         text    NOT NULL DEFAULT 'new'
                   CHECK (status IN ('new', 'analyzing', 'analyzed', 'cased', 'ignored')),
    created_at     timestamptz NOT NULL DEFAULT now(),
    created_by     text        NOT NULL DEFAULT current_actor(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    updated_by     text        NOT NULL DEFAULT current_actor(),
    deleted_at     timestamptz,
    deleted_by     text,
    version        integer     NOT NULL DEFAULT 1
);
CREATE INDEX idx_reviews_status ON reviews (status) WHERE deleted_at IS NULL;
CREATE INDEX idx_reviews_store  ON reviews (store_id);
SELECT apply_audit_triggers('reviews');

-- AI 分析結果：允許同一 review 多筆（模型/prompt 換版重跑），is_current 標記現行版
CREATE TABLE analysis_results (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    review_id       uuid    NOT NULL REFERENCES reviews (id),
    sentiment       text    CHECK (sentiment IN ('positive', 'neutral', 'negative')),
    sentiment_score numeric(4,3),
    categories      text[]  NOT NULL DEFAULT '{}',
    keywords        text[]  NOT NULL DEFAULT '{}',
    risk_level      text    NOT NULL CHECK (risk_level IN ('high', 'medium', 'low')),
    risk_reasons    text[]  NOT NULL DEFAULT '{}',
    summary         text,
    -- 模型溯源（AI 決策的稽核）
    model_name      text    NOT NULL,
    model_version   text,
    prompt_version  text    NOT NULL,
    input_hash      text,
    raw_response    jsonb,
    latency_ms      integer,
    is_current      boolean NOT NULL DEFAULT true,
    created_at      timestamptz NOT NULL DEFAULT now(),
    created_by      text        NOT NULL DEFAULT current_actor(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    updated_by      text        NOT NULL DEFAULT current_actor(),
    deleted_at      timestamptz,
    deleted_by      text,
    version         integer     NOT NULL DEFAULT 1
);
-- 每則 review 只有一筆現行分析
CREATE UNIQUE INDEX uq_analysis_current ON analysis_results (review_id) WHERE is_current;
SELECT apply_audit_triggers('analysis_results');
