#!/usr/bin/env bash
set -euo pipefail
cd /opt/wernanmail

if [ -f /tmp/wm-mta ]; then install -m 755 /tmp/wm-mta /opt/wernanmail/bin/mta; fi
if [ -f /tmp/wm-api ]; then install -m 755 /tmp/wm-api /opt/wernanmail/bin/api; fi
chmod -R a+rX /opt/wernanmail/www/admin 2>/dev/null || true

restart() {
  local name="$1"
  if [ -f "logs/${name}.pid" ]; then
    kill "$(cat "logs/${name}.pid")" 2>/dev/null || true
    rm -f "logs/${name}.pid"
  fi
  pkill -f "/opt/wernanmail/bin/${name}" 2>/dev/null || true
}

restart mta
restart api
sleep 1

set -a
# shellcheck disable=SC1091
. ./.env
set +a
unset ADMIN_UI_DIR

nohup ./bin/mta >>logs/mta.log 2>&1 & echo $! >logs/mta.pid
nohup ./bin/api >>logs/api.log 2>&1 & echo $! >logs/api.pid
sleep 1

echo "mta_pid=$(cat logs/mta.pid) api_pid=$(cat logs/api.pid)"
ss -tlnp | grep -E ':(25|587|8080)\b' || true
strings /opt/wernanmail/bin/mta | grep -F 'body_template_plain' | head -1 || true
strings /opt/wernanmail/bin/api | grep -F 'body_template_plain' | head -1 || true
ls -la www/admin/assets | tail -3
