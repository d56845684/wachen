-- =============================================================
-- 000004: routing_rules / cases / case_assignments / replies / notifications
-- =============================================================

CREATE TABLE routing_rules (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    risk_level       text    NOT NULL CHECK (risk_level IN ('high', 'medium', 'low')),
    assignee_roles   text[]  NOT NULL,
    sla_hours        integer NOT NULL CHECK (sla_hours > 0),
    require_approval boolean NOT NULL DEFAULT false,  -- 回覆是否需審核
    priority         integer NOT NULL DEFAULT 100,
    enabled          boolean NOT NULL DEFAULT true,
    created_at       timestamptz NOT NULL DEFAULT now(),
    created_by       text        NOT NULL DEFAULT current_actor(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    updated_by       text        NOT NULL DEFAULT current_actor(),
    deleted_at       timestamptz,
    deleted_by       text,
    version          integer     NOT NULL DEFAULT 1
);
SELECT apply_audit_triggers('routing_rules');

CREATE TABLE cases (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    review_id     uuid    NOT NULL UNIQUE REFERENCES reviews (id),
    risk_level    text    NOT NULL CHECK (risk_level IN ('high', 'medium', 'low')),
    rule_id       uuid    REFERENCES routing_rules (id),
    status        text    NOT NULL DEFAULT 'open'
                  CHECK (status IN ('open', 'in_progress', 'resolved', 'closed')),
    sla_due_at    timestamptz NOT NULL,
    responded_at  timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    created_by    text        NOT NULL DEFAULT current_actor(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    updated_by    text        NOT NULL DEFAULT current_actor(),
    deleted_at    timestamptz,
    deleted_by    text,
    version       integer     NOT NULL DEFAULT 1
);
CREATE INDEX idx_cases_sla ON cases (sla_due_at) WHERE status IN ('open', 'in_progress');
SELECT apply_audit_triggers('cases');

CREATE TABLE case_assignments (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id        uuid NOT NULL REFERENCES cases (id),
    assignee_role  text NOT NULL,
    assignee_id    uuid REFERENCES users (id),
    assigned_at    timestamptz NOT NULL DEFAULT now(),
    created_at     timestamptz NOT NULL DEFAULT now(),
    created_by     text        NOT NULL DEFAULT current_actor(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    updated_by     text        NOT NULL DEFAULT current_actor(),
    deleted_at     timestamptz,
    deleted_by     text,
    version        integer     NOT NULL DEFAULT 1
);
CREATE INDEX idx_case_assignments_case ON case_assignments (case_id);
SELECT apply_audit_triggers('case_assignments');

-- 回覆留言（對外發文：狀態機 + 冪等 + 審核鏈完整留痕）
CREATE TABLE replies (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id            uuid    NOT NULL REFERENCES cases (id),
    review_id          uuid    NOT NULL REFERENCES reviews (id),
    content            text    NOT NULL,
    status             text    NOT NULL DEFAULT 'draft'
                       CHECK (status IN ('draft', 'pending_approval', 'approved', 'rejected', 'sending', 'sent', 'failed')),
    idempotency_key    text    NOT NULL UNIQUE,  -- MQ 重投遞不重複發文
    author_id          uuid    REFERENCES users (id),
    approved_by        uuid    REFERENCES users (id),
    approved_at        timestamptz,
    external_reply_id  text,
    reply_url          text,
    platform_response  jsonb,
    retry_count        integer NOT NULL DEFAULT 0,
    error              text,
    created_at         timestamptz NOT NULL DEFAULT now(),
    created_by         text        NOT NULL DEFAULT current_actor(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    updated_by         text        NOT NULL DEFAULT current_actor(),
    deleted_at         timestamptz,
    deleted_by         text,
    version            integer     NOT NULL DEFAULT 1
);
CREATE INDEX idx_replies_case   ON replies (case_id);
CREATE INDEX idx_replies_status ON replies (status);
SELECT apply_audit_triggers('replies');

CREATE TABLE notifications (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    case_id      uuid    NOT NULL REFERENCES cases (id),
    channel      text    NOT NULL CHECK (channel IN ('email', 'line')),
    recipient    text    NOT NULL,
    subject      text,
    body         text,
    status       text    NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed')),
    sent_at      timestamptz,
    retry_count  integer NOT NULL DEFAULT 0,
    error        text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    created_by   text        NOT NULL DEFAULT current_actor(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    updated_by   text        NOT NULL DEFAULT current_actor(),
    deleted_at   timestamptz,
    deleted_by   text,
    version      integer     NOT NULL DEFAULT 1
);
CREATE INDEX idx_notifications_status ON notifications (status) WHERE status = 'pending';
SELECT apply_audit_triggers('notifications');
