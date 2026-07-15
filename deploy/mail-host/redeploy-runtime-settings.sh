#!/usr/bin/env bash
set -euo pipefail
cd /opt/wernanmail

[ -f /tmp/wm-mta ] && install -m 755 /tmp/wm-mta /opt/wernanmail/bin/mta
[ -f /tmp/wm-worker ] && install -m 755 /tmp/wm-worker /opt/wernanmail/bin/worker

restart() {
  local name="$1"
  if [ -f "logs/${name}.pid" ]; then
    kill "$(cat "logs/${name}.pid")" 2>/dev/null || true
    rm -f "logs/${name}.pid"
  fi
  pkill -f "/opt/wernanmail/bin/${name}" 2>/dev/null || true
}

restart mta
restart worker
# kill orphan workers with deleted exe
pkill -9 -f '/opt/wernanmail/bin/worker' 2>/dev/null || true
sleep 1

set -a
# shellcheck disable=SC1091
. ./.env
set +a

nohup ./bin/mta >>logs/mta.log 2>&1 & echo $! >logs/mta.pid
nohup ./bin/worker >>logs/worker.log 2>&1 & echo $! >logs/worker.pid
sleep 1

echo "mta=$(cat logs/mta.pid) worker=$(cat logs/worker.pid)"
ss -tlnp | grep -E ':(25|587)\b' || true
pgrep -af '/opt/wernanmail/bin/worker' || true
tail -n 3 logs/worker.log || true
strings bin/mta | grep -F 'rate_auth_fail_per_min' | head -1 || true
strings bin/worker | grep -E 'require_tls|quarantine purge|mail retention' | head -5 || true
