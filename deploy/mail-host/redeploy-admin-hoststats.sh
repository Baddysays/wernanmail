#!/usr/bin/env bash
set -euo pipefail
install -m 755 /tmp/wm-admin /opt/wernanmail/bin/admin
if [ -f /opt/wernanmail/logs/admin.pid ]; then
  kill "$(cat /opt/wernanmail/logs/admin.pid)" 2>/dev/null || true
fi
pkill -f '/opt/wernanmail/bin/admin' 2>/dev/null || true
sleep 1
cd /opt/wernanmail
set -a
# shellcheck disable=SC1091
. ./.env
set +a
nohup ./bin/admin >>logs/admin.log 2>&1 &
echo $! >logs/admin.pid
sleep 1
PASS=$(grep '^ADMIN_PASSWORD=' .env | cut -d= -f2-)
USER=$(grep '^ADMIN_USER=' .env | cut -d= -f2-)
curl -sS -u "$USER:$PASS" http://127.0.0.1:8090/api/admin/host-stats | python3 -c 'import sys,json; d=json.load(sys.stdin)["data"]; print("rssMB", round(d["mailRssBytes"]/1048576,1)); print("procs", [(p["name"], p["pid"]) for p in d["processes"]])'
