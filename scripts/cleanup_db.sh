#!/usr/bin/env bash
# 資料庫瘦身（PoC demo 維護用）：每個門市/來源保留最新 N 筆評論，其餘連同
# 分析/案件/通知一併刪除；test_* 整合測試殘留全清；audit_logs 與 crawl_jobs
# 兩個 log 表清空。
#
#   scripts/cleanup_db.sh [N]     # N 預設 10
#
# 注意：
#   - 破壞性、不可復原。跑之前務必先停掉會產生資料的服務（見 README 提示）。
#   - raw_reviews / audit_logs 平時由 trigger 保護為 append-only；本腳本以
#     superuser 的 session_replication_role=replica 暫時停用所有 trigger 與 FK
#     檢查來執行維護刪除，這是維運操作、不走應用程式路徑。
set -uo pipefail

KEEP="${1:-10}"
COMPOSE="docker compose -f deploy/docker-compose.yml"
PSQL="$COMPOSE exec -T postgres psql -U wachen -d wachen -v ON_ERROR_STOP=1 -qtA"

if ! [[ "$KEEP" =~ ^[0-9]+$ ]]; then
  echo "N 必須是數字，收到：$KEEP" >&2
  exit 1
fi

echo "== 清理前 =="
$PSQL -c "SELECT 'reviews='||count(*) FROM reviews
  UNION ALL SELECT 'cases='||count(*) FROM cases
  UNION ALL SELECT 'audit_logs='||count(*) FROM audit_logs
  UNION ALL SELECT 'crawl_jobs='||count(*) FROM crawl_jobs"

echo "== 執行清理（每桶保留最新 $KEEP 筆、test_* 全刪、清 log）=="
$PSQL <<SQL
BEGIN;
-- 停用所有 trigger（append-only 防護、audit、touch）與 FK 檢查——維運專用
SET session_replication_role = replica;

-- keepers：非 test 來源，每個門市（未對映則以來源分桶）保留最新 N 筆
CREATE TEMP TABLE keepers ON COMMIT DROP AS
SELECT id FROM (
  SELECT v.id,
         row_number() OVER (
           PARTITION BY coalesce(v.store_id::text, 'src:'||v.source_name)
           ORDER BY v.created_at DESC, v.id DESC) AS rn
  FROM reviews v
  WHERE v.deleted_at IS NULL AND v.source_name NOT LIKE 'test_%'
) t WHERE rn <= $KEEP;

CREATE TEMP TABLE drop_reviews ON COMMIT DROP AS
SELECT id FROM reviews WHERE id NOT IN (SELECT id FROM keepers);

CREATE TEMP TABLE drop_cases ON COMMIT DROP AS
SELECT id FROM cases WHERE review_id IN (SELECT id FROM drop_reviews);

-- 案件鏈（子表先刪）
DELETE FROM notifications    WHERE case_id IN (SELECT id FROM drop_cases);
DELETE FROM replies         WHERE case_id IN (SELECT id FROM drop_cases);
DELETE FROM case_assignments WHERE case_id IN (SELECT id FROM drop_cases);
DELETE FROM cases           WHERE id IN (SELECT id FROM drop_cases);

-- 分析與評論
DELETE FROM analysis_results WHERE review_id IN (SELECT id FROM drop_reviews);
DELETE FROM ingest_quarantine WHERE raw_review_id IN (
  SELECT raw_review_id FROM reviews WHERE id IN (SELECT id FROM drop_reviews));
DELETE FROM reviews WHERE id IN (SELECT id FROM drop_reviews);

-- 孤兒 raw_reviews（沒有任何保留 review 指向）
DELETE FROM raw_reviews r WHERE NOT EXISTS (
  SELECT 1 FROM reviews v WHERE v.raw_review_id = r.id);

-- log 表：audit_logs 全清；crawl_jobs 只留仍被保留 raw 參照的
TRUNCATE audit_logs RESTART IDENTITY;
DELETE FROM crawl_jobs j WHERE NOT EXISTS (
  SELECT 1 FROM raw_reviews r WHERE r.crawl_job_id = j.id);

SET session_replication_role = DEFAULT;
COMMIT;
SQL

echo "== 回收空間 =="
$PSQL -c "VACUUM (ANALYZE)" >/dev/null 2>&1 || true

echo "== 清理後 =="
$PSQL -c "SELECT 'reviews='||count(*) FROM reviews
  UNION ALL SELECT 'raw_reviews='||count(*) FROM raw_reviews
  UNION ALL SELECT 'analysis_results='||count(*) FROM analysis_results
  UNION ALL SELECT 'cases='||count(*) FROM cases
  UNION ALL SELECT 'notifications='||count(*) FROM notifications
  UNION ALL SELECT 'audit_logs='||count(*) FROM audit_logs
  UNION ALL SELECT 'crawl_jobs='||count(*) FROM crawl_jobs"

echo "== 每桶剩餘 =="
$PSQL -c "SELECT coalesce(st.name, 'src:'||v.source_name), count(*)
  FROM reviews v LEFT JOIN stores st ON st.id = v.store_id
  WHERE v.deleted_at IS NULL GROUP BY 1 ORDER BY 2 DESC"

echo ""
echo "完成。若要恢復產生資料：docker compose -f deploy/docker-compose.yml start scheduler worker mockgoogle ingestion analyzer routing"
