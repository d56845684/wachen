#!/usr/bin/env bash
# M4 驗收：AI 分析管線（review.created → analysis_results → review.analyzed）
set -uo pipefail
source "$(dirname "$0")/lib.sh"

echo "== 等待 analyzer 消化管線（含重放 backlog 與編輯觸發的重分析）=="
deadline=$((SECONDS + 240))
while [ $SECONDS -lt $deadline ]; do
    analyses=$($PSQL "SELECT count(*) FROM analysis_results WHERE is_current")
    reanalyzed=$($PSQL "
        SELECT count(*) FROM (
            SELECT review_id FROM analysis_results GROUP BY review_id HAVING count(*) >= 2) r")
    echo "  ... current_analyses=$analyses reanalyzed_reviews=$reanalyzed"
    if [ "${analyses:-0}" -ge 8 ] && [ "${reanalyzed:-0}" -ge 1 ]; then
        break
    fi
    sleep 15
done

echo "== 1. 分析結果 =="
analyses=$($PSQL "SELECT count(*) FROM analysis_results WHERE is_current")
check_ge "analysis_results 已產出" 8 "$analyses"

unanalyzed=$($PSQL "
    SELECT count(*) FROM reviews v
    WHERE v.status = 'analyzed' AND v.deleted_at IS NULL
      AND v.source_name NOT LIKE 'test_%'
      AND NOT EXISTS (SELECT 1 FROM analysis_results a WHERE a.review_id = v.id AND a.is_current)")
check "每則 analyzed review 都有現行分析" "0" "$unanalyzed"

stuck=$($PSQL "
    SELECT count(*) FROM reviews
    WHERE status = 'new' AND deleted_at IS NULL
      AND source_name NOT LIKE 'test_%'
      AND updated_at < now() - interval '3 minutes'")
check "沒有卡在 new 超過 3 分鐘的 review" "0" "$stuck"

echo "== 2. 模型溯源（AI 決策可稽核）=="
bad_trace=$($PSQL "
    SELECT count(*) FROM analysis_results
    WHERE is_current AND created_by NOT LIKE 'test:%'
      AND (model_name IS NULL OR prompt_version IS NULL
       OR input_hash IS NULL OR raw_response IS NULL OR latency_ms IS NULL)")
check "溯源欄位完整（model/prompt/input_hash/raw_response/latency）" "0" "$bad_trace"

provider=$($PSQL "SELECT DISTINCT model_name FROM analysis_results WHERE is_current LIMIT 1")
echo "  INFO: 現行供應商 = ${provider}（GEMINI_API_KEY 未設定時預期為 heuristic）"

echo "== 3. 風險判定（截圖 ② 嚴重度）=="
high=$($PSQL "
    SELECT count(*) FROM analysis_results
    WHERE is_current AND risk_level = 'high' AND cardinality(risk_reasons) > 0")
check_ge "食安樣本被判 high 且理由可解釋" 1 "$high"

lexicon=$($PSQL "
    SELECT count(*) FROM analysis_results
    WHERE is_current AND risk_level = 'high'
      AND EXISTS (SELECT 1 FROM unnest(risk_reasons) r WHERE r LIKE '命中%關鍵字%')")
check_ge "高風險字典覆核留有痕跡（寧可誤升）" 1 "$lexicon"

cat_ok=$($PSQL "
    SELECT count(*) FROM analysis_results
    WHERE is_current AND '餐點品質' = ANY(categories)")
check_ge "分類命中（餐點品質）" 1 "$cat_ok"

echo "== 4. 版本更新 → 重新分析 =="
reanalyzed=$($PSQL "
    SELECT count(*) FROM (
        SELECT review_id FROM analysis_results GROUP BY review_id HAVING count(*) >= 2) r")
check_ge "編輯過的評論觸發重新分析（多筆歷史）" 1 "$reanalyzed"

dup_current=$($PSQL "
    SELECT count(*) FROM (
        SELECT review_id FROM analysis_results WHERE is_current
        GROUP BY review_id HAVING count(*) > 1) d")
check "每則 review 僅一筆現行分析（is_current 唯一）" "0" "$dup_current"

echo "== 5. review.analyzed 事件（供 M5 消費）=="
analyzed_events=$($COMPOSE exec -T nats wget -qO- "http://localhost:8222/jsz?streams=true&consumers=true" | python3 -c "
import json,sys
d = json.load(sys.stdin)
for acc in d.get('account_details', []):
    for s in acc.get('stream_detail', []):
        if s['name'] == 'REVIEWS':
            total = s['state']['messages']
            consumed = {}
            for c in s.get('consumer_detail', []):
                consumed[c['name']] = c['delivered']['consumer_seq'] + c['num_pending']
            print(max(0, total - consumed.get('ingestion', 0) - consumed.get('analysis', 0))); break
" 2>/dev/null || echo 0)
distinct_analyzed=$($PSQL "SELECT count(DISTINCT review_id) FROM analysis_results WHERE is_current")
check_ge "review.analyzed 事件數 >= 已分析 review 數" "$distinct_analyzed" "$analyzed_events"

echo "== 6. 稽核 =="
audited=$($PSQL "
    SELECT count(*) FROM audit_logs
    WHERE table_name = 'analysis_results' AND changed_by = 'svc:analyzer'")
check_ge "analysis_results 寫入均落 audit_logs（svc:analyzer）" 8 "$audited"

finish "M4"
