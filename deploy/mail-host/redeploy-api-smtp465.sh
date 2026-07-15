#!/usr/bin/env bash
set -euo pipefail
install -m 755 /tmp/wm-api /opt/wernanmail/bin/api
cp -a /tmp/wm-web-fix/. /opt/wernanmail/www/
chmod -R a+rX /opt/wernanmail/www
if [ -f /opt/wernanmail/logs/api.pid ]; then
  kill "$(cat /opt/wernanmail/logs/api.pid)" 2>/dev/null || true
fi
pkill -f '/opt/wernanmail/bin/api' 2>/dev/null || true
sleep 1
cd /opt/wernanmail
set -a
# shellcheck disable=SC1091
. ./.env
set +a
nohup ./bin/api >>logs/api.log 2>&1 &
echo $! >logs/api.pid
sleep 1
curl -sS http://127.0.0.1:8080/healthz
echo
# smoke: send path string present
strings /opt/wernanmail/bin/api | grep -F 'use port 465 SMTPS' | head -1 || true
grep -o 'smtpPort:[0-9]*' /opt/wernanmail/www/assets/*.js | sort -u | head -10 || true
echo DONE
