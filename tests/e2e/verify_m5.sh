#!/usr/bin/env bash
# M5 驗收：Routing Engine（分流/指派/SLA/通知）+ case.created 事件
set -uo pipefail
source "$(dirname "$0")/lib.sh"
trap mock_teardown EXIT   # 測完（含失敗）一定砍掉 mock
mock_setup

echo "== 等待 routing 消化 review.analyzed backlog =="
deadline=$((SECONDS + 240))
while [ $SECONDS -lt $deadline ]; do
    cases=$($PSQL "SELECT count(*) FROM cases")
    sent=$($PSQL "SELECT count(*) FROM notifications WHERE status = 'sent'")
    # 食安高風險樣本可能比一般樣本晚進管線，等到有 high 案件才開始驗證
    high=$($PSQL "SELECT count(*) FROM cases WHERE risk_level = 'high'")
    echo "  ... cases=$cases sent_notifications=$sent high_cases=$high"
    if [ "${cases:-0}" -ge 8 ] && [ "${sent:-0}" -ge 8 ] && [ "${high:-0}" -ge 1 ]; then
        break
    fi
    sleep 15
done

echo "== 1. 案件建立（截圖 ③ 自動分流）=="
cases=$($PSQL "SELECT count(*) FROM cases")
check_ge "cases 已建立" 8 "$cases"

one_per=$($PSQL "
    SELECT count(*) FROM (
        SELECT review_id FROM cases GROUP BY review_id HAVING count(*) > 1) d")
check "一則評論一個案件執行緒（review_id UNIQUE）" "0" "$one_per"

high_ok=$($PSQL "
    SELECT count(*) FROM cases c
    WHERE c.risk_level = 'high'
      AND (SELECT count(DISTINCT assignee_role) FROM case_assignments a
           WHERE a.case_id = c.id AND a.assignee_role IN ('hq_service','pr_legal')) = 2")
check_ge "高風險案件指派雙角色（總部客服 + 公關法務）" 1 "$high_ok"

sla_ok=$($PSQL "
    SELECT count(*) FROM cases c JOIN routing_rules r ON r.id = c.rule_id
    WHERE c.sla_due_at IS NULL OR c.rule_id IS NULL OR c.analysis_id IS NULL")
check "每個案件都有規則、SLA、分析指標" "0" "$sla_ok"

echo "== 2. 對帳（analyzed-未建案 = 0）=="
unrouted=$($PSQL "
    SELECT count(*) FROM reviews v
    JOIN analysis_results a ON a.review_id = v.id AND a.is_current AND a.deleted_at IS NULL
    LEFT JOIN cases c ON c.review_id = v.id
    WHERE v.deleted_at IS NULL AND v.source_name NOT LIKE 'test_%'
      AND a.created_at < now() - interval '3 minutes'
      AND (c.id IS NULL OR c.analysis_id IS DISTINCT FROM a.id)")
check "無未認領的現行分析（漏建案/漏升級）" "0" "$unrouted"

echo "== 3. 通知（PoC log sender）=="
sent=$($PSQL "SELECT count(*) FROM notifications WHERE status = 'sent'")
check_ge "通知已送出（pending → sent）" 8 "$sent"

failed=$($PSQL "
    SELECT count(*) FROM notifications n
    JOIN cases c ON c.id = n.case_id
    JOIN reviews v ON v.id = c.review_id
    WHERE n.status = 'failed' AND v.source_name NOT LIKE 'test_%'")
check "無 failed 通知（排除整合測試殘留）" "0" "$failed"

no_body=$($PSQL "
    SELECT count(*) FROM notifications
    WHERE body IS NULL OR body = '' OR body NOT LIKE '%原始留言%'")
check "通知內容含摘要與原始留言連結" "0" "$no_body"

echo "== 4. SLA 逾期提醒（人工逾期一個案件，等 ticker ≤ 45s）=="
victim=$($PSQL "
    SELECT id FROM cases
    WHERE status = 'open' AND sla_reminded_at IS NULL LIMIT 1")
$PSQL "UPDATE cases SET sla_due_at = now() - interval '1 hour' WHERE id = '$victim'" > /dev/null
reminded=""
deadline=$((SECONDS + 60))
while [ $SECONDS -lt $deadline ]; do
    reminded=$($PSQL "SELECT count(*) FROM cases WHERE id = '$victim' AND sla_reminded_at IS NOT NULL")
    [ "${reminded:-0}" -ge 1 ] && break
    sleep 5
done
check "逾期案件被標記提醒（sla_reminded_at）" "1" "$reminded"

sla_notif=$($PSQL "
    SELECT count(*) FROM notifications
    WHERE case_id = '$victim' AND subject LIKE '%SLA 逾期%'")
check_ge "SLA 提醒通知已排入" 1 "$sla_notif"

echo "== 5. case.created 事件（供 M6/通知下游消費）=="
case_events=$($COMPOSE exec -T nats wget -qO- "http://localhost:8222/jsz?streams=true" | python3 -c "
import json,sys
d = json.load(sys.stdin)
for acc in d.get('account_details', []):
    for s in acc.get('stream_detail', []):
        if s['name'] == 'CASES':
            print(s['state']['messages']); break
" 2>/dev/null || echo 0)
cases=$($PSQL "SELECT count(*) FROM cases")
check_ge "CASES stream 事件數 >= 案件數" "$cases" "$case_events"

echo "== 6. 稽核 =="
audited=$($PSQL "
    SELECT count(*) FROM audit_logs
    WHERE table_name IN ('cases', 'case_assignments', 'notifications')
      AND changed_by = 'svc:routing'")
check_ge "路由寫入均落 audit_logs（svc:routing）" 20 "$audited"

finish "M5"
