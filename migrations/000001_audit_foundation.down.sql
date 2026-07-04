DROP TRIGGER IF EXISTS trg_audit_logs_immutable ON audit_logs;
DROP FUNCTION IF EXISTS apply_audit_triggers(regclass);
DROP FUNCTION IF EXISTS forbid_change_fn();
DROP FUNCTION IF EXISTS touch_audit_columns_fn();
DROP FUNCTION IF EXISTS audit_trigger_fn();
DROP TABLE IF EXISTS audit_logs;
DROP FUNCTION IF EXISTS current_actor();
