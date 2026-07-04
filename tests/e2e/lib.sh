# 驗收腳本共用函式：source "$(dirname "$0")/lib.sh"
# shellcheck shell=bash

COMPOSE="docker compose -f deploy/docker-compose.yml"
# PSQL_BASE：heredoc / 自帶參數用；PSQL：單句 SQL 直接接字串
PSQL_BASE="$COMPOSE exec -T postgres psql -U wachen -d wachen -v ON_ERROR_STOP=1 -qtA"
PSQL="$PSQL_BASE -c"

pass=0
fail=0

check() {
    local desc="$1" expected="$2" actual="$3"
    if [ "$expected" = "$actual" ]; then
        echo "  PASS: $desc"; pass=$((pass+1))
    else
        echo "  FAIL: $desc (expected=$expected, actual=$actual)"; fail=$((fail+1))
    fi
}

check_ge() {
    local desc="$1" min="$2" actual="$3"
    if [ "${actual:-0}" -ge "$min" ] 2>/dev/null; then
        echo "  PASS: $desc (actual=$actual)"; pass=$((pass+1))
    else
        echo "  FAIL: $desc (expected>=$min, actual=$actual)"; fail=$((fail+1))
    fi
}

# finish <里程碑名稱>：輸出總結並以 fail 數決定 exit code
finish() {
    echo ""
    echo "===================="
    echo "結果: $pass PASS / $fail FAIL"
    if [ "$fail" -eq 0 ]; then
        echo "$1 驗收通過 ✓"
    else
        echo "$1 驗收未通過 ✗"
        exit 1
    fi
}

# ---- mock 測試腳手架：產生 → 測試 → 砍掉（M2/M3 自給自足，不依賴常駐 mock）----

# mock_setup：重建 mock 門市/來源並啟動管線服務
mock_setup() {
    echo "== [setup] 建立 mock 門市/來源並啟動 mock 管線 =="
    $PSQL "SET app.current_actor='svc:verify';
      INSERT INTO stores (name, google_location_id, google_place_id, created_by, updated_by) VALUES
        ('Mock 一號店','locations/mock-loc-1','mock-place-1','svc:verify','svc:verify'),
        ('Mock 二號店','locations/mock-loc-2','mock-place-2','svc:verify','svc:verify')
      ON CONFLICT (google_location_id) DO NOTHING;" >/dev/null
    $PSQL "SET app.current_actor='svc:verify';
      INSERT INTO sources (name, adapter, config, capabilities, schedule_cron, enabled, created_by, updated_by) VALUES
        ('google_review_mock_a','google_review',
         '{\"api_base_url\":\"http://mockgoogle:8081\",\"account_id\":\"accounts/mock-account\",\"location_ids\":[\"locations/mock-loc-1\"],\"max_rating\":3}',
         '{\"can_reply\":true}','* * * * *',true,'svc:verify','svc:verify'),
        ('google_review_mock_b','google_review',
         '{\"api_base_url\":\"http://mockgoogle:8081\",\"account_id\":\"accounts/mock-account\",\"location_ids\":[\"locations/mock-loc-2\"],\"max_rating\":3}',
         '{\"can_reply\":true}','* * * * *',true,'svc:verify','svc:verify')
      ON CONFLICT (name) DO NOTHING;" >/dev/null
    $COMPOSE start mockgoogle scheduler worker ingestion analyzer routing >/dev/null 2>&1
}

# mock_teardown：停止 mock 產生器並清除所有 mock 資料（trap EXIT 呼叫，失敗也清）
mock_teardown() {
    echo "== [teardown] 停止 mock 產生器並清除 mock 資料 =="
    $COMPOSE stop scheduler worker mockgoogle >/dev/null 2>&1
    $PSQL_BASE >/dev/null <<'SQL'
BEGIN;
SET session_replication_role = replica;
CREATE TEMP TABLE mrev ON COMMIT DROP AS SELECT id, raw_review_id FROM reviews WHERE source_name LIKE 'google_review_mock%';
CREATE TEMP TABLE mcase ON COMMIT DROP AS SELECT id FROM cases WHERE review_id IN (SELECT id FROM mrev);
DELETE FROM notifications    WHERE case_id IN (SELECT id FROM mcase);
DELETE FROM replies          WHERE case_id IN (SELECT id FROM mcase);
DELETE FROM case_assignments WHERE case_id IN (SELECT id FROM mcase);
DELETE FROM cases            WHERE id IN (SELECT id FROM mcase);
DELETE FROM analysis_results WHERE review_id IN (SELECT id FROM mrev);
DELETE FROM ingest_quarantine WHERE raw_review_id IN (SELECT raw_review_id FROM mrev);
DELETE FROM reviews          WHERE id IN (SELECT id FROM mrev);
DELETE FROM raw_reviews      WHERE source_name LIKE 'google_review_mock%';
DELETE FROM crawl_jobs j USING sources s WHERE j.source_id = s.id AND s.name LIKE 'google_review_mock%';
DELETE FROM sources WHERE name LIKE 'google_review_mock%';
DELETE FROM stores  WHERE google_location_id LIKE 'locations/mock-%';
SET session_replication_role = DEFAULT;
COMMIT;
SQL
}

# nats_stream_messages <stream>：容器內查 JetStream 訊息數
nats_stream_messages() {
    $COMPOSE exec -T nats wget -qO- "http://localhost:8222/jsz?streams=true" | python3 -c "
import json,sys
d = json.load(sys.stdin)
for acc in d.get('account_details', []):
    for s in acc.get('stream_detail', []):
        if s['name'] == '$1':
            print(s['state']['messages']); break
" 2>/dev/null || echo 0
}
