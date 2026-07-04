#!/usr/bin/env bash
# M2 驗收：分散式抓取、版本化去重、增量 cursor、source_url、負評過濾、稽核
set -uo pipefail
source "$(dirname "$0")/lib.sh"



echo "== 等待管線運轉（scheduler 每 10s 派工、mock 每 20s 產生事件、約 1/3 是編輯）=="
deadline=$((SECONDS + 240))
while [ $SECONDS -lt $deadline ]; do
    done_jobs=$($PSQL "SELECT count(*) FROM crawl_jobs WHERE status = 'succeeded'")
    raws=$($PSQL "SELECT count(*) FROM raw_reviews WHERE source_name LIKE 'google_review_mock%'")
    versions=$($PSQL "
        SELECT count(*) FROM (
            SELECT external_id FROM raw_reviews
            WHERE source_name LIKE 'google_review_mock%'
            GROUP BY source_name, external_id HAVING count(*) >= 2) v")
    echo "  ... succeeded_jobs=$done_jobs raw_reviews=$raws edited_versions=$versions"
    # 至少 4 個成功任務、有資料、且抓到至少一則「編輯後的新版本」
    if [ "${done_jobs:-0}" -ge 4 ] && [ "${raws:-0}" -ge 8 ] && [ "${versions:-0}" -ge 1 ]; then
        break
    fi
    sleep 15
done

echo "== 1. 分散式抓取 =="
succeeded=$($PSQL "SELECT count(*) FROM crawl_jobs WHERE status = 'succeeded'")
check_ge "成功任務數 >= 4" 4 "$succeeded"

workers=$($PSQL "SELECT count(DISTINCT worker_id) FROM crawl_jobs WHERE status = 'succeeded'")
check_ge "至少 2 個不同 worker 處理過任務" 2 "$workers"

dead=$($PSQL "SELECT count(*) FROM crawl_jobs WHERE status = 'dead_letter'")
check "沒有死信任務（failed 屬暫態，允許）" "0" "$dead"

stuck=$($PSQL "SELECT count(*) FROM crawl_jobs WHERE status = 'running' AND started_at < now() - interval '5 minutes'")
check "沒有卡死的 running 任務（reaper 生效）" "0" "$stuck"

echo "== 2. 抓取結果 =="
raws=$($PSQL "SELECT count(*) FROM raw_reviews WHERE source_name LIKE 'google_review_mock%'")
check_ge "raw_reviews 已寫入" 8 "$raws"

no_url=$($PSQL "SELECT count(*) FROM raw_reviews WHERE source_name LIKE 'google_review_mock%' AND (source_url IS NULL OR source_url NOT LIKE '%placeid=%')")
check "每則留言的 source_url 由 place_id 組出（非 mock 捏造）" "0" "$no_url"

no_loc=$($PSQL "SELECT count(*) FROM raw_reviews WHERE source_name LIKE 'google_review_mock%' AND location_id IS NULL")
check "每則留言都記錄 location_id（門市歸屬）" "0" "$no_loc"

high_star=$($PSQL "SELECT count(*) FROM raw_reviews WHERE source_name LIKE 'google_review_mock%' AND payload->>'starRating' IN ('FOUR','FIVE')")
check "只收 <=3 星負評（FOUR/FIVE 被過濾）" "0" "$high_star"

echo "== 3. 版本化與增量 =="
versions=$($PSQL "
    SELECT count(*) FROM (
        SELECT external_id FROM raw_reviews
        WHERE source_name LIKE 'google_review_mock%'
        GROUP BY source_name, external_id HAVING count(*) >= 2) v")
check_ge "編輯過的評論產生多版本列（T1-A 端到端）" 1 "$versions"

# 連續去重的不變量：相鄰版本不得同內容（非相鄰同內容 = 回改，合法）
dups=$($PSQL "
    SELECT count(*) FROM (
        SELECT content_hash,
               lag(content_hash) OVER (PARTITION BY source_name, external_id
                                       ORDER BY created_at, id) AS prev
        FROM raw_reviews WHERE source_name LIKE 'google_review_mock%'
    ) t WHERE content_hash = prev")
check "無相鄰重複版本（連續去重生效）" "0" "$dups"

cursor_set=$($PSQL "
    SELECT count(*) FROM crawl_jobs
    WHERE status = 'succeeded' AND cursor_state::text NOT IN ('{}', 'null')")
check_ge "成功任務都寫回 cursor" 4 "$cursor_set"

cap_hits=$($PSQL "SELECT count(*) FROM crawl_jobs WHERE stats->>'page_cap_hit' = 'true'")
check "無首次同步截斷（page_cap_hit）" "0" "$cap_hits"

echo "== 4. 事件發佈（review.raw → 供 M3 消費）=="
msgs=$($COMPOSE exec -T nats wget -qO- "http://localhost:8222/jsz?streams=true" | python3 -c "
import json,sys
d = json.load(sys.stdin)
for acc in d.get('account_details', []):
    for s in acc.get('stream_detail', []):
        if s['name'] == 'REVIEWS':
            print(s['state']['messages']); break
" 2>/dev/null || echo 0)
check_ge "REVIEWS stream 有 review.raw 訊息" 1 "$msgs"

echo "== 5. 稽核鏈路 =="
audited=$($PSQL "
    SELECT count(*) FROM audit_logs
    WHERE table_name = 'raw_reviews' AND changed_by LIKE 'svc:crawler-worker:%'")
check_ge "raw_reviews 寫入均有 audit_logs 且記錄 worker 身分" 8 "$audited"

forge=$($COMPOSE exec -T postgres psql "postgres://app_user:app_dev_password@localhost:5432/wachen" -qtA \
    -c "INSERT INTO audit_logs (table_name, record_id, action, changed_by) VALUES ('x','x','INSERT','forged')" 2>&1 | grep -c "permission denied")
check "app_user 無法直接偽造 audit_logs" "1" "$forge"

finish "M2"
