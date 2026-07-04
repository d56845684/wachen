#!/usr/bin/env bash
# M3 驗收：Ingestion（raw → reviews 正規化 + store_id + 版本更新）+ Webhook Gateway
set -uo pipefail
source "$(dirname "$0")/lib.sh"

# port 不對外：從 webhook 容器內部打自己（服務間一律走內部網路）
WCURL="$COMPOSE exec -T webhook curl -s -o /dev/null -w %{http_code}"
WEBHOOK_URL="http://localhost:8090/v1/sources/webhook_generic/reviews"
SECRET="dev_webhook_secret"


echo "== 等待 ingestion 消化爬蟲管線 =="
deadline=$((SECONDS + 120))
while [ $SECONDS -lt $deadline ]; do
    revs=$($PSQL "SELECT count(*) FROM reviews WHERE source_name LIKE 'google_review_mock%'")
    echo "  ... reviews=$revs"
    [ "${revs:-0}" -ge 8 ] && break
    sleep 10
done

echo "== 1. Ingestion 正規化（爬蟲來源）=="
revs=$($PSQL "SELECT count(*) FROM reviews WHERE source_name LIKE 'google_review_mock%'")
check_ge "reviews 已從 raw 正規化" 8 "$revs"

one_per=$($PSQL "
    SELECT count(*) FROM (
        SELECT source_name, external_id FROM reviews
        GROUP BY 1, 2 HAVING count(*) > 1) d")
check "一則評論一列（無重複 source+external_id）" "0" "$one_per"

no_store=$($PSQL "SELECT count(*) FROM reviews WHERE source_name LIKE 'google_review_mock%' AND store_id IS NULL")
check "爬蟲來源的 store_id 全部解析成功（T3-A）" "0" "$no_store"

bad_fields=$($PSQL "
    SELECT count(*) FROM reviews
    WHERE source_name LIKE 'google_review_mock%'
      AND (author_name IS NULL OR rating IS NULL OR content = '' OR posted_at IS NULL OR source_url = '')")
check "正規化欄位完整（author/rating/content/posted_at/source_url）" "0" "$bad_fields"

echo "== 2. 版本更新（編輯評論 → 同列更新 + status 重回 new + audit 留痕）=="
edited=$($PSQL "
    SELECT count(*) FROM reviews r
    WHERE source_name LIKE 'google_review_mock%' AND version > 1")
check_ge "至少一則 review 經歷版本更新（mock 編輯事件）" 1 "$edited"

audit_upd=$($PSQL "
    SELECT count(*) FROM audit_logs
    WHERE table_name = 'reviews' AND action = 'UPDATE' AND changed_by = 'svc:ingestion'")
check_ge "版本更新在 audit_logs 留有舊值" 1 "$audit_upd"

echo "== 3. Webhook Gateway（推送型來源）=="
# 冪等鍵每輪唯一，腳本才可重跑
WEXT="verify-m3-$(date +%s)"
resp=$($WCURL -X POST "$WEBHOOK_URL" \
    -H "X-Webhook-Secret: $SECRET" -H "Content-Type: application/json" \
    -d '{"external_id": "'$WEXT'", "author": "官網訪客", "rating": 1, "content": "訂位系統一直轉圈圈，最後訂不進去", "source_url": "https://example.com/feedback/verify-m3-001"}')
check "webhook 收件（201）" "201" "$resp"

resp=$($WCURL -X POST "$WEBHOOK_URL" \
    -H "X-Webhook-Secret: $SECRET" -H "Content-Type: application/json" \
    -d '{"external_id": "'$WEXT'", "author": "官網訪客", "rating": 1, "content": "訂位系統一直轉圈圈，最後訂不進去", "source_url": "https://example.com/feedback/verify-m3-001"}')
check "冪等重送（200 非 201）" "200" "$resp"

resp=$($WCURL -X POST "$WEBHOOK_URL" \
    -H "X-Webhook-Secret: wrong" -H "Content-Type: application/json" -d '{}')
check "錯誤密鑰被拒（401）" "401" "$resp"

resp=$($WCURL -X POST "$WEBHOOK_URL" \
    -H "X-Webhook-Secret: $SECRET" -H "Content-Type: application/json" \
    -d '{"external_id": "verify-m3-002", "source_url": "https://x"}')
check "缺 content/rating 被拒（400）" "400" "$resp"

# 等 ingestion 消化 webhook 事件
deadline=$((SECONDS + 30))
wh=""
while [ $SECONDS -lt $deadline ]; do
    wh=$($PSQL "SELECT count(*) FROM reviews WHERE source_name = 'webhook_generic' AND external_id = '$WEXT'")
    [ "${wh:-0}" -ge 1 ] && break
    sleep 3
done
check "webhook 留言流進 reviews（端到端）" "1" "$wh"

echo "== 4. review.created 事件（供 M4 消費）=="
# 精準計數：created = stream 總訊息 - ingestion consumer 消化的 review.raw 數
# （總數混計 raw+created 的檢查是空洞的——created 為零也會過）
created=$($COMPOSE exec -T nats wget -qO- "http://localhost:8222/jsz?streams=true&consumers=true" | python3 -c "
import json,sys
d = json.load(sys.stdin)
for acc in d.get('account_details', []):
    for s in acc.get('stream_detail', []):
        if s['name'] == 'REVIEWS':
            total = s['state']['messages']
            raw = 0
            for c in s.get('consumer_detail', []):
                if c['name'] == 'ingestion':
                    raw = c['delivered']['consumer_seq'] + c['num_pending']
            print(max(0, total - raw)); break
" 2>/dev/null || echo 0)
revs_total=$($PSQL "SELECT count(*) FROM reviews WHERE deleted_at IS NULL")
check_ge "review.created 事件數 >= reviews 數（每則至少通知一次）" "$revs_total" "$created"

# 排除整合測試殘留（test_% 來源會刻意製造隔離案例）
quarantined=$($PSQL "
    SELECT count(*) FROM ingest_quarantine q
    JOIN raw_reviews r ON r.id = q.raw_review_id
    WHERE r.source_name NOT LIKE 'test_%'")
check "無隔離中的毒藥 raw（normalize 全數成功）" "0" "$quarantined"

echo "== 5. 稽核 =="
ing_audit=$($PSQL "
    SELECT count(*) FROM audit_logs
    WHERE table_name = 'reviews' AND changed_by = 'svc:ingestion'")
check_ge "ingestion 寫入均落 audit_logs" 8 "$ing_audit"

wh_audit=$($PSQL "
    SELECT count(*) FROM audit_logs
    WHERE table_name = 'raw_reviews' AND changed_by = 'svc:webhook'")
check_ge "webhook 寫入均落 audit_logs" 1 "$wh_audit"

finish "M3"
