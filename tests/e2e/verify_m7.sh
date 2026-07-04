#!/usr/bin/env bash
# M7 驗收：後台回覆留言（草稿→[高風險審核]→送出）+ Reply Worker
# 自給自足：自建臨時來源 + 中/高風險案件 → 測試 → 全部砍掉，完全不碰真實資料。
set -uo pipefail
source "$(dirname "$0")/lib.sh"

SRC="test_reply_$(date +%s)"

reply_teardown() {
    echo "== [teardown] 清除回覆測試 fixture（${SRC}）=="
    $PSQL_BASE >/dev/null <<SQL
BEGIN;
SET session_replication_role = replica;
CREATE TEMP TABLE trev ON COMMIT DROP AS SELECT id, raw_review_id FROM reviews WHERE source_name = '$SRC';
CREATE TEMP TABLE tcase ON COMMIT DROP AS SELECT id FROM cases WHERE review_id IN (SELECT id FROM trev);
DELETE FROM replies          WHERE case_id IN (SELECT id FROM tcase);
DELETE FROM notifications    WHERE case_id IN (SELECT id FROM tcase);
DELETE FROM case_assignments WHERE case_id IN (SELECT id FROM tcase);
DELETE FROM cases            WHERE id IN (SELECT id FROM tcase);
DELETE FROM analysis_results WHERE review_id IN (SELECT id FROM trev);
DELETE FROM reviews          WHERE source_name = '$SRC';
DELETE FROM raw_reviews      WHERE source_name = '$SRC';
DELETE FROM sources          WHERE name = '$SRC';
SET session_replication_role = DEFAULT;
COMMIT;
SQL
}
trap reply_teardown EXIT   # 測完（含失敗）一定清乾淨

echo "== [setup] 自建可回覆來源 + 中/高風險案件 =="
$PSQL_BASE >/dev/null <<SQL
DO \$\$
DECLARE rr uuid; rv uuid; med_rule uuid; high_rule uuid;
BEGIN
  PERFORM set_config('app.current_actor', 'svc:verify', true);
  SELECT id INTO med_rule  FROM routing_rules WHERE risk_level='medium' AND enabled AND deleted_at IS NULL LIMIT 1;
  SELECT id INTO high_rule FROM routing_rules WHERE risk_level='high'   AND enabled AND deleted_at IS NULL LIMIT 1;

  INSERT INTO sources (name, adapter, config, capabilities, enabled, created_by, updated_by)
  VALUES ('$SRC', 'webhook_generic', '{"reply_channel":"echo"}',
          '{"can_reply":true,"reply_max_length":4096}', false, 'svc:verify', 'svc:verify');

  -- 中風險（免審核）
  INSERT INTO raw_reviews (source_name, external_id, payload, content_hash, source_url, fetched_at)
    VALUES ('$SRC', 'ext-med', '{}', 'h-med', 'https://example.com/med', now()) RETURNING id INTO rr;
  INSERT INTO reviews (raw_review_id, source_name, external_id, content, source_url, status)
    VALUES (rr, '$SRC', 'ext-med', '中風險測試留言', 'https://example.com/med', 'analyzed') RETURNING id INTO rv;
  INSERT INTO cases (review_id, risk_level, rule_id, status, sla_due_at)
    VALUES (rv, 'medium', med_rule, 'open', now() + interval '24 hours');

  -- 高風險（需審核）
  INSERT INTO raw_reviews (source_name, external_id, payload, content_hash, source_url, fetched_at)
    VALUES ('$SRC', 'ext-high', '{}', 'h-high', 'https://example.com/high', now()) RETURNING id INTO rr;
  INSERT INTO reviews (raw_review_id, source_name, external_id, content, source_url, status)
    VALUES (rr, '$SRC', 'ext-high', '高風險測試留言', 'https://example.com/high', 'analyzed') RETURNING id INTO rv;
  INSERT INTO cases (review_id, risk_level, rule_id, status, sla_due_at)
    VALUES (rv, 'high', high_rule, 'open', now() + interval '2 hours');
END
\$\$;
SQL

MED=$($PSQL "SELECT c.id FROM cases c JOIN reviews v ON v.id=c.review_id WHERE v.source_name='$SRC' AND c.risk_level='medium'")
HIGH=$($PSQL "SELECT c.id FROM cases c JOIN reviews v ON v.id=c.review_id WHERE v.source_name='$SRC' AND c.risk_level='high'")
check "fixture 建立（中/高風險案件）" "$([ -n "$MED" ] && [ -n "$HIGH" ] && echo 1)" "1"

WCURL="$COMPOSE exec -T webhook curl -s"
BASE="http://web"
TOKEN=$($WCURL -X POST "$BASE/api/v1/login" -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"Wachen!2026"}' | python3 -c "import json,sys;print(json.load(sys.stdin)['token'])" 2>/dev/null)

api() { $WCURL -H "Authorization: Bearer $TOKEN" "$@"; }
reply_status() { api "$BASE/api/v1/cases/$1" | python3 -c "
import json,sys
r=json.load(sys.stdin).get('replies',[])
print(r[-1]['status'] if r else 'none')"; }

echo "== 1. 中風險：建立即送出（echo 通道）=="
code=$($WCURL -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/cases/$MED/replies" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"content":"感謝您的寶貴回饋，我們已將問題反映給店主管改善。"}')
check "建立回覆（201）" "201" "$code"
st=""
for i in $(seq 1 12); do st=$(reply_status "$MED"); [ "$st" = "sent" ] && break; sleep 2; done
check "回覆經 Reply Worker 送出（sent）" "sent" "$st"

echo "== 2. 高風險：進審核佇列，不直接送出 =="
$WCURL -o /dev/null -X POST "$BASE/api/v1/cases/$HIGH/replies" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"content":"已緊急聯繫顧客並啟動食安複查，將於今日回報處理結果。"}'
sleep 1
check "高風險回覆進待審（pending_approval）" "pending_approval" "$(reply_status "$HIGH")"

RID=$(api "$BASE/api/v1/approvals" | python3 -c "
import json,sys
rs=json.load(sys.stdin)['replies']
print(next((r['id'] for r in rs if r['case_id']=='$HIGH'), ''))")
check "審核佇列含該高風險回覆" "$([ -n "$RID" ] && echo 1)" "1"

echo "== 3. 核准 → Reply Worker 送出 =="
code=$($WCURL -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/replies/$RID/approve" \
  -H "Authorization: Bearer $TOKEN")
check "核准（200）" "200" "$code"
st=""
for i in $(seq 1 12); do st=$(reply_status "$HIGH"); [ "$st" = "sent" ] && break; sleep 2; done
check "核准後送出（sent）" "sent" "$st"

echo "== 4. 冪等 / 稽核 =="
ext=$($PSQL "SELECT external_reply_id FROM replies WHERE case_id='$HIGH' ORDER BY created_at DESC LIMIT 1")
check "已記錄平台回覆 ID" "$([ -n "$ext" ] && echo 1)" "1"

audited=$($PSQL "
  SELECT count(*) FROM audit_logs
  WHERE table_name='replies' AND changed_by IN ('user:admin@example.com','svc:replier')")
check_ge "回覆生命週期落 audit_logs（建立者 + worker）" 3 "$audited"

echo "== 5. API gate =="
code=$($WCURL -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/cases/00000000-0000-0000-0000-000000000000/replies" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"content":"x"}')
check "不存在案件回覆被拒（404）" "404" "$code"

finish "M7"
