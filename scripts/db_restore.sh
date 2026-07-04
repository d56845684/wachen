#!/usr/bin/env bash
# 把 db_dump.sh 產出的 dump 還原到另一個環境（make db-restore DUMP=... ）。
#
#   TARGET_DATABASE_URL='postgres://USER:PASS@HOST:5432/wachen?sslmode=...' \
#     bash scripts/db_restore.sh backups/wachen_<時間>.dump
#
# TARGET_DATABASE_URL 指向目標的 wachen 資料庫；建立角色/資料庫需連 maintenance
# 庫，腳本會把路徑中的 DB 名換成 postgres 當管理連線（可用 TARGET_ADMIN_URL 覆寫）。
# 目標的連線使用者需有 CREATEDB / CREATEROLE 權限（RDS/CloudSQL 的主帳號即可）。
#
# 流程：建 app_user 角色 → 建空的 wachen 庫 → pg_restore → 補 grant → 驗證筆數。
# 全程用 postgres:16 容器跑 client，本機不需裝 psql；dump 掛進容器。
set -euo pipefail
cd "$(dirname "$0")/.."

DUMP="${1:-}"
if [ -z "$DUMP" ] || [ ! -f "$DUMP" ]; then
  echo "用法：TARGET_DATABASE_URL='...' bash scripts/db_restore.sh <dump 檔>" >&2
  exit 1
fi
if [ -z "${TARGET_DATABASE_URL:-}" ]; then
  echo "缺 TARGET_DATABASE_URL（目標的 wachen 資料庫連線字串）" >&2
  exit 1
fi

# 管理連線：把 URL 的資料庫路徑換成 /postgres（保留 query string）
ADMIN_URL="${TARGET_ADMIN_URL:-$(echo "$TARGET_DATABASE_URL" \
  | sed -E 's#(://[^/]+)/[^?]+#\1/postgres#')}"
APP_PW="${APP_USER_PASSWORD:-app_dev_password}"
ROLES_FILE="${DUMP%.dump}.roles"
IMG="postgres:16-alpine"
DUMP_DIR="$(cd "$(dirname "$DUMP")" && pwd)"
DUMP_NAME="$(basename "$DUMP")"

# psql/pg_restore 都在容器內跑，dump 目錄唯讀掛載
run() { docker run --rm --network "${DOCKER_NETWORK:-host}" \
  -v "$DUMP_DIR:/backups:ro" -e PGCONNECT_TIMEOUT=15 "$IMG" "$@"; }

echo "== 1/5 建立 app_user 角色（若不存在）=="
run psql "$ADMIN_URL" -v ON_ERROR_STOP=1 -c "
  DO \$\$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='app_user') THEN
      CREATE ROLE app_user LOGIN PASSWORD '$APP_PW';
    END IF;
  END \$\$;
  ALTER ROLE app_user WITH LOGIN PASSWORD '$APP_PW' NOSUPERUSER NOCREATEDB NOCREATEROLE;"

echo "== 2/5 建立 wachen 資料庫（若不存在）=="
run psql "$ADMIN_URL" -v ON_ERROR_STOP=1 -tc \
  "SELECT 1 FROM pg_database WHERE datname='wachen'" | grep -q 1 \
  || run psql "$ADMIN_URL" -v ON_ERROR_STOP=1 -c "CREATE DATABASE wachen"

echo "== 3/5 還原 schema + 資料 =="
# --no-owner：物件歸目標連線使用者；--single-transaction：失敗整批回滾不留半套
run pg_restore --no-owner --no-privileges --single-transaction \
  -d "$TARGET_DATABASE_URL" "/backups/$DUMP_NAME"

echo "== 4/5 補回 app_user 權限（等同 migration 000005）=="
run psql "$TARGET_DATABASE_URL" -v ON_ERROR_STOP=1 -c "
  GRANT USAGE ON SCHEMA public TO app_user;
  GRANT SELECT, INSERT, UPDATE ON ALL TABLES IN SCHEMA public TO app_user;
  GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO app_user;
  REVOKE UPDATE ON audit_logs, raw_reviews FROM app_user;
  REVOKE ALL ON schema_migrations FROM app_user;"

echo "== 5/5 驗證 =="
run psql "$TARGET_DATABASE_URL" -v ON_ERROR_STOP=1 -c "
  SELECT 'schema_migrations 版本' AS item, max(version)::text AS val FROM schema_migrations
  UNION ALL SELECT 'reviews', count(*)::text FROM reviews
  UNION ALL SELECT 'analysis_results', count(*)::text FROM analysis_results
  UNION ALL SELECT 'stores', count(*)::text FROM stores
  UNION ALL SELECT 'audit_logs', count(*)::text FROM audit_logs;"

echo ""
echo "還原完成。新環境的服務把 DATABASE_URL 指向目標即可。"
echo "提醒：app_user 密碼為 '$APP_PW'（預設值；正式環境請用 APP_USER_PASSWORD 覆寫並改服務設定）。"
