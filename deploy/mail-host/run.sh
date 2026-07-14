#!/usr/bin/env bash
# Lightweight process supervisor for Phase 2 mail stack (no heavy image builds).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"
mkdir -p data logs
set -a
# shellcheck disable=SC1091
[ -f .env ] && . ./.env
set +a
export DATA_DIR="${DATA_DIR:-$ROOT/data}"
export SMTP_ADDR="${SMTP_ADDR:-:2525}"
export SUBMIT_ADDR="${SUBMIT_ADDR:-:2587}"
export IMAP_ADDR="${IMAP_ADDR:-:143}"
export ADMIN_ADDR="${ADMIN_ADDR:-:8090}"
export ADMIN_UI_DIR="${ADMIN_UI_DIR:-$ROOT/admin-ui}"
export MAIL_HOSTNAME="${MAIL_HOSTNAME:-localhost}"

stop_all() {
  for p in mta imapd worker admin; do
    if [ -f "logs/$p.pid" ]; then
      kill "$(cat "logs/$p.pid")" 2>/dev/null || true
      rm -f "logs/$p.pid"
    fi
  done
}

start_one() {
  name="$1"
  nohup "$ROOT/bin/$name" >>"logs/$name.log" 2>&1 &
  echo $! >"logs/$name.pid"
  echo "started $name pid=$!"
}

case "${1:-start}" in
  stop) stop_all; echo stopped ;;
  restart) stop_all; sleep 1; exec "$0" start ;;
  start)
    stop_all
    start_one mta
    start_one imapd
    start_one worker
    start_one admin
    sleep 1
    echo ok
    ;;
  *) echo "usage: $0 start|stop|restart"; exit 1 ;;
esac
