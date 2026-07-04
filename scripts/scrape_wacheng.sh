#!/usr/bin/env bash
# Google Maps 評論全量爬取 orchestration（make scrape-wacheng 的實體）：
#   1. 從 stores 表匯出瓦城門市清單（google_place_id）
#   2. Playwright 容器爬每家店的全部評論，推入 webhook（與 API 版同 source、同管線）
# 需先跑過 make crawl-wacheng（stores 表要有門市）。
# 環境變數透傳：STORE_LIMIT / MAX_REVIEWS_PER_STORE / HEADLESS
set -euo pipefail
cd "$(dirname "$0")/.."

COMPOSE="docker compose -f deploy/docker-compose.yml"

echo "== 匯出門市清單 =="
# ONE_PER_BRAND=1：每個品牌只挑一家（品牌 = 店名第一段，去掉括號註記）
FILTER=""
if [ "${ONE_PER_BRAND:-0}" = "1" ]; then
  FILTER="DISTINCT ON (regexp_replace(split_part(name, ' ', 1), '（.*|\(.*', ''))"
fi
$COMPOSE exec -T postgres psql -U wachen -d wachen -tA -c "
  SELECT coalesce(json_agg(row_to_json(t)), '[]') FROM (
    SELECT $FILTER name, google_place_id AS place_id
    FROM stores
    WHERE deleted_at IS NULL
      AND google_place_id IS NOT NULL
      AND google_location_id NOT LIKE 'locations/%'  -- 排除 mock 門市
    ORDER BY $([ -n "$FILTER" ] && echo "regexp_replace(split_part(name, ' ', 1), '（.*|\(.*', ''),") name
  ) t
" > scripts/.wacheng_stores.json
echo "$(python3 -c "import json;print(len(json.load(open('scripts/.wacheng_stores.json'))))" \
  2>/dev/null || echo '?') 家門市"

echo "== Playwright 爬取（單線程節流，全量會跑很久）=="
# 必須用有頭模式（xvfb 虛擬顯示）——headless Chromium 會被 Google 判定為 bot、
# 回傳「內容受限」降級頁，評論完全不載入。HEADLESS 固定 0，勿改。
docker run --rm --network deploy_default --env-file deploy/.env \
    -e STORE_LIMIT="${STORE_LIMIT:-0}" \
    -e MAX_REVIEWS_PER_STORE="${MAX_REVIEWS_PER_STORE:-0}" \
    -e HEADLESS=0 \
    -e STORE_PAUSE="${STORE_PAUSE:-10}" \
    -v "$PWD/scripts:/scripts" -w /scripts \
    mcr.microsoft.com/playwright/python:v1.49.0-jammy \
    sh -c "pip install -q playwright==1.49.0 && xvfb-run -a python3 scrape_wacheng_maps.py"
