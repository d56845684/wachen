#!/usr/bin/env bash
# M1 驗收：任何寫入都自動產生 audit_logs、append-only 表防篡改、種子資料就緒
set -uo pipefail
source "$(dirname "$0")/lib.sh"



echo "== 1. 稽核 trigger：INSERT / UPDATE / 軟刪除 都要落 audit_logs =="
$PSQL_BASE <<'SQL' > /dev/null
BEGIN;
SET LOCAL app.current_actor = 'test:verify';
SET LOCAL app.request_id    = 'req-verify-001';
INSERT INTO sources (name, adapter, config)
VALUES ('verify_source', 'webhook_generic', '{"test": true}');
UPDATE sources SET enabled = false WHERE name = 'verify_source';
UPDATE sources SET deleted_at = now(), deleted_by = current_actor() WHERE name = 'verify_source';
COMMIT;
SQL

audit_count=$($PSQL_BASE -c "
    SELECT count(*) FROM audit_logs a
    JOIN sources s ON s.id::text = a.record_id
    WHERE a.table_name = 'sources' AND s.name = 'verify_source'")
check "verify_source 產生 3 筆 audit_logs (INSERT + 2xUPDATE)" "3" "$audit_count"

actor=$($PSQL_BASE -c "
    SELECT DISTINCT changed_by FROM audit_logs a
    JOIN sources s ON s.id::text = a.record_id
    WHERE a.table_name = 'sources' AND s.name = 'verify_source'")
check "changed_by 正確記錄操作者" "test:verify" "$actor"

req=$($PSQL_BASE -c "
    SELECT DISTINCT request_id FROM audit_logs a
    JOIN sources s ON s.id::text = a.record_id
    WHERE a.table_name = 'sources' AND s.name = 'verify_source'")
check "request_id 正確串接" "req-verify-001" "$req"

ver=$($PSQL_BASE -c "SELECT version FROM sources WHERE name = 'verify_source'")
check "version 樂觀鎖自動遞增 (1→3)" "3" "$ver"

upd_by=$($PSQL_BASE -c "SELECT updated_by FROM sources WHERE name = 'verify_source'")
check "updated_by 由 trigger 自動維護" "test:verify" "$upd_by"

echo "== 2. append-only：raw_reviews / audit_logs 禁止 UPDATE/DELETE =="
$PSQL_BASE <<'SQL' > /dev/null
BEGIN;
SET LOCAL app.current_actor = 'test:verify';
INSERT INTO raw_reviews (source_name, external_id, payload, content_hash, source_url, fetched_at)
VALUES ('verify', 'ext-001', '{"text": "難吃"}', 'hash001', 'https://example.com/review/1', now());
COMMIT;
SQL

update_blocked=$($PSQL_BASE -c "UPDATE raw_reviews SET content_hash = 'tampered' WHERE external_id = 'ext-001'" 2>&1 | grep -c "append-only")
check "raw_reviews UPDATE 被拒" "1" "$update_blocked"

delete_blocked=$($PSQL_BASE -c "DELETE FROM raw_reviews WHERE external_id = 'ext-001'" 2>&1 | grep -c "append-only")
check "raw_reviews DELETE 被拒" "1" "$delete_blocked"

audit_tamper=$($PSQL_BASE -c "DELETE FROM audit_logs WHERE table_name = 'raw_reviews'" 2>&1 | grep -c "append-only")
check "audit_logs DELETE 被拒" "1" "$audit_tamper"

echo "== 3. 版本化：內容變更 = 新版本；回改（A→B→A）也是新版本 =="
# raw_reviews 是 append-only，腳本要可重跑必須每輪用唯一 ID。
# 連續去重（同「最新版本」同內容才跳過）是應用層邏輯，由 store 整合測試驗證；
# DB 層驗證的是：任何版本都進得來、append-only 不可改。
EXT="ext-verify-$(date +%s)"
ver=$($PSQL_BASE -c "
    BEGIN;
    SET LOCAL app.current_actor = 'test:verify';
    INSERT INTO raw_reviews (source_name, external_id, payload, content_hash, fetched_at)
    VALUES ('verify', '$EXT', '{\"text\": \"難吃\"}', 'hash-a', now()),
           ('verify', '$EXT', '{\"text\": \"難吃，吃完中毒\"}', 'hash-b', now()),
           ('verify', '$EXT', '{\"text\": \"難吃\"}', 'hash-a', now());
    COMMIT;
    SELECT count(*) FROM raw_reviews WHERE external_id = '$EXT'")
check "三個版本（v1、編輯、回改）全部保留" "3" "$ver"

echo "== 4. 種子資料 =="
rules=$($PSQL_BASE -c "SELECT count(*) FROM routing_rules WHERE enabled")
check "routing_rules 三條分流規則 (high/medium/low)" "3" "$rules"

high_sla=$($PSQL_BASE -c "SELECT sla_hours FROM routing_rules WHERE risk_level = 'high'")
check "高風險 SLA = 2 小時" "2" "$high_sla"

srcs=$($PSQL_BASE -c "SELECT count(*) FROM sources WHERE name IN ('google_review_main', 'webhook_generic')")
check "sources 種子 (google_review + webhook)" "2" "$srcs"

echo "== 5. app_user 權限（第二道防線）=="
app_del=$($COMPOSE exec -T postgres psql "postgres://app_user:app_dev_password@localhost:5432/wachen" -qtA \
    -c "DELETE FROM sources WHERE name = 'verify_source'" 2>&1 | grep -c "permission denied")
check "app_user 無 DELETE 權限（強制軟刪除）" "1" "$app_del"

finish "M1"
