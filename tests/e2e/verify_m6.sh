#!/usr/bin/env bash
# M6 驗收：後台（nginx + SPA + API）——登入、案件、狀態變更、稽核 actor
set -uo pipefail
source "$(dirname "$0")/lib.sh"
trap mock_teardown EXIT   # 測完（含失敗）一定砍掉 mock
mock_setup

# 一律走 nginx（後台唯一入口）；從內部網路打 http://web/
WCURL="$COMPOSE exec -T webhook curl -s"
BASE="http://web"

echo "== 0. 等 web/api 就緒 =="
deadline=$((SECONDS + 60))
while [ $SECONDS -lt $deadline ]; do
    code=$($WCURL -o /dev/null -w '%{http_code}' "$BASE/" 2>/dev/null || echo 000)
    [ "$code" = "200" ] && break
    sleep 3
done

echo "== 1. SPA 與反向代理 =="
spa=$($WCURL -o /dev/null -w '%{http_code}' "$BASE/")
check "首頁 200（nginx 靜態）" "200" "$spa"

deep=$($WCURL "$BASE/cases/deadbeef" | grep -c '<div id="root">')
check "SPA 深層路徑 fallback 到 index.html" "1" "$deep"

echo "== 2. 登入認證 =="
bad=$($WCURL -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/login" \
    -H 'Content-Type: application/json' \
    -d '{"email": "admin@example.com", "password": "wrong"}')
check "錯誤密碼 401" "401" "$bad"

TOKEN=$($WCURL -X POST "$BASE/api/v1/login" \
    -H 'Content-Type: application/json' \
    -d '{"email": "admin@example.com", "password": "Wachen!2026"}' | python3 -c "
import json,sys; print(json.load(sys.stdin).get('token',''))" 2>/dev/null)
if [ -n "$TOKEN" ]; then check "預設帳號登入取得 token" "ok" "ok"; else check "預設帳號登入取得 token" "ok" "fail"; fi

noauth=$($WCURL -o /dev/null -w '%{http_code}' "$BASE/api/v1/cases")
check "無 token 拒絕（401）" "401" "$noauth"

echo "== 等待 mock 管線生成案件（爬取 → 分析 → 分流）=="
deadline=$((SECONDS + 240))
while [ $SECONDS -lt $deadline ]; do
    cases=$($PSQL "SELECT count(*) FROM cases")
    echo "  ... cases=$cases"
    [ "${cases:-0}" -ge 8 ] && break
    sleep 15
done

echo "== 3. 案件收件匣 =="
LIST=$($WCURL -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/cases")
count=$(echo "$LIST" | python3 -c "import json,sys; print(len(json.load(sys.stdin)['cases']))" 2>/dev/null || echo 0)
check_ge "案件列表有資料" 8 "$count"

CASE_ID=$(echo "$LIST" | python3 -c "
import json,sys
cs = json.load(sys.stdin)['cases']
for c in cs:
    if c['status'] == 'open':
        print(c['id']); break" 2>/dev/null)

high_only=$($WCURL -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/cases?risk=high" | python3 -c "
import json,sys
cs = json.load(sys.stdin)['cases']
print(0 if any(c['risk_level'] != 'high' for c in cs) else 1)" 2>/dev/null || echo 0)
check "風險過濾正確（risk=high）" "1" "$high_only"

FACETS=$($WCURL -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/facets")
nstores=$(echo "$FACETS" | python3 -c "import json,sys; print(len(json.load(sys.stdin)['stores']))" 2>/dev/null || echo 0)
check_ge "facets 回傳門市清單" 1 "$nstores"

SRC=$(echo "$FACETS" | python3 -c "import json,sys; print(json.load(sys.stdin)['sources'][0]['value'])" 2>/dev/null)
src_only=$($WCURL -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/cases?source=$SRC" | python3 -c "
import json,sys
cs = json.load(sys.stdin)['cases']
print(0 if any(c['source_name'] != '$SRC' for c in cs) else 1)" 2>/dev/null || echo 0)
check "來源過濾正確（source=${SRC}）" "1" "$src_only"

LOC=$(echo "$FACETS" | python3 -c "
import json,sys
for s in json.load(sys.stdin)['stores']:
    if s['value'] != '__none__': print(s['value']); break" 2>/dev/null)
store_scoped=$($WCURL -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/cases?store=$LOC" | python3 -c "
import json,sys; print(1 if len(json.load(sys.stdin)['cases']) > 0 else 0)" 2>/dev/null || echo 0)
check "門市過濾有結果（store=${LOC}）" "1" "$store_scoped"

detail_ok=$($WCURL -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/cases/$CASE_ID" | python3 -c "
import json,sys
d = json.load(sys.stdin)
ok = bool(d.get('review_content') is not None and d.get('assignments') and d.get('source_url'))
print(1 if ok else 0)" 2>/dev/null || echo 0)
check "案件詳情含留言/指派/原始連結" "1" "$detail_ok"

echo "== 3b. AI 處理進度分頁 =="
pipe_ok=$($WCURL -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/pipeline" | python3 -c "
import json,sys
d = json.load(sys.stdin)
ok = ('funnel' in d and 'raw_reviews' in d['funnel'] and 'ai' in d
      and isinstance(d['ai'].get('models'), list) and 'recent' in d and 'risk' in d)
print(1 if ok else 0)" 2>/dev/null || echo 0)
check "pipeline 端點含漏斗/AI/風險/最近分析" "1" "$pipe_ok"

pipe_auth=$($WCURL -o /dev/null -w '%{http_code}' "$BASE/api/v1/pipeline")
check "pipeline 需認證（401）" "401" "$pipe_auth"

echo "== 4. 狀態變更（稽核 actor = 登入使用者）=="
patched=$($WCURL -o /dev/null -w '%{http_code}' -X PATCH \
    -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
    "$BASE/api/v1/cases/$CASE_ID/status" -d '{"status": "in_progress"}')
check "open → in_progress（200）" "200" "$patched"

invalid=$($WCURL -o /dev/null -w '%{http_code}' -X PATCH \
    -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
    "$BASE/api/v1/cases/$CASE_ID/status" -d '{"status": "closed"}')
check "非法轉換 in_progress → closed（422）" "422" "$invalid"

actor=$($PSQL "
    SELECT count(*) FROM audit_logs
    WHERE table_name = 'cases' AND record_id = '$CASE_ID'
      AND action = 'UPDATE' AND changed_by = 'user:admin@example.com'")
check_ge "狀態變更以登入者身分落 audit_logs" 1 "$actor"

finish "M6"
