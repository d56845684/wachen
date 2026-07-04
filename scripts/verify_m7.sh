#!/usr/bin/env bash
# M7 驗收：後台回覆留言（草稿→[高風險審核]→送出）+ Reply Worker
set -uo pipefail
source "$(dirname "$0")/lib.sh"

WCURL="$COMPOSE exec -T webhook curl -s"
BASE="http://web"
TOKEN=$($WCURL -X POST "$BASE/api/v1/login" -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"Wachen!2026"}' | python3 -c "import json,sys;print(json.load(sys.stdin)['token'])" 2>/dev/null)
AUTH="-H Authorization:Bearer\ $TOKEN"

api() { $WCURL -H "Authorization: Bearer $TOKEN" "$@"; }

pick_case() { # $1=risk → 印出一個該風險、可回覆、狀態非 closed 的 case id
  api "$BASE/api/v1/cases?risk=$1" | python3 -c "
import json,sys
cs=json.load(sys.stdin)['cases']
print(cs[0]['id'] if cs else '')"
}
reply_status() { # $1=case_id → 最新一筆回覆狀態
  api "$BASE/api/v1/cases/$1" | python3 -c "
import json,sys
r=json.load(sys.stdin).get('replies',[])
print(r[-1]['status'] if r else 'none')"
}

echo "== 1. 低/中風險：建立即送出（echo 通道）=="
LOW=$(pick_case medium); [ -z "$LOW" ] && LOW=$(pick_case low)
check "找到可回覆的中/低風險案件" "$([ -n "$LOW" ] && echo 1)" "1"
code=$($WCURL -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/cases/$LOW/replies" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"content":"感謝您的寶貴回饋，我們已將問題反映給店主管改善。"}')
check "建立回覆（201）" "201" "$code"
# Reply Worker 非同步送出，等狀態變 sent
st=""
for i in $(seq 1 12); do st=$(reply_status "$LOW"); [ "$st" = "sent" ] && break; sleep 2; done
check "回覆經 Reply Worker 送出（sent）" "sent" "$st"

echo "== 2. 高風險：進審核佇列，不直接送出 =="
HIGH=$(pick_case high)
$WCURL -o /dev/null -X POST "$BASE/api/v1/cases/$HIGH/replies" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"content":"已緊急聯繫顧客並啟動食安複查，將於今日回報處理結果。"}'
sleep 1
st=$(reply_status "$HIGH")
check "高風險回覆進待審（pending_approval）" "pending_approval" "$st"

pending=$(api "$BASE/api/v1/approvals" | python3 -c "
import json,sys
rs=json.load(sys.stdin)['replies']
print(sum(1 for r in rs if r['case_id']=='$HIGH'))")
check_ge "審核佇列含該高風險回覆" 1 "$pending"

echo "== 3. 核准 → Reply Worker 送出 =="
RID=$(api "$BASE/api/v1/approvals" | python3 -c "
import json,sys
rs=json.load(sys.stdin)['replies']
print(next((r['id'] for r in rs if r['case_id']=='$HIGH'), ''))")
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

echo "== 5. 唯讀來源不可回覆（若有 can_reply=false 的案件）=="
# google_places 已開 echo，這裡驗 API gate：偽造一個不存在案件 → 404，不會誤放
code=$($WCURL -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/cases/00000000-0000-0000-0000-000000000000/replies" \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{"content":"x"}')
check "不存在案件回覆被拒（404）" "404" "$code"

finish "M7"
