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
