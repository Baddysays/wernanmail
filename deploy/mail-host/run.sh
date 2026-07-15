#!/usr/bin/env bash
# Lightweight process supervisor for Phase 2 mail stack (no heavy image builds).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$ROOT"
mkdir -p data logs www/admin
# nginx (www-data) must read SPA assets; scp/cp as root can leave 0700 dirs
chmod -R a+rX www 2>/dev/null || true
set -a
# shellcheck disable=SC1091
[ -f .env ] && . ./.env
set +a
export DATA_DIR="${DATA_DIR:-$ROOT/data}"
export SMTP_ADDR="${SMTP_ADDR:-:2525}"
export SUBMIT_ADDR="${SUBMIT_ADDR:-:2587}"
export IMAP_ADDR="${IMAP_ADDR:-:143}"
export ADMIN_ADDR="${ADMIN_ADDR:-:8090}"
export ADDR="${ADDR:-:8080}"
export COOKIE_SECURE="${COOKIE_SECURE:-true}"
export CORS_ORIGINS="${CORS_ORIGINS:-https://mail.wernanmail.ru}"
export MAIL_HOSTNAME="${MAIL_HOSTNAME:-localhost}"

stop_all() {
  for p in mta imapd worker admin api; do
    if [ -f "logs/$p.pid" ]; then
      kill "$(cat "logs/$p.pid")" 2>/dev/null || true
      rm -f "logs/$p.pid"
    fi
  done
  # sweep orphans left after binary replace (exe shows "(deleted)")
  pkill -f '/opt/wernanmail/bin/(mta|imapd|worker|admin|api)' 2>/dev/null || true
  pkill -f './bin/(mta|imapd|worker|admin|api)' 2>/dev/null || true
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
    start_one api
    sleep 1
    echo ok
    ;;
  *) echo "usage: $0 start|stop|restart"; exit 1 ;;
esac
