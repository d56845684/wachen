#!/usr/bin/env bash
# Cloudflare Tunnel：把本機後台（web:8088）透過 HTTPS 對外分享（PoC demo 用）
#
#   scripts/tunnel.sh start   啟動 quick tunnel，印出 https 網址
#   scripts/tunnel.sh check   顯示狀態與目前網址
#   scripts/tunnel.sh stop    關閉 tunnel
#   scripts/tunnel.sh url     只印出網址（給腳本串接用）
#
# 用 cloudflared 的 quick tunnel（免 Cloudflare 帳號、免網域）：
# 每次啟動產生一組隨機 *.trycloudflare.com 網址，關閉即失效。
# 需要固定網址請改用 named tunnel（見檔尾註解）。
set -uo pipefail

PORT="${TUNNEL_PORT:-8088}"
TARGET="http://localhost:${PORT}"
RUN_DIR="${TMPDIR:-/tmp}/wachen-tunnel"
PID_FILE="${RUN_DIR}/tunnel.pid"
LOG_FILE="${RUN_DIR}/tunnel.log"
URL_FILE="${RUN_DIR}/tunnel.url"
mkdir -p "$RUN_DIR"

c_red=$'\033[31m'; c_grn=$'\033[32m'; c_yel=$'\033[33m'; c_dim=$'\033[2m'; c_off=$'\033[0m'
say() { printf '%s\n' "$*"; }

ensure_cloudflared() {
  if command -v cloudflared >/dev/null 2>&1; then return 0; fi
  say "${c_red}找不到 cloudflared。${c_off}"
  say "  macOS:  brew install cloudflared"
  say "  Linux:  https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/"
  return 1
}

is_running() {
  [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null
}

target_up() {
  curl -s -o /dev/null -w '%{http_code}' --max-time 3 "$TARGET" 2>/dev/null | grep -q '^[23]'
}

start() {
  ensure_cloudflared || return 1
  if is_running; then
    say "${c_yel}tunnel 已在執行${c_off}（pid $(cat "$PID_FILE")）"
    [ -s "$URL_FILE" ] && say "網址：${c_grn}$(cat "$URL_FILE")${c_off}"
    return 0
  fi
  if ! target_up; then
    say "${c_red}後台 ${TARGET} 沒有回應${c_off} — 請先 ${c_dim}make up${c_off} 啟動服務"
    return 1
  fi

  : > "$LOG_FILE"; : > "$URL_FILE"
  say "${c_dim}啟動 quick tunnel → ${TARGET} …${c_off}"
  nohup cloudflared tunnel --no-autoupdate --url "$TARGET" >>"$LOG_FILE" 2>&1 &
  echo $! > "$PID_FILE"

  # 從 log 撈出 trycloudflare 網址（最多等 20 秒）
  for _ in $(seq 1 40); do
    url=$(grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' "$LOG_FILE" 2>/dev/null | head -1)
    if [ -n "$url" ]; then
      echo "$url" > "$URL_FILE"
      say ""
      say "  ${c_grn}▉ HTTPS 已就緒${c_off}"
      say "  ${c_grn}$url${c_off}"
      say ""
      say "  ${c_dim}後台登入：admin@example.com / Wachen!2026${c_off}"
      say "  ${c_dim}停止：scripts/tunnel.sh stop${c_off}"
      return 0
    fi
    if ! is_running; then
      say "${c_red}cloudflared 啟動失敗${c_off}，log 尾："
      tail -5 "$LOG_FILE"
      rm -f "$PID_FILE"
      return 1
    fi
    sleep 0.5
  done
  say "${c_red}20 秒內未取得網址${c_off}，log 尾："
  tail -8 "$LOG_FILE"
  return 1
}

check() {
  if is_running; then
    say "${c_grn}● 執行中${c_off}（pid $(cat "$PID_FILE")）"
    [ -s "$URL_FILE" ] && say "網址：${c_grn}$(cat "$URL_FILE")${c_off}"
    if target_up; then
      say "後台 ${TARGET}：${c_grn}正常${c_off}"
    else
      say "後台 ${TARGET}：${c_red}無回應${c_off}（tunnel 開著但後端掛了）"
    fi
  else
    say "${c_dim}○ 未執行${c_off}"
    return 1
  fi
}

stop() {
  if is_running; then
    pid=$(cat "$PID_FILE")
    kill "$pid" 2>/dev/null
    for _ in $(seq 1 10); do kill -0 "$pid" 2>/dev/null || break; sleep 0.3; done
    kill -9 "$pid" 2>/dev/null || true
    rm -f "$PID_FILE" "$URL_FILE"
    say "${c_yel}tunnel 已停止${c_off}"
  else
    say "${c_dim}tunnel 未在執行${c_off}"
    rm -f "$PID_FILE" "$URL_FILE"
  fi
}

url() {
  [ -s "$URL_FILE" ] && cat "$URL_FILE" || { say "（未執行）"; return 1; }
}

case "${1:-}" in
  start) start ;;
  stop) stop ;;
  check|status) check ;;
  restart) stop; sleep 1; start ;;
  url) url ;;
  *)
    say "用法：$0 {start|stop|check|restart|url}"
    say ""
    say "  start    啟動 Cloudflare quick tunnel（HTTPS，隨機 *.trycloudflare.com）"
    say "  check    顯示狀態與網址"
    say "  stop     停止"
    say "  restart  重啟（換新網址）"
    say "  url      只印網址"
    exit 1
    ;;
esac

# ── 固定網址（正式 demo）改用 named tunnel，需 Cloudflare 帳號 + 網域：
#   cloudflared tunnel login
#   cloudflared tunnel create wachen
#   cloudflared tunnel route dns wachen wachen.yourdomain.com
#   cloudflared tunnel run --url http://localhost:8088 wachen
