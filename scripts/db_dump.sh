#!/usr/bin/env bash
# 匯出整個 wachen 資料庫，供遷移到另一個環境（make db-dump）。
#
# 產出兩個檔案到 backups/：
#   wachen_<時間>.dump   — pg_dump 自訂格式（-Fc，壓縮、可平行還原、含 schema+資料）
#   wachen_<時間>.roles  — app_user 角色定義（單一 DB dump 不含角色，需另存）
#
# 為什麼用自訂格式全量 dump（而非 data-only）：
#   pg_dump 會把 trigger/constraint 排在資料 COPY 之後（post-data 段），
#   還原時 COPY 進表當下 append-only／audit trigger 尚未建立，不會誤觸發、
#   也不會被 current_actor() 記成 unknown。schema_migrations 一併帶走，
#   新環境會知道自己停在哪一版 migration。
set -euo pipefail
cd "$(dirname "$0")/.."

COMPOSE="docker compose -f deploy/docker-compose.yml"
STAMP="${1:-$($COMPOSE exec -T postgres date -u +%Y%m%d_%H%M%S)}"
OUT_DIR="backups"
mkdir -p "$OUT_DIR"
DUMP="$OUT_DIR/wachen_${STAMP}.dump"
ROLES="$OUT_DIR/wachen_${STAMP}.roles"

echo "== 匯出角色（app_user）=="
# 只挑 app_user，避免帶走 target 環境可能衝突的 superuser（wachen）
$COMPOSE exec -T postgres pg_dumpall -U wachen --roles-only --no-role-passwords \
  | grep -iE "create role app_user|alter role app_user" > "$ROLES" || true
echo "app_user 角色 → $ROLES"

echo "== 匯出資料庫（schema + 資料，自訂格式）=="
$COMPOSE exec -T postgres pg_dump -U wachen -d wachen -Fc --no-owner --no-privileges \
  > "$DUMP"
# 註：--no-privileges 讓 GRANT 不寫進 dump，改由 migration 000005 或還原腳本補；
#     --no-owner 讓還原時物件歸還原連線的使用者，跨環境使用者名不同也不卡。

SIZE=$(wc -c < "$DUMP" | tr -d ' ')
echo ""
echo "== 完成 =="
echo "  資料庫 dump : $DUMP （$((SIZE/1024)) KB）"
echo "  角色       : $ROLES"
echo ""
echo "搬到新環境後執行："
echo "  TARGET_DATABASE_URL='postgres://user:pass@host:5432/wachen?sslmode=disable' \\"
echo "    bash scripts/db_restore.sh $DUMP"
