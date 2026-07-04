-- =============================================================
-- 000001: 稽核基礎建設
--   - audit_logs 全域異動軌跡（append-only）
--   - audit_trigger_fn: 業務表異動自動寫入 audit_logs
--   - touch_audit_columns_fn: 自動維護 updated_at/updated_by/version
--   - forbid_change_fn: append-only 表的防篡改保險
-- 應用層約定：每個交易開頭執行
--   SET LOCAL app.current_actor = '<user_id 或 svc:服務名>';
--   SET LOCAL app.request_id    = '<trace id>';
-- =============================================================

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- 取得當前操作者（未設定時記為 unknown，服務端應視為錯誤）
CREATE FUNCTION current_actor() RETURNS text
LANGUAGE sql STABLE AS $$
    SELECT coalesce(nullif(current_setting('app.current_actor', true), ''), 'unknown')
$$;

CREATE TABLE audit_logs (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    table_name  text        NOT NULL,
    record_id   text        NOT NULL,
    action      text        NOT NULL CHECK (action IN ('INSERT', 'UPDATE', 'DELETE')),
    old_data    jsonb,
    new_data    jsonb,
    changed_by  text        NOT NULL,
    request_id  text,
    changed_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_logs_record     ON audit_logs (table_name, record_id);
CREATE INDEX idx_audit_logs_changed_at ON audit_logs (changed_at);

CREATE FUNCTION audit_trigger_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    rec_id text;
BEGIN
    IF tg_op = 'DELETE' THEN
        rec_id := old.id::text;
    ELSE
        rec_id := new.id::text;
    END IF;

    INSERT INTO audit_logs (table_name, record_id, action, old_data, new_data, changed_by, request_id)
    VALUES (
        tg_table_name,
        rec_id,
        tg_op,
        CASE WHEN tg_op IN ('UPDATE', 'DELETE') THEN to_jsonb(old) END,
        CASE WHEN tg_op IN ('INSERT', 'UPDATE') THEN to_jsonb(new) END,
        current_actor(),
        nullif(current_setting('app.request_id', true), '')
    );
    RETURN coalesce(new, old);
END;
$$;

CREATE FUNCTION touch_audit_columns_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    new.updated_at := now();
    new.updated_by := current_actor();
    new.version    := old.version + 1;
    RETURN new;
END;
$$;

CREATE FUNCTION forbid_change_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'table % is append-only: % is not allowed', tg_table_name, tg_op;
END;
$$;

-- 幫業務表一次掛上 audit + touch 兩個 trigger
CREATE FUNCTION apply_audit_triggers(tbl regclass) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
    EXECUTE format(
        'CREATE TRIGGER trg_audit AFTER INSERT OR UPDATE OR DELETE ON %s
         FOR EACH ROW EXECUTE FUNCTION audit_trigger_fn()', tbl);
    EXECUTE format(
        'CREATE TRIGGER trg_touch BEFORE UPDATE ON %s
         FOR EACH ROW EXECUTE FUNCTION touch_audit_columns_fn()', tbl);
END;
$$;

-- audit_logs 本身不可改不可刪
CREATE TRIGGER trg_audit_logs_immutable
    BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION forbid_change_fn();
