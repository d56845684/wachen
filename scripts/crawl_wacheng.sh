#!/usr/bin/env bash
# 台北瓦城集團 Google 評論爬取 orchestration（make crawl-wacheng 的實體）：
#   1. Places API 爬取並推入 webhook（crawl_wacheng_places.py）
#   2. 門市 upsert 進 stores 表（每家門市獨立店家）
#   3. 等 ingestion 消化完，回填 reviews.store_id
#      （external_id 格式 places/{place_id}/reviews/{rid}，可直接解析出門市）
# 可重複執行，冪等。
set -euo pipefail
cd "$(dirname "$0")/.."

COMPOSE="docker compose -f deploy/docker-compose.yml"
PSQL="$COMPOSE exec -T postgres psql -U wachen -d wachen -v ON_ERROR_STOP=1"

echo "== 爬取並推入 =="
docker run --rm --network deploy_default --env-file deploy/.env \
    -v "$PWD/scripts:/scripts" -w /scripts python:3.12-alpine \
    python3 crawl_wacheng_places.py

echo "== upsert stores =="
$PSQL -q < scripts/wacheng_stores.generated.sql
$PSQL -tAc "SELECT count(*)||' 家門市在 stores 表' FROM stores WHERE deleted_at IS NULL"

echo "== 等 ingestion 消化 =="
prev=-1
for _ in $(seq 1 30); do
    cur=$($PSQL -tAc "SELECT count(*) FROM reviews WHERE source_name='google_places_wacheng'")
    [ "$cur" = "$prev" ] && break
    prev=$cur; sleep 3
done
echo "reviews(google_places_wacheng) = $prev"

echo "== 回填 store_id =="
$PSQL -q <<'SQL'
SET app.current_actor = 'svc:crawl-wacheng';
UPDATE reviews v SET store_id = st.id
FROM stores st
WHERE v.source_name = 'google_places_wacheng' AND v.store_id IS NULL
  AND split_part(v.external_id, '/', 2) = st.google_location_id;
SQL
$PSQL -c "SELECT st.name AS store, count(*) AS reviews
FROM reviews v JOIN stores st ON st.id = v.store_id
WHERE v.source_name = 'google_places_wacheng'
GROUP BY st.name ORDER BY st.name"
