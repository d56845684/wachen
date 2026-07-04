# 資料庫遷移到另一個環境

把現有 `wachen` 資料庫（schema + 資料 + 角色）搬到另一台機器 / 另一個雲端 PG（RDS、CloudSQL、另一個 compose stack）。已實測 round-trip 通過。

## 為什麼是「dump + restore」而不是只跑 migration

schema 由 golang-migrate（`migrations/*.sql`）管理，跑 `make migrate` 就能在空庫重建 schema——但那只有結構、沒有資料。遷移環境要連**資料**一起搬，所以用 `pg_dump` 全量匯出。dump 內含 `schema_migrations` 表，新環境會知道自己停在哪一版，之後仍可繼續套用新 migration。

三個這個專案特有、遷移時要處理的點：

1. **append-only 稽核 trigger**：`raw_reviews`、`audit_logs` 有防篡改 trigger。全量 dump 用自訂格式（`-Fc`），`pg_dump` 會把 trigger 排在資料 COPY 之後，還原灌資料當下 trigger 尚未建立，不會誤觸發、也不會被 `current_actor()` 記成 unknown。
2. **`app_user` 角色**：服務連線用的非 superuser。單一 DB 的 dump 不含角色定義，`db_restore.sh` 會在目標先建好角色再補權限（等同 migration 000005 的 GRANT）。
3. **資料完整性**：`cleanup_db.sh` 用 `session_replication_role=replica` 繞過 FK 刪資料，可能留下孤兒引用（例如 `ingest_quarantine` 指向已刪的 `raw_reviews`）。這種孤兒平時查詢看不到，但還原重建 FK 時會失敗。`db_dump.sh` 前先確認來源乾淨（見下方檢查）。

## 步驟

### 1. 來源環境：匯出

```bash
make db-dump
```

產出到 `backups/`（已在 .gitignore）：

- `wachen_<時間>.dump` — 自訂格式，schema + 資料（含 `schema_migrations`）
- `wachen_<時間>.roles` — `app_user` 角色定義

### 2. 把 dump 檔搬到目標可存取的地方

`scp`、雲端硬碟、或直接在目標機器 clone repo 都行。restore 用 docker 內的 psql client，本機不需裝 PostgreSQL 工具。

### 3. 目標環境：還原

```bash
TARGET_DATABASE_URL='postgres://ADMIN:PASS@HOST:5432/wachen?sslmode=require' \
  make db-restore DUMP=backups/wachen_<時間>.dump
```

- `TARGET_DATABASE_URL` 指向目標的 `wachen` 庫；建立角色 / 資料庫需要連 maintenance 庫，腳本自動把路徑換成 `/postgres`（可用 `TARGET_ADMIN_URL` 覆寫）。連線使用者需有 CREATEDB / CREATEROLE（RDS/CloudSQL 主帳號即可）。
- 目標若是另一個本機 compose stack，加 `DOCKER_NETWORK=<該 stack 網路>`（預設 `host`）。
- `app_user` 密碼預設沿用 PoC 的 `app_dev_password`，正式環境用 `APP_USER_PASSWORD='...'` 覆寫，並同步改各服務的 `DATABASE_URL`。

restore 流程：建 `app_user` → 建 `wachen` 庫 → `pg_restore --single-transaction`（失敗整批回滾，不留半套）→ 補 GRANT → 印出筆數驗證。

### 4. 目標環境：接上服務

把 `deploy/docker-compose.yml`（或正式部署）裡各服務的 `DATABASE_URL` / `POSTGRES_*` 指向目標 DB，起服務即可。NATS JetStream 是另一套狀態（訊息佇列），評論資料都在 PG，一般不需搬 NATS。

## 遷移前的一致性檢查（建議）

若來源曾跑過 `cleanup_db.sh`，dump 前先確認沒有孤兒 FK，否則 restore 會在重建 FK 時失敗：

```sql
-- 應全部回傳 0
SELECT count(*) FROM ingest_quarantine q
  WHERE q.raw_review_id IS NOT NULL
    AND NOT EXISTS (SELECT 1 FROM raw_reviews r WHERE r.id = q.raw_review_id);
```

有孤兒就先刪（無價值的失敗記錄殘骸）：

```sql
SET app.current_actor = 'svc:maintenance';
DELETE FROM ingest_quarantine q
  WHERE q.raw_review_id IS NOT NULL
    AND NOT EXISTS (SELECT 1 FROM raw_reviews r WHERE r.id = q.raw_review_id);
```

## 相關檔案

- `scripts/db_dump.sh` — 匯出（`make db-dump`）
- `scripts/db_restore.sh` — 還原（`make db-restore`）
- `migrations/` — schema 版本，遷移後仍是 schema 演進的唯一真實來源
